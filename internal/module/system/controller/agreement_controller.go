package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/system/service"

	"github.com/gin-gonic/gin"
)

type AgreementController struct {
	agreementService service.AgreementService
}

func NewAgreementController() *AgreementController {
	return &AgreementController{agreementService: service.NewAgreementService()}
}

// @Summary 创建协议
// @Tags 协议
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param title body string true "标题"
// @Param content body string false "正文"
// @Param type body string true "协议类型"
// @Param sort body int false "排序"
// @Param status body int false "状态：0 停用 / 1 正常"
// @Success 200 {object} common.Response
// @Router /system/agreement [post]
func (ctl *AgreementController) Create(c *gin.Context) {
	var req struct {
		Title   string `json:"title" binding:"required"`
		Content string `json:"content"`
		Type    string `json:"type" binding:"required"`
		Sort    int    `json:"sort"`
		Status  int8   `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	if err := ctl.agreementService.Create(req.Title, req.Content, req.Type, req.Sort, req.Status,
			common.GetCurrentUserID(c), common.GetTenantID(c)); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 更新协议
// @Tags 协议
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id body int true "协议 ID"
// @Param title body string true "标题"
// @Param content body string false "正文"
// @Param type body string true "协议类型"
// @Param sort body int false "排序"
// @Param status body int false "状态：0 停用 / 1 正常"
// @Success 200 {object} common.Response
// @Router /system/agreement [put]
func (ctl *AgreementController) Update(c *gin.Context) {
	var req struct {
		ID      uint   `json:"id" binding:"required"`
		Title   string `json:"title" binding:"required"`
		Content string `json:"content"`
		Type    string `json:"type" binding:"required"`
		Sort    int    `json:"sort"`
		Status  int8   `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	if err := ctl.agreementService.Update(req.ID, req.Title, req.Content, req.Type, req.Sort, req.Status,
			common.GetCurrentUserID(c), common.GetTenantID(c)); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 删除协议
// @Tags 协议
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} common.Response
// @Router /system/agreement/{id} [delete]
func (ctl *AgreementController) Delete(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	if err := ctl.agreementService.Delete(common.GetTenantID(c), id); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 协议列表
// @Tags 协议
// @Produce json
// @Security BearerAuth
// @Param name query string false "标题（模糊）"
// @Param type query string false "协议类型"
// @Param status query int false "状态"
// @Param page query int false "页码"
// @Param pageSize query int false "每页条数"
// @Success 200 {object} common.Response
// @Router /system/agreement/list [get]
func (ctl *AgreementController) FindList(c *gin.Context) {
	var req struct {
		Name     string `form:"name"`
		Type     string `form:"type"`
		Status   *int8  `form:"status"`
		// 分页参数统一内嵌：绑定与归一化走 common.BindPage
		common.PageQuery
	}
	if err := common.BindPage(c, &req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	list, total, err := ctl.agreementService.FindList(common.GetTenantID(c), req.Name, req.Type, req.Status, req.Page, req.PageSize)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.SuccessWithPage(c, list, total, req.Page, req.PageSize)
}

// @Summary 按类型取协议
// @Description 前端协议页用；同类型取排序最靠前的一条
// @Tags 协议
// @Produce json
// @Security BearerAuth
// @Param type path string true "协议类型"
// @Success 200 {object} common.Response
// @Router /system/agreement/type/{type} [get]
func (ctl *AgreementController) FindByType(c *gin.Context) {
	typ := c.Param("type")
	if typ == "" {
		common.Error(c, common.CodeBadRequest, "类型不能为空")
		return
	}
	agreement, err := ctl.agreementService.FindByType(common.GetTenantID(c), typ)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, agreement)
}
