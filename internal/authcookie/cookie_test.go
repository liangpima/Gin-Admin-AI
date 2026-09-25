package authcookie

import (
	"encoding/hex"
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

// TestSetWritesLoginFlag 登录态标记的属性（P3-B2）。
//
// 这个 cookie 的存在理由有点绕，值得钉死：token 都 HttpOnly 之后，
// 前端**没有任何办法**知道"本地有没有凭据"，路由守卫会退化成
// 「永远未登录 → 死循环跳 /login」。所以服务端要额外发一个**非敏感**标记。
//
// 两个断言各自对应一种真实退化：
//   - HttpOnly 被误设成 true → 前端读不到 → 守卫永远判定未登录（页面打不开）
//   - MaxAge 跟着 access token 走 → 2 小时后标记先失效，守卫在**发请求之前**
//     就跳登录页，B4 的自动续期根本没机会跑（用户以为"续期坏了"）
func TestSetWritesLoginFlag(t *testing.T) {
	withSecurity(t, config.TokenTransportBoth, config.CookieSameSiteLax, nil, 7200, 604800)

	c, w := newCookieCtx()
	Set(c, "access-1", "refresh-1")
	got := cookiesOf(t, w)

	flag, ok := got[LoginFlagCookieName]
	if !ok {
		t.Fatalf("应下发 %s，实际只有 %v", LoginFlagCookieName, keysOf(got))
	}
	if flag.HttpOnly {
		t.Error("登录态标记**必须**是 JS 可读的（HttpOnly=false），" +
			"否则前端守卫永远判定未登录")
	}
	if flag.Value != LoginFlagCookieValue {
		t.Errorf("标记值应为 %q（前端按严格相等比较），实际 %q",
			LoginFlagCookieValue, flag.Value)
	}
	if flag.Path != LoginFlagCookiePath {
		t.Errorf("标记 Path 应为 %q，实际 %q", LoginFlagCookiePath, flag.Path)
	}
	if flag.MaxAge != 604800 {
		t.Errorf("标记 MaxAge 应取自 jwt.refresh_expire(604800) 而非 access_expire(7200)，实际 %d",
			flag.MaxAge)
	}

	// 反过来确认真正的 token 仍是 HttpOnly —— 这条是 B 组改造的**唯一真正收益**，
	// 加标记 cookie 时最容易顺手把它一起放开
	if access := got[AccessCookieName]; access != nil && !access.HttpOnly {
		t.Error("access_token 必须保持 HttpOnly（新增标记 cookie 不应影响它）")
	}
}

// TestSetWritesCSRFToken 双提交 Cookie 的令牌属性（P3-B3）。
//
// 与登录态标记相反，这个 cookie **必须**是 JS 可读的：整个 double-submit 机制
// 就是「浏览器读出 cookie 的值、放进请求头，服务端比对两者是否相等」。
// 因此它是最容易被后人「顺手加强」的地方 —— 看起来像凭据就加上 HttpOnly，
// 加完登录照常能用（GET 不受影响），但所有写操作会在下一次请求时 403。
func TestSetWritesCSRFToken(t *testing.T) {
	withSecurity(t, config.TokenTransportBoth, config.CookieSameSiteLax, nil, 7200, 604800)

	c, w := newCookieCtx()
	Set(c, "access-1", "refresh-1")
	got := cookiesOf(t, w)

	token, ok := got[CSRFCookieName]
	if !ok {
		t.Fatalf("应下发 %s，实际只有 %v", CSRFCookieName, keysOf(got))
	}
	if token.HttpOnly {
		t.Error("CSRF 令牌**必须**是 JS 可读的（HttpOnly=false）：" +
			"读不到就无法放进请求头，double-submit 直接失效")
	}
	if token.Path != CSRFCookiePath {
		t.Errorf("CSRF 令牌 Path 应为 %q（它要跟着每个非 GET 请求发出去），实际 %q",
			CSRFCookiePath, token.Path)
	}
	if token.MaxAge != 604800 {
		t.Errorf("CSRF 令牌 MaxAge 应取自 jwt.refresh_expire(604800)，实际 %d", token.MaxAge)
	}
	if token.Value == "" {
		t.Error("CSRF 令牌不能为空 —— 空值会让「空 == 空」通过校验")
	}
	if len(token.Value) != csrfTokenBytes*2 {
		t.Errorf("令牌应为 %d 位 hex 字符串，实际长度 %d（%q）",
			csrfTokenBytes*2, len(token.Value), token.Value)
	}
	if _, err := hex.DecodeString(token.Value); err != nil {
		t.Errorf("令牌应为合法 hex，实际 %q：%v", token.Value, err)
	}
}

// TestCSRFTokenIsStableWithinSession 续期时**不得**轮换令牌。
//
// 这是本方案的一处关键取舍（理由写在 csrfTokenFor 的注释里）：续期发生在
// 并发请求的中间，若此时轮换令牌，那些「已经读好 cookie、拼好头但还没发出去」
// 的请求会带着旧值、而 cookie 已经变成新值 —— 头与 cookie 不一致 → 403。
// 窗口很窄但真实存在，现象是「偶发某一个请求失败」，几乎不可能定位。
func TestCSRFTokenIsStableWithinSession(t *testing.T) {
	withSecurity(t, config.TokenTransportBoth, config.CookieSameSiteLax, nil, 7200, 604800)

	// 1) 首次登录（请求里没有令牌）→ 生成
	c, w := newCookieCtx()
	Set(c, "access-1", "refresh-1")
	first := cookiesOf(t, w)[CSRFCookieName].Value

	// 2) 续期（浏览器把令牌带了回来）→ 必须沿用同一个值
	c2, w2 := newCookieCtx()
	c2.Request.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: first})
	Set(c2, "access-2", "refresh-2")

	second, ok := cookiesOf(t, w2)[CSRFCookieName]
	if !ok {
		t.Fatalf("续期时也应重新下发 %s（否则它会在 refresh token 之前过期）", CSRFCookieName)
	}
	if second.Value != first {
		t.Errorf("续期不应轮换 CSRF 令牌：旧 %q，新 %q", first, second.Value)
	}

	// 3) 请求里带着一个别处的值 → 原样沿用（不去猜它从哪来）
	c3, w3 := newCookieCtx()
	c3.Request.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "preset-value"})
	Set(c3, "access-3", "refresh-3")
	if got := cookiesOf(t, w3)[CSRFCookieName].Value; got != "preset-value" {
		t.Errorf("请求已带令牌时应沿用，实际 %q", got)
	}
}

