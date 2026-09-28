package middleware

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-admin/config"
	"go-admin/internal/common"

	"github.com/gin-gonic/gin"
)

// runBodyLimit 让一个请求穿过 BodyLimit 与下游 handler。
//
// declaredLen 单独传而不是从 payload 推：本中间件的第一道防线正是
// 「ContentLength 与真实长度不一致」时的行为（chunked 为 -1、伪造为小值），
// 若把两者绑死就永远测不到第二道防线（MaxBytesReader）。
func runBodyLimit(t *testing.T, limit int64, payload string, declaredLen int64, downstream gin.HandlerFunc) (*httptest.ResponseRecorder, *countingBody) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimit(limit))
	r.POST("/x", downstream)

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	body := &countingBody{reader: strings.NewReader(payload)}
	req.Body = body
	req.ContentLength = declaredLen
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w, body
}

// respCode 解析统一响应体里的业务码。
//
// 空响应体视为 0（放行）：本中间件的下游 handler 用 c.Status 结束、
// 不写任何 body，而中间件拒绝时才会写统一响应结构。
// 若哪天中间件「拒绝但没写响应体」，这里会返回 0 让断言以
// 「应返回 413，实际 0」的形式失败，不会被静默放过。
func respCode(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	if w.Body.Len() == 0 {
		return 0
	}
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v（原始内容 %q）", err, w.Body.String())
	}
	return resp.Code
}

// TestBodyLimitRejectsDeclaredOversizeWithoutReading 长度已声明且超限时，
// 必须**一个字节都不读**就返回 413。
//
// 为什么断言「读走 0 字节」而不是只看业务码：这道防线的全部价值就是
// 「不为拒绝一个 1GB 请求而先把它收下来」。若实现改成先 io.ReadAll 再判长度，
// 业务码依然是 413、用例依然通过，但内存已经被吃光了 —— 只有字节数能区分。
func TestBodyLimitRejectsDeclaredOversizeWithoutReading(t *testing.T) {
	handlerRan := false
	payload := strings.Repeat("a", 200)

	w, body := runBodyLimit(t, 100, payload, int64(len(payload)), func(c *gin.Context) {
		handlerRan = true
		c.Status(http.StatusOK)
	})

	if code := respCode(t, w); code != common.CodePayloadTooLarge {
		t.Errorf("超限应返回业务码 %d，实际 %d", common.CodePayloadTooLarge, code)
	}
	// **HTTP 状态码同样必须是 413**，不能是 200。
	//
	// 这条断言此前缺失，而上面的注释一直写着「返回 413」—— 实现用 common.Error
	// （固定 c.JSON(200, …)）时用例照样通过。后果不是前端（拦截器按业务码判定），
	// 而是**边缘完全无声**：nginx / LB / WAF 的访问日志与错误率统计里，
	// 被拒绝的超大请求与正常请求长得一模一样，防护触发时没有任何信号；
	// curl -f 这类按状态码判断的调用方还会当成功。
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("超限应返回 HTTP 413，实际 %d", w.Code)
	}
	if handlerRan {
		t.Error("超限请求不应进入下游 handler")
	}
	if body.read != 0 {
		t.Errorf("超限请求不应读取任何字节，实际读走 %d", body.read)
	}
}

// TestBodyLimitAllowsWithinLimit 未超限的请求必须原样放行，body 完整可读。
//
// 反向验证：若实现把判断写成 `>=` 或把 MaxBytesReader 的上限算错，
// 边界内的合法请求会被误拒 —— 这是「修了 DoS 却让正常功能不可用」的典型回归。
func TestBodyLimitAllowsWithinLimit(t *testing.T) {
	payload := `{"hello":"world"}`
	var got string

	w, body := runBodyLimit(t, 1024, payload, int64(len(payload)), func(c *gin.Context) {
		b, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Errorf("未超限的 body 应能完整读出，实际 %v", err)
		}
		got = string(b)
		c.Status(http.StatusOK)
	})

	if code := respCode(t, w); code != 0 {
		t.Fatalf("未超限应放行（业务码 0），实际 %d", code)
	}
	if got != payload {
		t.Errorf("下游读到的 body 不完整：%q", got)
	}
	if body.read != len(payload) {
		t.Errorf("应读走全部 %d 字节，实际 %d", len(payload), body.read)
	}
}

