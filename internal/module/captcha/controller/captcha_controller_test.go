package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/module/captcha/model"
	"go-admin/internal/testsupport"

	"github.com/gin-gonic/gin"
)

// 本文件锁定「验证码 IP 口径」这条回归（H6）。
//
// 背景：验证码凭证 `captcha:verified:<token>` 存的是 `"1|<ip>"`，
// 登录时 `ConsumeVerifiedToken` 做**等值比对**。而登录侧写入的 IP 是
// `common.NormalizeIP(c.ClientIP())`（见 auth_controller.go）。
// 若验证码侧用原始 `c.ClientIP()`，则 localhost / IPv6 反代 / Windows 浏览器
// 把 localhost 解析成 `::1` 的客户端会出现：
//   验证码侧存 "::1"、登录侧传 "127.0.0.1" → 比对失败。
// 且失败分支在 Del 之前返回，凭证不被消费，TTL 内重试多少次都失败 ——
// 这类客户端**登录 100% 失败**，且刷新验证码无效。
//
// 断言对象刻意选「Redis 里存了什么」而不是「接口返回了什么」：
// 两种 IP 形态都能让接口返回成功，只有存储内容能区分口径是否一致。

const (
	// normalizedLoopback 两种回环写法归一化后的结果
	normalizedLoopback = "127.0.0.1"
	// captchaRateKey 归一化后的生成限流键（与 service 层的键构造一致）
	captchaRateKey = "captcha:rate:ip:" + normalizedLoopback
)

// genCtx 造一个 ClientIP 为指定值的 gin 上下文。
//
// 用 RemoteAddr 而不是 X-Forwarded-For：项目默认 trusted_proxies 为空，
// ClientIP() 只取连接对端地址（伪造头无效），所以测试也必须走同一条路径。
func genCtx(method, path, remoteAddr, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = remoteAddr
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, w
}

// resetCaptchaRateKey 清掉限流计数键。
//
// 限流窗口是 1 分钟，但计数键在**同一个 Redis** 上跨测试、跨运行累积：
// 同机重复跑本包时，第 4 次运行就会撞到 captchaRateLimit=10 而被误判为失败。
// 显式清掉才能让断言「计数应为 2」稳定成立。
func resetCaptchaRateKey(t *testing.T) {
	t.Helper()
	if err := cache.Del(t.Context(), captchaRateKey); err != nil {
		t.Fatalf("清理限流计数键失败: %v", err)
	}
}

// decodeCaptchaResp 取出统一响应体里的 data。
func decodeCaptchaResp(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp struct {
		Code    int                    `json:"code"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v（原始 %q）", err, w.Body.String())
	}
	if resp.Code != 0 {
		t.Fatalf("接口应成功，实际 code=%d message=%s", resp.Code, resp.Message)
	}
	return resp.Data
}

// TestCaptchaIPNormalizedEndToEnd 验证码生成/校验/凭证三处必须使用
// **归一化后**的 IP，且形态变化（::1 → ::ffff:127.0.0.1）不能影响比对。
//
// 这两条 IPv6 回环写法会被 common.NormalizeIP 统一成 127.0.0.1，
// 但原始 c.ClientIP() 会原样透出 —— 正是缺陷的触发条件。
func TestCaptchaIPNormalizedEndToEnd(t *testing.T) {
	testsupport.WithTestRedis(t)
	resetCaptchaRateKey(t)
	ctl := NewCaptchaController()

	// ① 生成：客户端以 IPv6 回环出现
	genCtxObj, genW := genCtx(http.MethodGet, "/api/v1/captcha/generate", "[::1]:34567", "")
	ctl.Generate(genCtxObj)

	genData := decodeCaptchaResp(t, genW)
	token, _ := genData["token"].(string)
	if token == "" {
		t.Fatal("生成接口未返回 token")
	}

	// 断言 Redis 里的验证码数据绑定的是归一化 IP，而不是 "::1"
	raw, err := cache.Get(genCtxObj.Request.Context(), "captcha:"+token)
	if err != nil {
		t.Fatalf("读取验证码数据失败: %v", err)
	}
	var data struct {
		Points []model.Point `json:"points"`
		IP     string        `json:"ip"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatalf("验证码数据不是合法 JSON: %v", err)
	}
	if data.IP != normalizedLoopback {
		t.Errorf("生成的验证码应绑定归一化 IP %s，实际 %q —— "+
			"登录侧用的是归一化值，不一致会让这类客户端 100%% 登录失败",
			normalizedLoopback, data.IP)
	}

	// ② 校验：同一客户端换成 IPv4-mapped 写法（真实浏览器/反代会出现）
	// 若校验侧不做归一化，这里的 clientIP 是 "::ffff:127.0.0.1"，
	// 与存储的 "127.0.0.1" 不等 → 被判成「凭证被转手」而拒绝。
	verifyBody, _ := json.Marshal(model.CaptchaVerifyRequest{Token: token, Points: data.Points})
	verCtx, verW := genCtx(http.MethodPost, "/api/v1/captcha/verify",
		"[::ffff:127.0.0.1]:45678", string(verifyBody))
	ctl.Verify(verCtx)

	verData := decodeCaptchaResp(t, verW)
	if ok, _ := verData["success"].(bool); !ok {
		t.Fatalf("同一客户端的 IPv4-mapped 写法应能通过校验，实际被拒: %v（%v）",
			verData["message"], verData)
	}
	newToken, _ := verData["token"].(string)
	if newToken == "" {
		t.Fatal("校验成功应返回新的凭证 token")
	}

	// ③ 凭证内容：登录侧会拿它与 common.NormalizeIP(ClientIP) 做等值比对
	val, err := cache.Get(verCtx.Request.Context(), "captcha:verified:"+newToken)
	if err != nil {
		t.Fatalf("读取一次性凭证失败: %v", err)
	}
	if want := "1|" + normalizedLoopback; val != want {
		t.Errorf("凭证内容应为 %q（登录侧按归一化 IP 比对），实际 %q", want, val)
	}
}

