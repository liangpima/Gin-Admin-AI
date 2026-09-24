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