// TestCSRFTokenIsRandomPerLogin 令牌必须是随机的。
//
// 若实现退化成固定值（或某个可推导的值），double-submit 就只剩「攻击者
// 猜得到那个常量」这一层薄壳 —— 而功能上一切正常，没有任何现象。
func TestCSRFTokenIsRandomPerLogin(t *testing.T) {
	withSecurity(t, config.TokenTransportBoth, config.CookieSameSiteLax, nil, 7200, 604800)

	seen := make(map[string]bool, 8)
	for i := 0; i < 8; i++ {
		c, w := newCookieCtx()
		Set(c, "access", "refresh")
		v := cookiesOf(t, w)[CSRFCookieName].Value
		if seen[v] {
			t.Fatalf("令牌出现重复值 %q —— 必须每次登录重新随机生成", v)
		}
		seen[v] = true
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
		// 登录态标记也要清：漏了它，用户登出后刷新页面会被标记骗回
		// 「已登录」分支，白拉一次 userInfo 再被踢一次
		{LoginFlagCookieName, LoginFlagCookiePath},
		// CSRF 令牌一并清（P3-B3）：留着不构成漏洞，但会让下一次登录把它
		// 当成「请求已带」而沿用，掩盖掉本应重新生成的路径
		{CSRFCookieName, CSRFCookiePath},
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
