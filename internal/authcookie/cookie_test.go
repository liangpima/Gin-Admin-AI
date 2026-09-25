package authcookie

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-admin/config"

	"github.com/gin-gonic/gin"
)

// withSecurity 临时设定 token 传输策略与 JWT 有效期，并在用例结束时还原。
//
// config.Cfg 是包级全局。不还原的话，同包用例会看到上一个用例留下的取值，
// 结果取决于执行顺序 —— 这类测试比不写还危险。
func withSecurity(t *testing.T, transport, sameSite string, secure *bool, accessExpire, refreshExpire int64) {
	t.Helper()
	prev := config.Cfg
	config.Cfg.Security.TokenTransport = transport
	config.Cfg.Security.CookieSameSite = sameSite
	config.Cfg.Security.CookieSecure = secure
	config.Cfg.JWT.AccessExpire = accessExpire
	config.Cfg.JWT.RefreshExpire = refreshExpire
	t.Cleanup(func() { config.Cfg = prev })
}

// newCookieCtx 造一个能记录 Set-Cookie 的上下文。
func newCookieCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	return c, w
}

// cookiesOf 把响应里的 Set-Cookie 解析成「名字 → cookie」。
//
// 用 http.ReadSetCookies 而不是自己切字符串：Set-Cookie 的格式细节
// （属性顺序、Expires 的逗号）很容易切错，而切错的方向恰好是「看起来通过了」。
func cookiesOf(t *testing.T, w *httptest.ResponseRecorder) map[string]*http.Cookie {
	t.Helper()
	raw := w.Result().Cookies()
	out := make(map[string]*http.Cookie, len(raw))
	for _, c := range raw {
		out[c.Name] = c
	}
	return out
}

// TestSetDoesNothingInHeaderMode header 模式是回滚开关，必须连 cookie 都不发。
//
// 这是 B1 的「零风险回滚」保证：把 security.token_transport 改回 header，
// 服务端行为应当与改造前**完全一致**（不下发 cookie、只认 Authorization 头）。
// 若这里仍然下发 cookie，回滚就只是「前端不用」而不是「服务端不发」，
// 浏览器里会残留一份没人清理的凭据。
func TestSetDoesNothingInHeaderMode(t *testing.T) {
	withSecurity(t, config.TokenTransportHeader, config.CookieSameSiteLax, nil, 7200, 604800)

	c, w := newCookieCtx()
	Set(c, "access-1", "refresh-1")

	if got := cookiesOf(t, w); len(got) != 0 {
		t.Errorf("header 模式不应下发任何 cookie，实际 %v", got)
	}
}

// TestSetWritesBothCookies both 模式下两个 cookie 的属性。
//
// 断言的重点是**本项改造的唯一真正收益**：HttpOnly。
// 它一旦丢失，token 又回到 JS 可达范围内，整个 B 组就白做了 ——
// 而这种退化在功能上完全看不出来（登录照常能用）。
func TestSetWritesBothCookies(t *testing.T) {
	secure := true
	withSecurity(t, config.TokenTransportBoth, config.CookieSameSiteLax, &secure, 7200, 604800)

	c, w := newCookieCtx()
	Set(c, "access-1", "refresh-1")
	got := cookiesOf(t, w)

	access, ok := got[AccessCookieName]
	if !ok {
		t.Fatalf("应下发 %s，实际只有 %v", AccessCookieName, keysOf(got))
	}
	if !access.HttpOnly {
		t.Error("access_token 必须 HttpOnly —— 这是本项改造的唯一真正收益")
	}
	if access.Value != "access-1" {
		t.Errorf("access_token 值应为 access-1，实际 %q", access.Value)
	}
	if access.Path != AccessCookiePath {
		t.Errorf("access_token Path 应为 %q，实际 %q", AccessCookiePath, access.Path)
	}
	// MaxAge 必须来自 jwt.access_expire，不能写死：
	// 写死会让「改了 token 有效期但 cookie 还是老时长」只能靠用户莫名被登出才发现
	if access.MaxAge != 7200 {
		t.Errorf("access_token MaxAge 应取自 jwt.access_expire(7200)，实际 %d", access.MaxAge)
	}
	if !access.Secure {
		t.Error("cookie_secure=true 时 Secure 属性必须带上")
	}
	if access.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite 应为 Lax，实际 %v", access.SameSite)
	}

	refresh, ok := got[RefreshCookieName]
	if !ok {
		t.Fatalf("应下发 %s，实际只有 %v", RefreshCookieName, keysOf(got))
	}
	if !refresh.HttpOnly {
		t.Error("refresh_token 必须 HttpOnly")
	}
	if refresh.Path != RefreshCookiePath {
		t.Errorf("refresh_token Path 应为 %q，实际 %q", RefreshCookiePath, refresh.Path)
	}
	if refresh.MaxAge != 604800 {
		t.Errorf("refresh_token MaxAge 应取自 jwt.refresh_expire(604800)，实际 %d", refresh.MaxAge)
	}
}

