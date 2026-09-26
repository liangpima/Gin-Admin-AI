package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/service"

	"github.com/gin-gonic/gin"
)

type MemberController struct {
	memberService service.MemberService
	levelService  service.MemberLevelService
	tagService    service.MemberTagService
}

func NewMemberController() *MemberController {
	return &MemberController{
		memberService: service.NewMemberService(),
		levelService:  service.NewMemberLevelService(),
		tagService:    service.NewMemberTagService(),
	}
}

// @Summary 创建会员
// @Tags 会员
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.CreateMemberRequest true "会员信息"
// @Success 200 {object} common.Response
// @Router /member [post]
func (ctl *MemberController) Create(c *gin.Context) {
	var req dto.CreateMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	operatorID := common.GetCurrentUserID(c)
	tenantID := common.GetTenantID(c)

	if err := ctl.memberService.Create(&req, operatorID, tenantID); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 更新会员
// @Tags 会员
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.UpdateMemberRequest true "会员信息（含 id）"
// @Success 200 {object} common.Response
// @Router /member [put]
func (ctl *MemberController) Update(c *gin.Context) {
	var req dto.UpdateMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	operatorID := common.GetCurrentUserID(c)
	tenantID := common.GetTenantID(c)
	if err := ctl.memberService.Update(&req, operatorID, tenantID); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 删除会员
// @Tags 会员
// @Produce json
// @Security BearerAuth
// @Param id path int true "会员 ID"
// @Success 200 {object} common.Response
// @Router /member/{id} [delete]
func (ctl *MemberController) Delete(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	tenantID := common.GetTenantID(c)
	if err := ctl.memberService.Delete(tenantID, id); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 会员详情
// @Tags 会员
// @Produce json
// @Security BearerAuth
// @Param id path int true "会员 ID"
// @Success 200 {object} common.Response
// @Router /member/{id} [get]
func (ctl *MemberController) FindByID(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	tenantID := common.GetTenantID(c)
	member, err := ctl.memberService.FindByID(tenantID, id)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, member)
}

// @Summary 会员列表
// @Tags 会员
// @Produce json
// @Security BearerAuth
// @Param phone query string false "手机号"
// @Param nickname query string false "昵称（模糊）"
// @Param levelId query int false "等级 ID"
// @Param status query int false "状态：0 停用 / 1 正常"
// @Param page query int false "页码"
// @Param pageSize query int false "每页条数"
// @Success 200 {object} common.Response
// @Router /member/list [get]
func (ctl *MemberController) FindList(c *gin.Context) {
	var req dto.MemberListRequest
	if err := common.BindPage(c, &req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	tenantID := common.GetTenantID(c)
	list, total, err := ctl.memberService.FindList(tenantID, &req)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.SuccessWithPage(c, list, total, req.Page, req.PageSize)
}

// @Summary 启用 / 停用会员
// @Tags 会员
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.UpdateMemberStatusRequest true "会员 ID 与目标状态"
// @Success 200 {object} common.Response
// @Router /member/status [put]
func (ctl *MemberController) UpdateStatus(c *gin.Context) {
	var req dto.UpdateMemberStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	tenantID := common.GetTenantID(c)
	if err := ctl.memberService.UpdateStatus(tenantID, &req); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 设置会员标签
// @Tags 会员
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.UpdateMemberTagsRequest true "会员 ID 与标签 ID 列表"
// @Success 200 {object} common.Response
// @Router /member/tags [put]
func (ctl *MemberController) UpdateTags(c *gin.Context) {
	var req dto.UpdateMemberTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	tenantID := common.GetTenantID(c)
	if err := ctl.memberService.UpdateTags(tenantID, &req); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 全部会员等级（不分页）
// @Description 给下拉选项用；`/member/level/list` 是分页版
// @Tags 会员等级
// @Produce json
// @Security BearerAuth
// @Success 200 {object} common.Response
// @Router /member/level/all [get]
func (ctl *MemberController) FindAllLevels(c *gin.Context) {
	tenantID := common.GetTenantID(c)
	levels, err := ctl.levelService.FindAll(tenantID)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, levels)
}

// @Summary 全部会员标签（不分页）
// @Description 给下拉选项用；`/member/tag/list` 是分页版
// @Tags 会员标签
// @Produce json
// @Security BearerAuth
// @Success 200 {object} common.Response
// @Router /member/tag/all [get]
func (ctl *MemberController) FindAllTags(c *gin.Context) {
	tenantID := common.GetTenantID(c)
	tags, err := ctl.tagService.FindAll(tenantID)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, tags)
}

// @Summary 更新最后访问时间
// @Tags 会员
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id body int true "会员 ID"
// @Success 200 {object} common.Response
// @Router /member/visit [put]
func (ctl *MemberController) UpdateLastVisit(c *gin.Context) {
	var req struct {
		ID uint `json:"id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	tenantID := common.GetTenantID(c)
	if err := ctl.memberService.UpdateLastVisit(tenantID, req.ID); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}
