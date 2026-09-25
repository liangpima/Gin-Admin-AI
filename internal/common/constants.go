package common

const (
	StatusEnabled  = 1
	StatusDisabled = 0

	MenuTypeDir    = 0
	MenuTypeMenu   = 1
	MenuTypeButton = 2

	SuperAdminID = 1

	ContextKeyTenantID = "tenant_id"
	ContextKeyUserID   = "user_id"
	ContextKeyUsername = "username"
	ContextKeyRoles    = "roles"
	ContextKeyDeptID   = "dept_id"

	// ContextKeyTokenSource 记录本次请求的 token 来自哪里（"header" / "cookie"）。
	//
	// 两个用途：
	//   · 排查「为什么这个请求 401」时，「token 来自头还是 cookie」是最关键的一条信息
	//   · CSRF 校验只对**走 cookie 认证**的请求生效（走 Authorization 头的调用方
	//     不会被浏览器自动带上凭据，天然免疫 CSRF，必须豁免）
	ContextKeyTokenSource = "token_source"

	// ContextKeyAccessToken 保存本次请求**实际使用**的 access token 原文。
	//
	// 登出需要拿它去拉黑。改造前 Controller 直接切 Authorization 头，
	// 而 cookie 认证的请求根本没有那个头 —— 由 Auth 中间件统一取好放进上下文，
	// Controller 就不必再关心 token 是怎么传上来的。
	ContextKeyAccessToken = "access_token"

	// TokenSourceHeader / TokenSourceCookie 是 ContextKeyTokenSource 的取值。
	TokenSourceHeader = "header"
	TokenSourceCookie = "cookie"

	HeaderTenantID = "X-Tenant-Id"
	HeaderUserID   = "X-User-Id"
)