// TestBodyLimitCapsUndeclaredOversizeBody 未声明长度（chunked）时，
// MaxBytesReader 必须在读取阶段就地截断。
//
// 这是「内存不被吃光」这个安全目标的**实际保障** —— 第一道防线依赖
// ContentLength，而攻击者可以不给它。若这里被改成只靠 ContentLength 判断，
// 本用例会看到下游把 200 字节全部读完且无错误。
func TestBodyLimitCapsUndeclaredOversizeBody(t *testing.T) {
	const limit = 100
	payload := strings.Repeat("a", 200) // 真实长度远超上限

	var readErr error
	var readN int

	w, body := runBodyLimit(t, limit, payload, -1, func(c *gin.Context) {
		b, err := io.ReadAll(c.Request.Body)
		readErr = err
		readN = len(b)
		c.Status(http.StatusOK)
	})
	_ = w // 本用例断言的是「读取被截断」，响应体不是关注点

	if readErr == nil {
		t.Fatalf("超过上限的 chunked body 读取应报错，实际读走 %d 字节且无错误", readN)
	}
	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Errorf("错误应为 *http.MaxBytesError，实际 %T: %v", readErr, readErr)
	}
	if maxErr != nil && maxErr.Limit != limit {
		t.Errorf("MaxBytesError.Limit 应为 %d，实际 %d", limit, maxErr.Limit)
	}
	// MaxBytesReader 读满 limit 后即报错，因此下游拿到的不可能超过 limit。
	if readN > limit {
		t.Errorf("读取量必须被上限封顶（%d），实际 %d", limit, readN)
	}
	if body.read > limit+1 {
		t.Errorf("底层 reader 被读走的字节数应约等于上限，实际 %d", body.read)
	}
}

// TestBodyLimitDisabledWhenNonPositive 上限 <= 0 表示不启用。
//
// 这条口径要保留：老配置里没有 max_body_size 字段时读到的是 0，
// 若把 0 当成「上限 0 字节」就会拒绝掉所有带 body 的请求。
// 默认值的补齐在 config.Validate 里做，中间件自身只认「<=0 即关闭」。
func TestBodyLimitDisabledWhenNonPositive(t *testing.T) {
	payload := strings.Repeat("a", 200)
	handlerRan := false
	var got string

	w, body := runBodyLimit(t, 0, payload, int64(len(payload)), func(c *gin.Context) {
		handlerRan = true
		b, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Errorf("不启用时不应有读取限制，实际 %v", err)
		}
		got = string(b)
		c.Status(http.StatusOK)
	})

	if !handlerRan {
		t.Fatal("上限为 0 时应视为不启用，请求必须放行")
	}
	if code := respCode(t, w); code != 0 {
		t.Errorf("不启用时业务码应为 0，实际 %d", code)
	}
	if got != payload {
		t.Errorf("不启用时下游应能读走完整 body（%d 字节），实际 %d 字节", len(payload), len(got))
	}
	if body.read != len(payload) {
		t.Errorf("不启用时下游应能读走全部 %d 字节，实际 %d", len(payload), body.read)
	}
}

// TestHumanSize 文案换算：上限以 MB 配置，文案也必须是 MB。
func TestHumanSize(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{64 << 20, "64MB"},
		{10 << 20, "10MB"},
		{1 << 20, "1MB"},
		// 不足 1MB 时向下取整会得到 0，必须兜底成 1MB，
		// 否则文案会变成「最大允许 0MB」
		{100, "1MB"},
		{0, "1MB"},
	}
	for _, c := range cases {
		if got := humanSize(c.bytes); got != c.want {
			t.Errorf("humanSize(%d) = %q，期望 %q", c.bytes, got, c.want)
		}
	}
}

