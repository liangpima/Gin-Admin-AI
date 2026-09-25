package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go-admin/config"
	"go-admin/internal/authcookie"
	"go-admin/internal/common"

	"github.com/gin-gonic/gin"
)

// withTransport 临时设定 token 传输策略，并在用例结束时还原。
//
// config.Cfg 是包级全局。不还原的话，同包用例会看到上一个用例留下的取值，
// 结果取决于执行顺序 —— 这类测试比不写还危险。
func withTransport(t *testing.T, transport string) {
	t.Helper()
	prev := config.Cfg
	config.Cfg.Security.TokenTransport = transport
	t.Cleanup(func() { config.Cfg = prev })
}

// runCSRF 跑一遍 CSRF 中间件，返回上下文与响应记录器。
//
// 直接构造 gin.Context 而不是起路由：本中间件的全部输入就是
// 「方法 + 上下文里的凭据来源 + cookie + 头」这四样，
// 走真实路由只会把 gin 的路由匹配行为混进来（而它已经被别处覆盖）。
func runCSRF(t *testing.T, method, source, cookieToken, headerToken string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/api/v1/system/user", nil)

	// 空串表示「这个输入不存在」，便于表驱动里一眼看出每组缺了什么
	if source != "" {
		c.Set(common.ContextKeyTokenSource, source)
	}
	if cookieToken != "" {
		c.Request.AddCookie(&http.Cookie{Name: authcookie.CSRFCookieName, Value: cookieToken})
	}
	if headerToken != "" {
		c.Request.Header.Set(authcookie.CSRFHeaderName, headerToken)
	}

	CSRF()(c)
	return c, w
}

// TestCSRFDoubleSubmit 双提交 Cookie 的完整判定表（P3-B3）。
//
// 这是 CSRF 纵深防御的唯一实现处，因此把「什么该拒、什么该放」逐条钉死。
// 三组断言各自对应一种真实退化：
//
//  1. **该校验的被跳过**（把校验写成恒真 / 忘接中间件）→ 伪造请求直接打到业务接口。
//     这是最危险的一种，因为功能上完全看不出来（正常用户一切照旧）。
//  2. **不该校验的被校验**（把豁免规则写反）→ 非浏览器客户端（Swagger、curl、
//     服务间调用）集体失效，而且是在「登录明明成功」的前提下失效，很难联想到 CSRF。
//  3. **缺凭据时放行**（把「不是 cookie 就豁免」写成默认分支）→ 上下文里
//     没有来源信息时静默放行，等于给攻击者留了一条「想办法让 source 为空」的路。
func TestCSRFDoubleSubmit(t *testing.T) {
	withTransport(t, config.TokenTransportBoth)

	const (
		cookieSrc = common.TokenSourceCookie
		headerSrc = common.TokenSourceHeader
	)

	cases := []struct {
		name       string
		method     string
		source     string
		cookie     string
		header     string
		wantPass   bool
		wantReason string
	}{
		{
			name: "cookie 认证：头与 cookie 一致 → 放行",
			method: http.MethodPost, source: cookieSrc, cookie: "tok", header: "tok",
			wantPass: true, wantReason: "这是正常浏览器的形态，绝不能拦",
		},
		{
			name: "cookie 认证：缺头 → 拒绝",
			method: http.MethodPost, source: cookieSrc, cookie: "tok",
			wantPass: false, wantReason: "跨站请求带得上 cookie，但填不出自定义头",
		},
		{
			name: "cookie 认证：缺 cookie → 拒绝",
			method: http.MethodPost, source: cookieSrc, header: "tok",
			wantPass: false, wantReason: "没有可比对的另一半，不能当成通过",
		},
		{
			name: "cookie 认证：两者都缺 → 拒绝",
			method: http.MethodPost, source: cookieSrc,
			wantPass: false, wantReason: "空 == 空 若算通过，等于关掉整个校验",
		},
		{
			name: "cookie 认证：值不一致 → 拒绝",
			method: http.MethodPost, source: cookieSrc, cookie: "tok", header: "other",
			wantPass: false, wantReason: "攻击者填的任意值必须与 cookie 对不上",
		},
		{
			name: "cookie 认证：仅大小写不同也拒绝",
			method: http.MethodPost, source: cookieSrc, cookie: "tok", header: "TOK",
			wantPass: false, wantReason: "令牌是随机 hex，比较必须是逐字节的",
		},
		{
			name: "PUT 同样校验（不只是 POST）",
			method: http.MethodPut, source: cookieSrc, cookie: "tok",
			wantPass: false, wantReason: "改状态的动词都要覆盖",
		},
		{
			name: "DELETE 同样校验",
			method: http.MethodDelete, source: cookieSrc, cookie: "tok",
			wantPass: false, wantReason: "改状态的动词都要覆盖",
		},
		{
			name: "PATCH 同样校验",
			method: http.MethodPatch, source: cookieSrc, cookie: "tok",
			wantPass: false, wantReason: "改状态的动词都要覆盖",
		},
		{
			name: "GET 免校验",
			method: http.MethodGet, source: cookieSrc,
			wantPass: true, wantReason: "跨站 <img>/<script> 触发的正是 GET，只读无 CSRF 可言",
		},
		{
			name: "HEAD 免校验",
			method: http.MethodHead, source: cookieSrc,
			wantPass: true, wantReason: "安全方法，同 GET",
		},
		{
			name: "OPTIONS 免校验",
			method: http.MethodOptions, source: cookieSrc,
			wantPass: true, wantReason: "CORS 预检本身不带自定义头，拦了会打死跨域部署",
		},
		{
			name: "Authorization 头认证 → 豁免",
			method: http.MethodPost, source: headerSrc,
			wantPass: true, wantReason: "B1 双读带来的好处：Swagger / curl 无需任何改动",
		},
		{
			name: "Authorization 头认证 + 恰好也带了 cookie → 仍豁免",
			method: http.MethodPost, source: headerSrc, cookie: "tok", header: "wrong",
			wantPass: true, wantReason: "豁免看的是凭据来源，不是浏览器顺带带了什么 cookie",
		},
		{
			name: "上下文里没有来源信息 → 拒绝（fail-closed）",
			method: http.MethodPost, source: "",
			wantPass: false, wantReason: "认不出来时若放行，等于留一条绕过路径",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := runCSRF(t, tc.method, tc.source, tc.cookie, tc.header)

			passed := !c.IsAborted()
			if passed != tc.wantPass {
				t.Fatalf("放行=%v，期望 %v（%s）", passed, tc.wantPass, tc.wantReason)
			}

			if tc.wantPass {
				if w.Code != http.StatusOK {
					t.Errorf("放行时不应写响应，实际状态码 %d", w.Code)
				}
				return
			}

			// 拒绝必须是 403（不是 401）：凭据本身是有效的，只是这次请求没通过校验。
			// 报 401 会让前端拦截器去走「续期后重放」—— 重放依旧缺头，白折腾一轮
			// 还多消耗一次 refresh token 轮换。
			if w.Code != http.StatusForbidden {
				t.Errorf("拒绝时状态码应为 403，实际 %d", w.Code)
			}
		})
	}
}

