// Package authcookie 负责把 access / refresh token 写进 HttpOnly cookie，以及清除它们。
//
// 为什么单独成包：写认证 cookie 需要同时知道三件事 ——
//   · token 的值与有效期（来自 Service 的登录/刷新结果）
//   · 传输策略（security.token_transport：要不要下发、是否只读 cookie）
//   · cookie 属性（SameSite / Secure）
//
// 这三者分属 Controller、config 与中间件；而中间件**读** cookie 时又必须用
// 与这里完全一致的 cookie 名与 Path（名字对不上会表现成「登录成功但立刻未登录」，
// Path 对不上会表现成「refresh 能用但登出清不掉」）。把这些常量集中在一处，
// 就是为了让「读」和「写」不可能漂移。
//
// 不放进 middleware：那会让 Controller 反向依赖中间件。
// 不放进 common：那会让 common 依赖 config。
package authcookie

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"

	"go-admin/config"
	"go-admin/internal/logger"
)

const (
	// AccessCookieName access token 的 cookie 名。
	//
	// 刻意不复用前端现有的 `go_admin_token`：那个是 JS 可读的，
	// 两者同时存在时同名会互相覆盖，反而把 httpOnly 的属性弄丢。
	AccessCookieName = "access_token"

	// RefreshCookieName refresh token 的 cookie 名。
	RefreshCookieName = "refresh_token"

	// AccessCookiePath access token 在全部接口上都可能用到。
	AccessCookiePath = "/"

	// RefreshCookiePath refresh token 的作用域。
	//
	// 计划里原本写的是 `/api/v1/auth/refresh`（更窄），实施时改为 `/api/v1/auth`，
	// 原因是**登出必须能吊销 refresh token**：只拉黑 access token 的话，
	// 手里握着 refresh token 的人仍可换发新的 access token，等于没登出
	// （这一点在 auth_service.LogoutByToken 的注释里已经踩过一次）。
	// 而 cookie 的 Path 决定浏览器会不会把它发到 `/auth/logout` ——
	// 写成 `/api/v1/auth/refresh` 就永远清不掉服务端的 refresh 记录。
	//
	// `/api/v1/auth` 已经排除了全部业务接口（/system、/member、/payment…），
	// 相对 Path=/ 仍是显著收窄。
	RefreshCookiePath = "/api/v1/auth"

	// LoginFlagCookieName 供前端路由守卫判断「要不要去拉用户信息」的**非敏感**标记。
	//
	// 为什么必须额外下发它：access / refresh token 都是 HttpOnly，JS 读不到 ——
	// 于是前端失去了「本地有没有凭据」这个判断依据。若不做补偿，
	// 路由守卫会退化成「永远判定未登录 → 死循环跳 /login」
	// （docs/plan-p3-optional.md 的 B.2 第 ① 条讲的就是这件事）。
	//
	// 为什么它可以**不是** HttpOnly：它不含任何凭据，读走或伪造都没有价值 ——
	// 伪造它最多让前端多发一次 /auth/userInfo，随后 401 回来清会话。
	// 鉴权永远只看真正的 token（这一点前端注释里也重复了一遍，因为它是
	// 最容易被后人「顺手加强」成鉴权依据的地方）。
	LoginFlagCookieName = "logged_in"

	// LoginFlagCookieValue 标记的取值。
	//
	// 前端用**严格相等**比较（`Cookies.get(...) === '1'`），不是 truthy ——
	// 所以这里不能改成 `true` / `yes` 之类，改了两端会静默不一致，
	// 现象是「登录成功但刷新页面就回到登录页」。
	LoginFlagCookieValue = "1"

	// LoginFlagCookiePath 与 access token 一致（全站可见）。
	//
	// 守卫在任意业务页面上都会读它，收窄 Path 会让部分页面读不到 ——
	// 那会表现成「在这个页面刷新会掉登录，在另一个页面不会」，极难定位。
	LoginFlagCookiePath = "/"

	// CSRFCookieName 「双提交 Cookie」（double-submit）方案里的 CSRF 令牌名。
	//
	// 它**必须是非 HttpOnly**：整个机制就是「浏览器把 cookie 里的值读出来、
	// 放进请求头，服务端只校验两者相等」，服务端不存储任何东西。
	// 这也是它与上面两个 token 的根本区别 —— 那两个越读不到越安全，
	// 这一个读不到就没法用。因此**不能**因为「看起来像凭据」而给它加上 HttpOnly。
	//
	// 为什么攻击者拿不到它：跨站请求能让浏览器**带上** cookie（表单、img、fetch），
	// 但**读不到** cookie 的值，也就填不出匹配的请求头；而自定义头会触发 CORS 预检，
	// 白名单之外直接失败。于是「带上 cookie」与「填对头」无法同时成立。
	CSRFCookieName = "csrf_token"

	// CSRFHeaderName 前端放置 CSRF 令牌的请求头名。
	//
	// 三处必须保持一致：这里、前端 `web/src/utils/auth.ts` 的同名常量、
	// 以及 `cors.allow_headers`（两份配置模板 + cors.go 的兜底默认值）。
	// 前两处不一致 → 所有非 GET 请求 403；第三处漏配只在**跨域部署**
	// （自定义头触发预检）时才暴露，同源部署下测不出来。
	CSRFHeaderName = "X-CSRF-Token"

	// CSRFCookiePath 与 access token 一致（全站可见）。
	//
	// 它要跟着**每一个**非 GET 请求发出去，收窄 Path 会让业务接口全部 403。
	CSRFCookiePath = "/"

	// csrfTokenBytes 令牌的随机字节数（hex 编码后长度翻倍）。
	//
	// 32 字节 = 256 位，与 session id 同量级。这里只需「不可猜」，
	// 不需要抗离线暴力破解 —— 每次请求都要与服务端下发的值比对，
	// 攻击者没有离线试错的余地。
	csrfTokenBytes = 32
)