// TestRefreshCookiePathCoversLogout refresh token 的作用域必须覆盖登出接口。
//
// 这是**实施时对计划的一处有意偏离**，也是本文件最该被钉住的一条：
// 计划里写的是 Path=/api/v1/auth/refresh（更窄），但那样浏览器不会把 cookie
// 发到 /auth/logout，服务端就拿不到它去吊销 —— 结果是「登出只拉黑了 access token，
// 手里握着 refresh token 的人仍可换发新的 access token」，等于没登出。
// 而这一点在 auth_service.LogoutByToken 的注释里已经踩过一次。
//
// 用「路径前缀匹配」表达而不是硬比字符串：cookie 的 Path 语义就是前缀匹配，
// 断言应当与语义一致。
func TestRefreshCookiePathCoversLogout(t *testing.T) {
	for _, p := range []string{
		"/api/v1/auth/refresh",
		"/api/v1/auth/logout",
	} {
		if !strings.HasPrefix(p, RefreshCookiePath) {
			t.Errorf("refresh cookie 的 Path(%q) 必须覆盖 %s，否则浏览器不会带上它",
				RefreshCookiePath, p)
		}
	}

	// 同时确认它确实收窄了：不该覆盖业务接口
	for _, p := range []string{
		"/api/v1/system/user/list",
		"/api/v1/member/list",
		"/api/v1/payment/order",
	} {
		if strings.HasPrefix(p, RefreshCookiePath) {
			t.Errorf("refresh cookie 不应被发到业务接口 %s（Path=%q）", p, RefreshCookiePath)
		}
	}
}

// TestSetSkipsEmptyToken 空 token 不应写出空 cookie。
//
// 写空 cookie 会让浏览器把已有的那个覆盖掉 —— 例如登录响应里只有 access token
// 时，refresh cookie 会被清空，用户下一次续期才发现自己没了凭据。
func TestSetSkipsEmptyToken(t *testing.T) {
	withSecurity(t, config.TokenTransportBoth, config.CookieSameSiteLax, nil, 7200, 604800)

	c, w := newCookieCtx()
	Set(c, "access-1", "")
	got := cookiesOf(t, w)

	if _, ok := got[AccessCookieName]; !ok {
		t.Error("非空的 access token 应照常下发")
	}
	if _, ok := got[RefreshCookieName]; ok {
		t.Error("空的 refresh token 不应写出 cookie（会把浏览器里已有的覆盖成空）")
	}
}

// TestClearExpiresBothCookies 登出必须把两个 cookie 都清掉，且 Path 与下发时一致。
//
// Path 不一致是这类实现最常见的 bug：浏览器按「名字 + Path + Domain」区分 cookie，
// 用别的 Path 去清只会新增一个空 cookie，旧的原封不动留着。
func TestClearExpiresBothCookies(t *testing.T) {
	withSecurity(t, config.TokenTransportBoth, config.CookieSameSiteLax, nil, 7200, 604800)

	c, w := newCookieCtx()
	Clear(c)
	got := cookiesOf(t, w)

	for _, tc := range []struct{ name, path string }{
		{AccessCookieName, AccessCookiePath},
		{RefreshCookieName, RefreshCookiePath},
	} {
		ck, ok := got[tc.name]
		if !ok {
			t.Errorf("登出应清除 %s", tc.name)
			continue
		}
		if ck.Value != "" {
			t.Errorf("%s 清除时值应为空，实际 %q", tc.name, ck.Value)
		}
		if ck.MaxAge >= 0 {
			t.Errorf("%s 清除时 MaxAge 应为负（立即过期），实际 %d", tc.name, ck.MaxAge)
		}
		if ck.Path != tc.path {
			t.Errorf("%s 清除时的 Path(%q) 必须与下发时(%q)一致，否则旧 cookie 清不掉",
				tc.name, ck.Path, tc.path)
		}
	}
}

// TestRefreshFromRequest 取 refresh token 的优先级：请求体 → cookie。
//
// 请求体优先是为了不破坏 Swagger / 脚本这类显式传参的调用方（B1 阶段它们
// 完全不该受影响）；cookie 是浏览器的路径。
func TestRefreshFromRequest(t *testing.T) {
	withSecurity(t, config.TokenTransportBoth, config.CookieSameSiteLax, nil, 7200, 604800)

	// 1) 只有 body
	c, _ := newCookieCtx()
	if got := RefreshFromRequest(c, "from-body"); got != "from-body" {
		t.Errorf("只有 body 时应返回 body 的值，实际 %q", got)
	}

	// 2) 只有 cookie
	c, _ = newCookieCtx()
	c.Request.AddCookie(&http.Cookie{Name: RefreshCookieName, Value: "from-cookie"})
	if got := RefreshFromRequest(c, ""); got != "from-cookie" {
		t.Errorf("只有 cookie 时应返回 cookie 的值，实际 %q", got)
	}

	// 3) 两者都有 → body 优先
	c, _ = newCookieCtx()
	c.Request.AddCookie(&http.Cookie{Name: RefreshCookieName, Value: "from-cookie"})
	if got := RefreshFromRequest(c, "from-body"); got != "from-body" {
		t.Errorf("两者都有时应优先 body，实际 %q", got)
	}

	// 4) 都没有 → 空串（调用方按「无需吊销」处理）
	c, _ = newCookieCtx()
	if got := RefreshFromRequest(c, ""); got != "" {
		t.Errorf("都没有时应返回空串，实际 %q", got)
	}
}

func keysOf(m map[string]*http.Cookie) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
