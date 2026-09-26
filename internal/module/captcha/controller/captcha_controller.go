package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/captcha/model"
	"go-admin/internal/module/captcha/service"

	"github.com/gin-gonic/gin"
)

type CaptchaController struct {
	captchaService service.CaptchaService
}

func NewCaptchaController() *CaptchaController {
	return &CaptchaController{captchaService: service.NewCaptchaService()}
}

// @Summary 生成点选验证码
// @Tags 验证码
// @Produce json
// @Success 200 {object} common.Response
// @Router /captcha/generate [get]
func (ctl *CaptchaController) Generate(c *gin.Context) {
	resp, err := ctl.captchaService.Generate(c.ClientIP())
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, resp)
}

// @Summary 校验点选验证码
// @Tags 验证码
// @Accept json
// @Produce json
// @Param body body model.CaptchaVerifyRequest true "验证码 token 与点击坐标"
// @Success 200 {object} common.Response
// @Router /captcha/verify [post]
func (ctl *CaptchaController) Verify(c *gin.Context) {
	var req model.CaptchaVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}

	resp, err := ctl.captchaService.Verify(c.ClientIP(), req.Token, req.Points)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, resp)
}
