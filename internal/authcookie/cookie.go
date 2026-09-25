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
	"github.com/gin-gonic/gin"

	"go-admin/config"
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
)

// Set 按当前配置下发 access / refresh cookie。
//
// 当 security.token_transport = header 时**什么都不做** —— 那是回滚开关，
// 回到「只认 Authorization 头」的改造前行为，连 cookie 都不该发出去。
func Set(c *gin.Context, accessToken, refreshToken string) {
	if !config.Cfg.Security.IssuesCookies() {
		return
	}
	if accessToken != "" {
		writeCookie(c, AccessCookieName, accessToken, AccessCookiePath, config.Cfg.JWT.AccessExpire)
	}
	if refreshToken != "" {
		writeCookie(c, RefreshCookieName, refreshToken, RefreshCookiePath, config.Cfg.JWT.RefreshExpire)
	}
}

// Clear 清除认证 cookie（登出时调用）。
//
// MaxAge 用 -1（等价于 Max-Age=0 + 立即过期）。Path 必须与下发时**完全一致**，
// 否则浏览器会认为这是另一个 cookie，旧的仍然留着。
func Clear(c *gin.Context) {
	clearCookie(c, AccessCookieName, AccessCookiePath)
	clearCookie(c, RefreshCookieName, RefreshCookiePath)
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
func writeCookie(c *gin.Context, name, value, path string, maxAgeSeconds int64) {
	sec := config.Cfg.Security
	c.SetSameSite(sec.SameSite())
	c.SetCookie(
		name,
		value,
		int(maxAgeSeconds),
		path,
		"", // Domain 留空 = 仅当前主机，不扩大到子域
		sec.CookieSecureEnabled(),
		true, // HttpOnly：本项改造的**唯一真正收益**，token 从此不在 JS 可达范围内
	)
}

// clearCookie 下发一个立即过期的同名同 Path cookie。
func clearCookie(c *gin.Context, name, path string) {
	sec := config.Cfg.Security
	c.SetSameSite(sec.SameSite())
	c.SetCookie(name, "", -1, path, "", sec.CookieSecureEnabled(), true)
}