// TestCSRFSkippedWhenCookiesDisabled header 模式是回滚开关，CSRF 校验必须一并失效。
//
// 理由：header 模式下服务端**不下发** csrf cookie，前端也就没有令牌可带 ——
// 若校验仍然生效，回滚到 header 之后所有写请求都会 403，
// 回滚开关本身就坏了（这正是「改一个配置项就能回滚」这个承诺的前提）。
func TestCSRFSkippedWhenCookiesDisabled(t *testing.T) {
	withTransport(t, config.TokenTransportHeader)

	// 故意不给任何令牌：若校验仍然生效，这里必然 403
	c, w := runCSRF(t, http.MethodPost, common.TokenSourceHeader, "", "")
	if c.IsAborted() {
		t.Fatalf("header 模式下不应校验 CSRF，实际被拒绝（状态码 %d）", w.Code)
	}
}

// TestIsSafeMethod 白名单方向：只列安全方法，其余一律按不安全处理。
//
// 用白名单而不是黑名单，是为了让「将来出现新方法」时默认落在需要校验的一侧。
// 本用例同时锁定这个方向 —— 若有人图省事改成 `method != "GET"` 之类的判断，
// 或在列表里加上 TRACE / CONNECT，这里会立刻失败。
func TestIsSafeMethod(t *testing.T) {
	cases := map[string]bool{
		http.MethodGet:     true,
		http.MethodHead:    true,
		http.MethodOptions: true,
		http.MethodPost:    false,
		http.MethodPut:     false,
		http.MethodPatch:   false,
		http.MethodDelete:  false,
		// 非标准方法必须落在「需要校验」一侧
		"TRACE":  false,
		"CONNECT": false,
		"":        false,
		"get":     false, // 方法名是规范里的大写字面量，不做大小写归一
	}

	for method, want := range cases {
		if got := isSafeMethod(method); got != want {
			t.Errorf("isSafeMethod(%q) = %v，期望 %v", method, got, want)
		}
	}
}
