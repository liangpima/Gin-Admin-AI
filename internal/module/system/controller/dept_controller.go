package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/service"

	"github.com/gin-gonic/gin"
)

type DeptController struct {
	deptService service.DeptService
}

func NewDeptController() *DeptController {
	return &DeptController{
		deptService: service.NewDeptService(),
	}
}

// @Summary 创建部门
// @Tags 部门
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.CreateDeptRequest true "部门信息"
// @Success 200 {object} common.Response
// @Router /system/dept [post]
func (ctl *DeptController) Create(c *gin.Context) {
	var req dto.CreateDeptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	operatorID := common.GetCurrentUserID(c)
	tenantID := common.GetTenantID(c)
	if err := ctl.deptService.Create(&req, operatorID, tenantID); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 更新部门
// @Tags 部门
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.UpdateDeptRequest true "部门信息（含 id）"
// @Success 200 {object} common.Response
// @Router /system/dept [put]
func (ctl *DeptController) Update(c *gin.Context) {
	var req dto.UpdateDeptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	operatorID := common.GetCurrentUserID(c)
	tenantID := common.GetTenantID(c)
	if err := ctl.deptService.Update(&req, operatorID, tenantID); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 删除部门
// @Tags 部门
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} common.Response
// @Router /system/dept/{id} [delete]
func (ctl *DeptController) Delete(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}

	// service 已用 BizError 标记「存在下级部门」，FailWith 会自动给出 400；
	// 其余（删除失败等）为系统错误，返回 500
	if err := ctl.deptService.Delete(common.GetTenantID(c), id); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 部门详情
// @Tags 部门
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} common.Response
// @Router /system/dept/{id} [get]
func (ctl *DeptController) FindByID(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}

	dept, err := ctl.deptService.FindByID(common.GetTenantID(c), id)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, dept)
}

// @Summary 部门树
// @Description 整棵返回、不分页；按租户过滤
// @Tags 部门
// @Produce json
// @Security BearerAuth
// @Success 200 {object} common.Response
// @Router /system/dept/tree [get]
func (ctl *DeptController) FindTree(c *gin.Context) {
	depts, err := ctl.deptService.FindTree(common.GetTenantID(c))
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, depts)
}
