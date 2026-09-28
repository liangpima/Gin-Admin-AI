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

// clientIPOf 采集客户端 IP，并做与项目其余各处一致的归一化。
//
// 为什么必须归一化（这是一个真实的登录阻断缺陷）：
// `common.NormalizeIP` 把 `::1` / `::ffff:127.0.0.1` 统一成 `127.0.0.1`。
// 登录侧 `auth_controller.go` 写入 `dto.LoginContext.IP` 时用的是**归一化后**的值，
// 而验证码侧此前用的是 `c.ClientIP()` 原始值 —— 二者被写进同一个一次性凭证
// （`captcha:verified:<token>` 的 `"1|<ip>"`）并在登录时做**等值比对**。
//
// 于是 localhost / IPv6 反代 / Windows 浏览器把 localhost 解析成 ::1 的客户端：
// 验证码侧存 "::1"、登录侧传 "127.0.0.1" → 比对失败，且失败分支在 Del 之前返回，
// 凭证不被消费，TTL 内怎么重试都失败 —— 这类客户端**登录 100% 失败**。
//
// 放在 Controller 而不是 Service：Service 刻意不依赖 gin（见 CaptchaService 注释），
// IP 的采集与形态归一化属于接入层职责。
func clientIPOf(c *gin.Context) string {
	return common.NormalizeIP(c.ClientIP())
}

// @Summary 生成点选验证码
// @Tags 验证码
// @Produce json
// @Success 200 {object} common.Response
// @Router /captcha/generate [get]
func (ctl *CaptchaController) Generate(c *gin.Context) {
	resp, err := ctl.captchaService.Generate(clientIPOf(c))
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

	resp, err := ctl.captchaService.Verify(clientIPOf(c), req.Token, req.Points)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, resp)
}
