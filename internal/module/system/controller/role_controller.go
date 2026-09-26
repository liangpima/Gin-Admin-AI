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

// @Summary 创建角色
// @Tags 角色
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.CreateRoleRequest true "角色信息"
// @Success 200 {object} common.Response
// @Router /system/role [post]
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

// @Summary 更新角色
// @Tags 角色
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.UpdateRoleRequest true "角色信息（含 id）"
// @Success 200 {object} common.Response
// @Router /system/role [put]
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

// @Summary 删除角色
// @Tags 角色
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} common.Response
// @Router /system/role/{id} [delete]
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

// @Summary 角色详情
// @Tags 角色
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} common.Response
// @Router /system/role/{id} [get]
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

// @Summary 角色列表
// @Tags 角色
// @Produce json
// @Security BearerAuth
// @Param name query string false "角色名称（模糊）"
// @Param status query int false "状态"
// @Param page query int false "页码"
// @Param pageSize query int false "每页条数"
// @Success 200 {object} common.Response
// @Router /system/role/list [get]
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

// @Summary 启用 / 停用角色
// @Tags 角色
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.StatusRequest true "角色 ID 与目标状态"
// @Success 200 {object} common.Response
// @Router /system/role/status [put]
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

// @Summary 全部角色（不分页）
// @Description 给用户编辑页的角色下拉用
// @Tags 角色
// @Produce json
// @Security BearerAuth
// @Success 200 {object} common.Response
// @Router /system/role/all [get]
func (ctl *RoleController) FindAll(c *gin.Context) {
	tenantID := common.GetTenantID(c)
	roles, err := ctl.roleService.FindAll(tenantID)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, roles)
}