// TestBodyLimitFromConfigUsesConfiguredSize 配置入口必须真的把
// server.max_body_size 传下去。
//
// 直接改全局 config.Cfg 并 defer 还原：本包用例不使用 t.Parallel()，
// 顺序执行下不会互相干扰。
func TestBodyLimitFromConfigUsesConfiguredSize(t *testing.T) {
	prev := config.Cfg.Server.MaxBodySize
	defer func() { config.Cfg.Server.MaxBodySize = prev }()

	config.Cfg.Server.MaxBodySize = 1 // 1MB

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimitFromConfig())
	r.POST("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	// 声明 1MB + 1 字节，刚好越过上限
	over := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(""))
	over.ContentLength = (1 << 20) + 1
	w := httptest.NewRecorder()
	r.ServeHTTP(w, over)
	if code := respCode(t, w); code != common.CodePayloadTooLarge {
		t.Errorf("超出配置上限应返回 %d，实际 %d", common.CodePayloadTooLarge, code)
	}

	// 上限之内必须放行
	under := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("{}"))
	under.ContentLength = 2
	w = httptest.NewRecorder()
	r.ServeHTTP(w, under)
	if code := respCode(t, w); code != 0 {
		t.Errorf("上限内应放行，实际业务码 %d", code)
	}
}

// TestBodyLimitFromConfigFallsBackToDefault 未配置时（MaxBodySize <= 0）
// 必须回落到内置默认值，而不是「不限制」。
//
// 「不限制」是本中间件要修的那个问题本身 —— 若默认值丢了，
// 所有未显式配置的部署都等于没接这道防线。
//
// 断言方式刻意**不写死默认值**：先取 `MaxBodyBytes()` 算出的实际上限，
// 再分别试探「上限 +1」与「上限 -1」。早前这里写死了 64MB/65MB/1MB，
// 默认值从 64 收到 14 后，用例照样通过但注释与理由全部失真 ——
// 「声明 65MB 应被拒」的理由已经变成「超过 14MB 上限」，
// 而读注释的人会以为默认值仍是 64。
func TestBodyLimitFromConfigFallsBackToDefault(t *testing.T) {
	prev := config.Cfg.Server.MaxBodySize
	defer func() { config.Cfg.Server.MaxBodySize = prev }()

	config.Cfg.Server.MaxBodySize = 0
	limit := config.Cfg.Server.MaxBodyBytes()
	if limit <= 0 {
		t.Fatalf("默认请求体上限丢失（MaxBodyBytes 返回 %d）", limit)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimitFromConfig())
	r.POST("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	// 默认上限 + 1 字节：必须被拒
	over := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(""))
	over.ContentLength = limit + 1
	w := httptest.NewRecorder()
	r.ServeHTTP(w, over)
	if code := respCode(t, w); code != common.CodePayloadTooLarge {
		t.Errorf("未配置时应按默认上限 %d 字节拒绝 %d 字节请求，实际业务码 %d",
			limit, limit+1, code)
	}

	// 默认上限 - 1 字节：必须放行
	under := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("{}"))
	under.ContentLength = limit - 1
	w = httptest.NewRecorder()
	r.ServeHTTP(w, under)
	if code := respCode(t, w); code != 0 {
		t.Errorf("默认上限内应放行，实际业务码 %d", code)
	}
}

// TestDefaultMaxBodySizeStaysCloseToUploadLimit 默认上限必须「贴着」上传上限，
// 不能远大于所需。
//
// 这个数字直接决定单个请求最坏占多少内存：操作日志中间件对 JSON body 会
// 读一份、再 Unmarshal 一份（峰值约 2× body）。取 64MB 时，
// 50 个并发就是 6.4GB —— 而本项目的 JSON 业务体都是 KB 级，
// 64MB 没有任何正当用途。
//
// 上界 16MB 是**设计约束**而非实现细节：请求体上限只需要覆盖
// upload.max_size(10MB) 的 multipart 边界开销。若将来上传上限调大，
// 这条断言应同步调整（那正是它存在的意义 —— 强制这次调整被人看见）。
func TestDefaultMaxBodySizeStaysCloseToUploadLimit(t *testing.T) {
	prev := config.Cfg.Server.MaxBodySize
	defer func() { config.Cfg.Server.MaxBodySize = prev }()

	config.Cfg.Server.MaxBodySize = 0
	limitMB := config.Cfg.Server.MaxBodyBytes() >> 20

	if limitMB > 16 {
		t.Errorf("默认请求体上限 %d MB 过大：它会成倍放大单请求内存占用，应贴着 upload.max_size 取值", limitMB)
	}
	if limitMB <= 0 {
		t.Errorf("默认请求体上限非法: %d MB", limitMB)
	}
}
