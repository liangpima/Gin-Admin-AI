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

func (ctl *MemberController) FindAllLevels(c *gin.Context) {
	tenantID := common.GetTenantID(c)
	levels, err := ctl.levelService.FindAll(tenantID)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, levels)
}

func (ctl *MemberController) FindAllTags(c *gin.Context) {
	tenantID := common.GetTenantID(c)
	tags, err := ctl.tagService.FindAll(tenantID)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, tags)
}

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
