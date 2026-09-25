package controller

import (
	"errors"
	"io"

	"go-admin/internal/authcookie"
	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/service"

	"github.com/gin-gonic/gin"
)

type AuthController struct {
	authService service.AuthService
	logService  service.LogService
}

func NewAuthController() *AuthController {
	return &AuthController{
		authService: service.NewAuthService(),
		logService:  service.NewLogService(),
	}
}

// @Summary 用户登录
// @Tags 认证
// @Accept json
// @Produce json
// @Param body body dto.LoginRequest true "登录参数"
// @Success 200 {object} common.Response{data=vo.LoginResponse}
// @Router /auth/login [post]
func (ctl *AuthController) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	// Controller 只做两件事：取参、把 HTTP 侧信息（IP/UA）交给 Service。
	// 人机校验、限频、锁定判定、登录日志都已在 authService.Login 内完成（规则 1）。
	resp, err := ctl.authService.Login(&req, &dto.LoginContext{
		IP:        common.NormalizeIP(c.ClientIP()),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		common.FailWith(c, err)
		return
	}

	// 登录成功顺手把两个 token 写进 HttpOnly cookie（B1：前端一行不改，
	// 两条路径并存，靠 security.token_transport 决定发不发）。
	// 响应体里照旧返回 token —— Swagger / curl / 第三方调用方不会自动带 cookie，
	// 那条路径不能断。
	authcookie.Set(c, resp.AccessToken, resp.RefreshToken)

	common.Success(c, resp)
}

// @Summary 刷新Token
// @Tags 认证
// @Accept json
// @Produce json
// @Param body body dto.RefreshTokenRequest false "RefreshToken（浏览器路径下可省略，凭据在 HttpOnly cookie 里）"
// @Success 200 {object} common.Response{data=vo.LoginResponse}
// @Router /auth/refresh [post]
func (ctl *AuthController) RefreshToken(c *gin.Context) {
	var req dto.RefreshTokenRequest
	// 浏览器路径下请求体是**空**的（refresh token 在 HttpOnly cookie 里，
	// JS 读不到也就传不了），所以「空 body」不是错误，只有「坏 JSON」才是。
	// 用 errors.Is(err, io.EOF) 把两者分开：少了这个区分，B2 之后所有浏览器端的
	// 自动续期都会在这里直接 400，而现象是「token 一过期就被踢出登录」。
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	// 凭据来源：请求体优先（Swagger / 脚本显式传参），其次 HttpOnly cookie（浏览器）。
	// 与 Logout 用同一套取值逻辑，避免两条路径对「凭据从哪来」有不同理解。
	req.RefreshToken = authcookie.RefreshFromRequest(c, req.RefreshToken)
	if req.RefreshToken == "" {
		// 用 Controller 自己的错误出口（而不是 FailWith）：这是参数层面的缺失，
		// 不是 Service 抛上来的业务错误。
		common.Error(c, common.CodeBadRequest, "缺少 refresh token")
		return
	}

	resp, err := ctl.authService.RefreshToken(&req)
	if err != nil {
		// 用 FailWith 收口，而不是把 err.Error() 直接透出：
		// 业务错误（token 无效/过期）按其 401 返回，前端据此清会话跳登录页；
		// 系统错误（Redis 故障等）回 500 + 通用文案。
		// 早前 `common.Error(c, CodeUnauthorized, err.Error())` 会把
		// "存储refresh token失败: dial tcp ..." 这类实现细节回给客户端。
		common.FailWith(c, err)
		return
	}

	// 刷新时后端是**轮换式**的（旧 refresh token 已从 Redis 删掉），
	// 所以 cookie 必须一起换新 —— 只更新响应体的话，浏览器手里还是旧的那个，
	// 下次续期就会拿着已被消费的 token 换来 401。
	authcookie.Set(c, resp.AccessToken, resp.RefreshToken)

	common.Success(c, resp)
}

// @Summary 退出登录
// @Tags 认证
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} common.Response
// @Router /auth/logout [post]
func (ctl *AuthController) Logout(c *gin.Context) {
	// 旧客户端不传请求体，绑定失败属预期，忽略
	var req dto.LogoutRequest
	_ = c.ShouldBindJSON(&req)

	// access token 由 Auth 中间件取好放进上下文 —— 不能再切 Authorization 头，
	// 因为 cookie 认证的请求根本没有那个头（那样会变成「拉黑空串」，
	// 看着登出成功，实际什么都没吊销）。
	accessToken, _ := c.Get(common.ContextKeyAccessToken)
	accessTokenStr, _ := accessToken.(string)

	// refresh token 优先用请求体（Swagger / 脚本显式传参），其次用 cookie（浏览器）。
	// 必须吊销它 —— 只拉黑 access token 的话，持有 refresh token 的人
	// 仍可换发新 access token，等于没登出。
	refreshToken := authcookie.RefreshFromRequest(c, req.RefreshToken)

	if err := ctl.authService.LogoutByToken(accessTokenStr, refreshToken); err != nil {
		common.FailWith(c, err)
		return
	}

	// 服务端吊销完再清 cookie：反过来的话，中途失败会让浏览器以为已登出、
	// 而服务端的 refresh token 还活着。
	authcookie.Clear(c)

	common.Success(c, nil)
}

// @Summary 获取用户信息
// @Tags 认证
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} common.Response{data=vo.UserInfoResponse}
// @Router /auth/userInfo [get]
func (ctl *AuthController) GetUserInfo(c *gin.Context) {
	userID := common.GetCurrentUserID(c)
	if userID == 0 {
		common.Unauthorized(c, "未登录")
		return
	}

	resp, err := ctl.authService.GetUserInfo(userID)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, resp)
}

