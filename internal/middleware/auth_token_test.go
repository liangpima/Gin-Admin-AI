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

// withTokenTransport 临时设定 token 传输方式并在用例结束时还原。
//
// config.Cfg 是包级全局，中间件直接读它（与 cors.go 读 config.Cfg.CORS 一致）。
// 用例必须还原，否则同包其它用例会看到上一个用例留下的取值 ——
// 这类「结果取决于执行顺序」的测试比不写还危险。
func withTokenTransport(t *testing.T, transport string) {
	t.Helper()
	prev := config.Cfg.Security.TokenTransport
	config.Cfg.Security.TokenTransport = transport
	t.Cleanup(func() { config.Cfg.Security.TokenTransport = prev })
}

// newTokenCtx 造一个带（或不带）Authorization 头与 access cookie 的请求上下文。
func newTokenCtx(t *testing.T, authHeader, cookieValue string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/userInfo", nil)
	if authHeader != "" {
		c.Request.Header.Set("Authorization", authHeader)
	}
	if cookieValue != "" {
		c.Request.AddCookie(&http.Cookie{Name: authcookie.AccessCookieName, Value: cookieValue})
	}
	return c
}

// TestExtractToken token 的三种来源优先级。
//
// 这是 B1（后端双读 cookie）的核心判定，单独抽成纯函数就是为了能这样穷举 ——
// Auth 中间件后面几步依赖 Redis，测试环境没有 Redis 就走不到。
//
// 三条必须钉住的语义：
//  1. both 模式下**头优先**：前端改造期间两条路径并存，头是显式意图，不能被 cookie 盖过
//  2. header 模式**不读 cookie**：这是回滚开关，必须能真正回到改造前的行为
//  3. 头存在但格式错时**不回退到 cookie**：回退会把「调用方发错了头」
//     掩盖成「用 cookie 认证成功」，问题变成偶发
func TestExtractToken(t *testing.T) {
	const goodToken = "header-token-value"
	const cookieToken = "cookie-token-value"

	cases := []struct {
		name       string
		transport  string
		authHeader string
		cookie     string
		wantToken  string
		wantSource string
		wantErrMsg string
	}{
		// ---- header 模式（改造前的行为）----
		{"header: 有合法头", config.TokenTransportHeader, "Bearer " + goodToken, "", goodToken, common.TokenSourceHeader, ""},
		{"header: 无头有 cookie 也必须拒绝", config.TokenTransportHeader, "", cookieToken, "", "", "请先登录"},
		{"header: 无头无 cookie", config.TokenTransportHeader, "", "", "", "", "请先登录"},
		{"header: 头格式错", config.TokenTransportHeader, "Token " + goodToken, "", "", common.TokenSourceHeader, "Token格式错误"},
		{"header: 头格式错时不因有 cookie 而放行", config.TokenTransportHeader, "Token " + goodToken, cookieToken, "", common.TokenSourceHeader, "Token格式错误"},

		// ---- both 模式（默认）----
		{"both: 头优先于 cookie", config.TokenTransportBoth, "Bearer " + goodToken, cookieToken, goodToken, common.TokenSourceHeader, ""},
		{"both: 无头时用 cookie", config.TokenTransportBoth, "", cookieToken, cookieToken, common.TokenSourceCookie, ""},
		{"both: 都没有", config.TokenTransportBoth, "", "", "", "", "请先登录"},
		{"both: 头格式错时不回退到 cookie", config.TokenTransportBoth, "Bearer", cookieToken, "", common.TokenSourceHeader, "Token格式错误"},
		{"both: 未配置该值时按 both 处理", "", "Bearer " + goodToken, cookieToken, goodToken, common.TokenSourceHeader, ""},

		// ---- cookie 模式 ----
		{"cookie: 忽略 Authorization 头", config.TokenTransportCookie, "Bearer " + goodToken, cookieToken, cookieToken, common.TokenSourceCookie, ""},
		{"cookie: 只有头没有 cookie 必须拒绝", config.TokenTransportCookie, "Bearer " + goodToken, "", "", "", "请先登录"},
		{"cookie: 只有 cookie", config.TokenTransportCookie, "", cookieToken, cookieToken, common.TokenSourceCookie, ""},
		{"cookie: 头格式错也不影响（本来就不看头）", config.TokenTransportCookie, "garbage", cookieToken, cookieToken, common.TokenSourceCookie, ""},

		// ---- 头格式的边角 ----
		{"Bearer 后为空串算格式错", config.TokenTransportBoth, "Bearer ", "", "", common.TokenSourceHeader, "Token格式错误"},
		{"Bearer 后只有空格算格式错", config.TokenTransportBoth, "Bearer    ", "", "", common.TokenSourceHeader, "Token格式错误"},
		{"大小写不匹配 Bearer 算格式错", config.TokenTransportBoth, "bearer " + goodToken, "", "", common.TokenSourceHeader, "Token格式错误"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withTokenTransport(t, tc.transport)

			token, source, errMsg := extractToken(newTokenCtx(t, tc.authHeader, tc.cookie))
			if token != tc.wantToken {
				t.Errorf("token = %q，期望 %q", token, tc.wantToken)
			}
			if source != tc.wantSource {
				t.Errorf("source = %q，期望 %q", source, tc.wantSource)
			}
			if errMsg != tc.wantErrMsg {
				t.Errorf("errMsg = %q，期望 %q", errMsg, tc.wantErrMsg)
			}
		})
	}
}

// TestExtractTokenDoesNotReadEmptyCookie 空值 cookie 视为「未携带」。
//
// 服务端只会写非空值，所以这条理论上走不到；但浏览器/中间设备
// 把 cookie 写成空串的情况真实存在（例如同名 cookie 被别的路径覆盖过）。
// 若不判空，空串会一路走到 ParseToken 并报「Token无效或已过期」，
// 把「没带凭证」说成「凭证坏了」。
func TestExtractTokenDoesNotReadEmptyCookie(t *testing.T) {
	withTokenTransport(t, config.TokenTransportBoth)

	c := newTokenCtx(t, "", "")
	c.Request.AddCookie(&http.Cookie{Name: authcookie.AccessCookieName, Value: ""})

	token, source, errMsg := extractToken(c)
	if token != "" || source != "" || errMsg != "请先登录" {
		t.Errorf("空值 cookie 应按未携带处理，实际 token=%q source=%q errMsg=%q", token, source, errMsg)
	}
}
