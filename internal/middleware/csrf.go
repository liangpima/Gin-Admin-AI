package middleware

import (
	"crypto/subtle"
	"net/http"

	"go-admin/config"
	"go-admin/internal/authcookie"
	"go-admin/internal/common"
	"go-admin/internal/logger"

	"github.com/gin-gonic/gin"
)

// CSRF 校验「双提交 Cookie」（double-submit）—— CSRF 的纵深防御。
//
// # 它防的是什么
//
// 主防线是 `SameSite=Lax`（见 config.SecurityConfig.CookieSameSite，启动日志会打印
// 当前取值）：浏览器不会把 cookie 带到跨站的 POST 上，绝大多数 CSRF 到不了这里。
// 但主防线有两个失效场景，本中间件就是为它们准备的：
//
//   - 前后端**分域名**部署时只能配 `SameSite=None`（跨站请求会携带 cookie），
//     主防线直接归零
//   - 老浏览器 / 非浏览器客户端（部分嵌入式 WebView）对 SameSite 支持不全
//
// 机制：登录时下发一个**非 HttpOnly** 的随机 `csrf_token`，前端每次非 GET 请求
// 把它放进 `X-CSRF-Token` 头；服务端只校验「头里的值 == cookie 里的值」，
// **不存储任何东西**。攻击者能让浏览器带上 cookie，但读不到 cookie 的值，
// 也就填不出匹配的头（自定义头还会触发 CORS 预检，白名单外直接失败）。
//
// # 为什么必须在 Auth **之后**串联
//
// 豁免规则依赖「本次请求的凭据是从哪来的」，而这个信息由 Auth 中间件写进上下文
// （common.ContextKeyTokenSource）。跑在 Auth 之前就无从判断。
//
// # 豁免规则（按顺序判断，任一命中即放行）
//
//  1. **安全方法**（GET / HEAD / OPTIONS）：只读请求不改状态，无 CSRF 可言。
//     放行它们也是必须的 —— 跨站 <img>/<script> 触发的正是 GET。
//  2. **未下发 cookie**（security.token_transport = header）：此时不存在 csrf cookie，
//     请求只可能走 Authorization 头，下面第 3 条也会放行；这里显式短路是为了
//     让「回滚到 header 模式」保持行为完全一致。
//  3. **凭据来自 Authorization 头**：这是 B1「双读」设计带来的额外好处 ——
//     浏览器不会自动给跨站请求附加自定义头，所以走头的调用方天然免疫 CSRF。
//     非浏览器客户端（Swagger、curl、服务间调用）因此**不需要任何改动**。
//
// 注意第 3 条是「只有来自头才豁免」，而不是「不是 cookie 就豁免」：
// 若写成后者，上下文里没有 source 时会静默放行（fail-open）。
// 方向必须反过来 —— 认不出来的请求也要过校验。
func CSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		if isSafeMethod(c.Request.Method) {
			c.Next()
			return
		}

		if !config.Cfg.Security.IssuesCookies() {
			c.Next()
			return
		}

		if source, _ := c.Get(common.ContextKeyTokenSource); source == common.TokenSourceHeader {
			c.Next()
			return
		}

		cookieToken, err := c.Cookie(authcookie.CSRFCookieName)
		headerToken := c.GetHeader(authcookie.CSRFHeaderName)

		// 三种失败都归到同一分支，且**返回同一句文案**：
		// 告诉调用方「是缺头还是值不对」对排查没帮助（两者都是前端没正确带令牌），
		// 但会把服务端校验细节透给攻击者。真正需要的信息在下面这行日志里。
		if err != nil || cookieToken == "" || headerToken == "" ||
			subtle.ConstantTimeCompare([]byte(cookieToken), []byte(headerToken)) != 1 {
			logger.Log.Warnf(
				"[csrf] 拒绝 cookie 认证的写请求：CSRF 令牌缺失或不匹配（method=%s path=%s user=%d）",
				c.Request.Method, c.FullPath(), common.GetCurrentUserID(c))
			common.Forbidden(c, "请求校验失败，请刷新页面后重试")
			c.Abort()
			return
		}

		c.Next()
	}
}

// isSafeMethod 判断方法是否为「安全方法」（RFC 9110：语义上只读，不改变服务端状态）。
//
// 只列安全方法、其余一律按不安全处理（白名单而非黑名单）：
// 将来出现新方法（或有人手工注册一个奇怪的路由）时，默认落在**需要校验**的一侧。
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}
