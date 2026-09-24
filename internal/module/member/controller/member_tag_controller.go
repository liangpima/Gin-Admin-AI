package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/service"

	"github.com/gin-gonic/gin"
)

type MemberTagController struct {
	tagService service.MemberTagService
}

func NewMemberTagController() *MemberTagController {
	return &MemberTagController{tagService: service.NewMemberTagService()}
}

// @Summary 创建会员标签
// @Tags 会员标签
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.CreateMemberTagRequest true "标签信息"
// @Success 200 {object} common.Response
// @Router /member/tag [post]
func (ctl *MemberTagController) Create(c *gin.Context) {
	var req dto.CreateMemberTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	operatorID := common.GetCurrentUserID(c)
	tenantID := common.GetTenantID(c)
	if err := ctl.tagService.Create(&req, operatorID, tenantID); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 更新会员标签
// @Tags 会员标签
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.UpdateMemberTagRequest true "标签信息"
// @Success 200 {object} common.Response
// @Router /member/tag [put]
func (ctl *MemberTagController) Update(c *gin.Context) {
	var req dto.UpdateMemberTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	operatorID := common.GetCurrentUserID(c)
	tenantID := common.GetTenantID(c)
	if err := ctl.tagService.Update(&req, operatorID, tenantID); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 删除会员标签
// @Tags 会员标签
// @Produce json
// @Security BearerAuth
// @Param id path int true "标签ID"
// @Success 200 {object} common.Response
// @Router /member/tag/{id} [delete]
func (ctl *MemberTagController) Delete(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	tenantID := common.GetTenantID(c)
	if err := ctl.tagService.Delete(tenantID, id); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 标签列表
// @Tags 会员标签
// @Produce json
// @Security BearerAuth
// @Param name query string false "标签名称"
// @Param page query int true "页码"
// @Param pageSize query int true "每页条数"
// @Success 200 {object} common.Response
// @Router /member/tag/list [get]
func (ctl *MemberTagController) FindList(c *gin.Context) {
	var req dto.MemberTagListRequest
	if err := common.BindPage(c, &req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	tenantID := common.GetTenantID(c)
	list, total, err := ctl.tagService.FindList(tenantID, &req)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.SuccessWithPage(c, list, total, req.Page, req.PageSize)
}