// TestCaptchaGenerateUsesNormalizedIPForRateLimit 生成限流也必须按归一化 IP 计数。
//
// 否则同一客户端在 "::1" 与 "::ffff:127.0.0.1" 之间来回切换就能拿到两份额度，
// 限流形同虚设（这正是本项目限流项当初被绕过的形态之一）。
func TestCaptchaGenerateUsesNormalizedIPForRateLimit(t *testing.T) {
	testsupport.WithTestRedis(t)
	resetCaptchaRateKey(t)
	ctl := NewCaptchaController()

	// 用两种写法各生成一次，限流键必须落在同一个 key 上
	for _, addr := range []string{"[::1]:11111", "[::ffff:127.0.0.1]:22222"} {
		c, w := genCtx(http.MethodGet, "/api/v1/captcha/generate", addr, "")
		ctl.Generate(c)
		decodeCaptchaResp(t, w)
	}

	// 归一化后两种写法都是 127.0.0.1，计数应为 2（同一个键）
	count, err := cache.Get(t.Context(), captchaRateKey)
	if err != nil {
		t.Fatalf("限流计数键应按归一化 IP 写入，读取失败: %v", err)
	}
	if count != "2" {
		t.Errorf("两种回环写法应共用同一个限流键，计数应为 2，实际 %q", count)
	}

	// 反证：原始形态的键不应存在
	if _, err := cache.Get(t.Context(), "captcha:rate:ip:::1"); err == nil {
		t.Error("不应再按原始 IPv6 形态计数（会出现两份额度，限流被绕过）")
	}
}

// TestNormalizeIPContract 固化 NormalizeIP 的契约，说明本修复依赖它的哪些行为。
//
// 若哪天有人「简化」掉 ::ffff: 前缀剥离，上面的端到端用例会以
// 「凭证来源不符」的形式失败，而这条会直接指出根因在 NormalizeIP。
func TestNormalizeIPContract(t *testing.T) {
	cases := map[string]string{
		"::1":              "127.0.0.1",
		"::ffff:127.0.0.1": "127.0.0.1",
		"::ffff:10.0.0.9":  "10.0.0.9",
		"192.168.1.5":      "192.168.1.5",
		"2001:db8::1":      "2001:db8::1", // 真 IPv6 原样保留
	}
	for in, want := range cases {
		if got := common.NormalizeIP(in); got != want {
			t.Errorf("NormalizeIP(%q) = %q，期望 %q", in, got, want)
		}
	}
}
