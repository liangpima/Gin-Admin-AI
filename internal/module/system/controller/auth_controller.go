package controller

import (

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
// @Router /api/v1/auth/login [post]
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
	common.Success(c, resp)
}

// @Summary 刷新Token
// @Tags 认证
// @Accept json
// @Produce json
// @Param body body dto.RefreshTokenRequest true "RefreshToken"
// @Success 200 {object} common.Response{data=vo.LoginResponse}
// @Router /api/v1/auth/refresh [post]
func (ctl *AuthController) RefreshToken(c *gin.Context) {
	var req dto.RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
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

	common.Success(c, resp)
}

// @Summary 退出登录
// @Tags 认证
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} common.Response
// @Router /api/v1/auth/logout [post]
func (ctl *AuthController) Logout(c *gin.Context) {
	// 旧客户端不传请求体，绑定失败属预期，忽略
	var req dto.LogoutRequest
	_ = c.ShouldBindJSON(&req)

	// 只负责取 header 与 body；拉黑与吊销都在 Service。
	// 注：历史上这里曾把 access token 当 refresh token 去删（键名对不上），
	// 导致登出后 refresh token 仍可换发新 access token —— 该修复在 Service 内保留。
	accessToken := ""
	if authHeader := c.GetHeader("Authorization"); len(authHeader) > 7 {
		accessToken = authHeader[7:]
	}

	if err := ctl.authService.LogoutByToken(accessToken, req.RefreshToken); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 获取用户信息
// @Tags 认证
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} common.Response{data=vo.UserInfoResponse}
// @Router /api/v1/auth/userInfo [get]
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