// Set 按当前配置下发 access / refresh cookie，以及供前端守卫使用的登录态标记。
//
// 当 security.token_transport = header 时**什么都不做** —— 那是回滚开关，
// 回到「只认 Authorization 头」的改造前行为，连 cookie 都不该发出去。
func Set(c *gin.Context, accessToken, refreshToken string) {
	if !config.Cfg.Security.IssuesCookies() {
		return
	}
	if accessToken != "" {
		writeCookie(c, AccessCookieName, accessToken, AccessCookiePath, config.Cfg.JWT.AccessExpire, true)
	}
	if refreshToken != "" {
		writeCookie(c, RefreshCookieName, refreshToken, RefreshCookiePath, config.Cfg.JWT.RefreshExpire, true)

		// 登录态标记的有效期必须跟着 **refresh** token 走，不能跟着 access token。
		//
		// 理由是它与 B4 自动续期的配合：access token 只有 2 小时
		// （jwt.access_expire: 7200），若标记也 2 小时就失效，用户刷新页面时
		// 路由守卫会在**发出任何请求之前**就判定「未登录」并跳登录页 ——
		// B4 那套「401 → 续期 → 重放」根本没有机会执行。
		// 跟着 refresh token（7 天）走，守卫才会放行到 /auth/userInfo，
		// 由那里的 401 触发续期，用户全程无感。
		//
		// 代价是「标记还在但 access token 已过期」成为常态 —— 这是设计允许的：
		// 标记本来就不代表会话有效（见 LoginFlagCookieName 的注释）。
		writeCookie(c, LoginFlagCookieName, LoginFlagCookieValue,
			LoginFlagCookiePath, config.Cfg.JWT.RefreshExpire, false)

		// CSRF 令牌与 refresh token 同寿命：会话没了，令牌也就没有意义。
		//
		// 取值「已带则沿用、没带才生成」，理由见 csrfTokenFor。
		// 生成失败时返回空串，此时**不下发**该 cookie（fail-closed：
		// 前端拿不到令牌，后续非 GET 请求会被中间件拒绝，而不是放行）。
		if token := csrfTokenFor(c); token != "" {
			writeCookie(c, CSRFCookieName, token, CSRFCookiePath, config.Cfg.JWT.RefreshExpire, false)
		}
	}
}

