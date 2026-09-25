package dto

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	// CaptchaToken /captcha/verify 校验通过后返回的一次性凭证。
	// 必填：留空意味着攻击者可直接 POST /auth/login 绕过人机校验。
	CaptchaToken string `json:"captchaToken" binding:"required"`
}

// LoginContext 登录请求的 HTTP 侧上下文。
//
// IP 与 User-Agent 来自请求，但它们同时是**业务判定依据**：
// IP 参与限频键、UA 要解析成登录日志的浏览器/系统字段。
// 由 Controller 采集后传入 Service，Service 就不必依赖 gin（规则 2）。
type LoginContext struct {
	// IP 已经过 NormalizeIP 归一化
	IP        string
	UserAgent string
}

type RefreshTokenRequest struct {
	// RefreshToken 待刷新的 refresh token。
	//
	// **刻意不是 `binding:"required"`**（P3-B2 起）：浏览器路径下它由 HttpOnly
	// cookie 携带，而前端 JS 读不到 HttpOnly cookie，也就**不可能**把它放进请求体 ——
	// 请求体是空的。真正「有没有凭据」的判定在 Controller 里用
	// authcookie.RefreshFromRequest 统一做（请求体优先、其次 cookie，
	// 两者都取不到才拒绝）。
	//
	// 保留该字段是为了不破坏 Swagger / curl / 第三方这类显式传参的调用方。
	// 若哪天有人"顺手"把 required 加回来，现象是「浏览器端 token 一过期就被
	// 踢到登录页」—— 与 B4 要修的那个缺陷一模一样，极难怀疑到参数绑定这一层。
	RefreshToken string `json:"refreshToken"`
}

type LogoutRequest struct {
	// RefreshToken 待吊销的 refresh token。
	// 登出必须连带吊销它 —— 只拉黑 access token 的话，
	// 持有 refresh token 的人仍可换发新 access token，等于没登出。
	RefreshToken string `json:"refreshToken"`
}
