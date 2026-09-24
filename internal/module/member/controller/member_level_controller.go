package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/service"

	"github.com/gin-gonic/gin"
)

type MemberLevelController struct {
	levelService service.MemberLevelService
}

func NewMemberLevelController() *MemberLevelController {
	return &MemberLevelController{levelService: service.NewMemberLevelService()}
}

// @Summary 创建会员等级
// @Tags 会员等级
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.CreateMemberLevelRequest true "等级信息"
// @Success 200 {object} common.Response
// @Router /member/level [post]
func (ctl *MemberLevelController) Create(c *gin.Context) {
	var req dto.CreateMemberLevelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	operatorID := common.GetCurrentUserID(c)
	tenantID := common.GetTenantID(c)
	if err := ctl.levelService.Create(&req, operatorID, tenantID); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 更新会员等级
// @Tags 会员等级
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.UpdateMemberLevelRequest true "等级信息"
// @Success 200 {object} common.Response
// @Router /member/level [put]
func (ctl *MemberLevelController) Update(c *gin.Context) {
	var req dto.UpdateMemberLevelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	operatorID := common.GetCurrentUserID(c)
	tenantID := common.GetTenantID(c)
	if err := ctl.levelService.Update(&req, operatorID, tenantID); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 删除会员等级
// @Tags 会员等级
// @Produce json
// @Security BearerAuth
// @Param id path int true "等级ID"
// @Success 200 {object} common.Response
// @Router /member/level/{id} [delete]
func (ctl *MemberLevelController) Delete(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	tenantID := common.GetTenantID(c)
	if err := ctl.levelService.Delete(tenantID, id); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 等级列表
// @Tags 会员等级
// @Produce json
// @Security BearerAuth
// @Param name query string false "等级名称"
// @Param page query int true "页码"
// @Param pageSize query int true "每页条数"
// @Success 200 {object} common.Response
// @Router /member/level/list [get]
func (ctl *MemberLevelController) FindList(c *gin.Context) {
	var req dto.MemberLevelListRequest
	if err := common.BindPage(c, &req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	tenantID := common.GetTenantID(c)
	list, total, err := ctl.levelService.FindList(tenantID, &req)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.SuccessWithPage(c, list, total, req.Page, req.PageSize)
}
