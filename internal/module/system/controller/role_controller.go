package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/service"

	"github.com/gin-gonic/gin"
)

type RoleController struct {
	roleService service.RoleService
}

func NewRoleController() *RoleController {
	return &RoleController{
		roleService: service.NewRoleService(),
	}
}

func (ctl *RoleController) Create(c *gin.Context) {
	var req dto.CreateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	operatorID := common.GetCurrentUserID(c)
	tenantID := common.GetTenantID(c)
	if err := ctl.roleService.Create(&req, operatorID, tenantID); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

func (ctl *RoleController) Update(c *gin.Context) {
	var req dto.UpdateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	operatorID := common.GetCurrentUserID(c)
	tenantID := common.GetTenantID(c)
	if err := ctl.roleService.Update(&req, operatorID, tenantID); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

func (ctl *RoleController) Delete(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}

	tenantID := common.GetTenantID(c)
	if err := ctl.roleService.Delete(tenantID, id); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

func (ctl *RoleController) FindByID(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}

	tenantID := common.GetTenantID(c)
	role, err := ctl.roleService.FindByID(tenantID, id)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, role)
}

func (ctl *RoleController) FindList(c *gin.Context) {
	var req dto.RoleListRequest
	if err := common.BindPage(c, &req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	tenantID := common.GetTenantID(c)
	roles, total, err := ctl.roleService.FindList(tenantID, &req)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.SuccessWithPage(c, roles, total, req.Page, req.PageSize)
}

func (ctl *RoleController) UpdateStatus(c *gin.Context) {
	var req dto.StatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	tenantID := common.GetTenantID(c)
	if err := ctl.roleService.UpdateStatus(tenantID, &req); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

func (ctl *RoleController) FindAll(c *gin.Context) {
	tenantID := common.GetTenantID(c)
	roles, err := ctl.roleService.FindAll(tenantID)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, roles)
}