// Clear 清除认证 cookie（登出时调用）。
//
// MaxAge 用 -1（等价于 Max-Age=0 + 立即过期）。Path 必须与下发时**完全一致**，
// 否则浏览器会认为这是另一个 cookie，旧的仍然留着。
//
// 这里**不判断** IssuesCookies()：header 模式下也照发清除指令。
// 回滚到 header 时，浏览器里可能残留着上一版发出的 cookie，
// 让登出把它们一并清掉比留着更安全（清一个不存在的 cookie 无副作用）。
func Clear(c *gin.Context) {
	clearCookie(c, AccessCookieName, AccessCookiePath, true)
	clearCookie(c, RefreshCookieName, RefreshCookiePath, true)
	// 登录态标记也必须清 —— 漏了它，用户登出后刷新页面会被标记骗回
	// 「已登录」分支，白拉一次 userInfo 再被踢一次，且中间会闪一下空白布局。
	clearCookie(c, LoginFlagCookieName, LoginFlagCookiePath, false)
	// CSRF 令牌一并清掉：留着它虽然不构成漏洞（没有会话可被利用），
	// 但会让「登出后 cookie 里还剩一个安全相关的值」看起来像漏清，
	// 下一次登录也会把它当成「已带」而沿用，掩盖掉本应重新生成的路径。
	clearCookie(c, CSRFCookieName, CSRFCookiePath, false)
}

// csrfTokenFor 返回本次应当下发的 CSRF 令牌：请求已带则**沿用**，否则新生成。
//
// 为什么沿用而不是每次登录/续期都换一个新的：续期发生在**并发请求的中间**。
// 若续期轮换令牌，那些「已经读好 cookie、拼好请求头，但还没发出去」的请求
// 会带着旧值，而浏览器真正发出时 cookie 已经变成新值 —— 头与 cookie 不一致，
// 于是被 403。窗口很窄但真实存在，且现象是「偶发某一个请求失败」，极难定位。
// 沿用旧值把这个竞态整个消除，且安全性上没有任何损失：令牌只承担
// 「与 cookie 同源可比对」的职责，不负责标识会话新鲜度（那是 refresh token 的事）。
//
// 生成失败（crypto/rand 不可用）时返回空串，调用方**不下发**该 cookie。
// 方向必须是安全的：宁可让前端拿不到令牌（后续非 GET 请求被拒），
// 也不能退回一个可预测的值 —— 那会让 double-submit 退化成「填什么都通过」。
func csrfTokenFor(c *gin.Context) string {
	if v, err := c.Cookie(CSRFCookieName); err == nil && v != "" {
		return v
	}

	buf := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		logger.Log.Errorf("[authcookie] 生成 CSRF 令牌失败，本次不下发该 cookie（写请求将被拒绝）: %v", err)
		return ""
	}
	return hex.EncodeToString(buf)
}

// RefreshFromRequest 取出本次请求携带的 refresh token。
//
// 优先用请求体里的（Swagger / 脚本等显式传参的调用方），
// 其次用 cookie（浏览器）。两者都没有时返回空串 —— 调用方按「无需吊销」处理。
func RefreshFromRequest(c *gin.Context, fromBody string) string {
	if fromBody != "" {
		return fromBody
	}
	if v, err := c.Cookie(RefreshCookieName); err == nil {
		return v
	}
	return ""
}

// writeCookie 下发单个认证 cookie。
//
// MaxAge 一律从 jwt.access_expire / jwt.refresh_expire 换算，**不写死**：
// 写死会让「改了 token 有效期但 cookie 还是老时长」这种不一致只能靠用户
// 莫名其妙被登出/拿到过期 cookie 才发现。
//
// httpOnly 由调用方决定，且**只有登录态标记会传 false**：
// token 传 true（这是本项改造的唯一真正收益），标记传 false（JS 要读它）。
// 用显式参数而不是「按 cookie 名判断」：后者会在改名时静默失效，
// 把 token 变成 JS 可读 —— 而这种退化在功能上完全看不出来。
func writeCookie(c *gin.Context, name, value, path string, maxAgeSeconds int64, httpOnly bool) {
	sec := config.Cfg.Security
	c.SetSameSite(sec.SameSite())
	c.SetCookie(
		name,
		value,
		int(maxAgeSeconds),
		path,
		"", // Domain 留空 = 仅当前主机，不扩大到子域
		sec.CookieSecureEnabled(),
		httpOnly,
	)
}

// clearCookie 下发一个立即过期的同名同 Path cookie。
//
// httpOnly 需要与下发时一致才能体现「这是同一个 cookie 的清除指令」，
// 虽然浏览器匹配 cookie 时并不比较该属性，但保持一致能让读代码的人
// 一眼看出这两处指的是同一个东西（历史上就出过「用别的 Path 去清」的 bug）。
func clearCookie(c *gin.Context, name, path string, httpOnly bool) {
	sec := config.Cfg.Security
	c.SetSameSite(sec.SameSite())
	c.SetCookie(name, "", -1, path, "", sec.CookieSecureEnabled(), httpOnly)
}
