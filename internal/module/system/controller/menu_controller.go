package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/service"

	"github.com/gin-gonic/gin"
)

type MenuController struct {
	menuService service.MenuService
}

func NewMenuController() *MenuController {
	return &MenuController{
		menuService: service.NewMenuService(),
	}
}

// @Summary 创建菜单
// @Tags 菜单
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.CreateMenuRequest true "菜单信息"
// @Success 200 {object} common.Response
// @Router /system/menu [post]
func (ctl *MenuController) Create(c *gin.Context) {
	var req dto.CreateMenuRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	operatorID := common.GetCurrentUserID(c)
	if err := ctl.menuService.Create(&req, operatorID); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 更新菜单
// @Tags 菜单
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.UpdateMenuRequest true "菜单信息（含 id）"
// @Success 200 {object} common.Response
// @Router /system/menu [put]
func (ctl *MenuController) Update(c *gin.Context) {
	var req dto.UpdateMenuRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	operatorID := common.GetCurrentUserID(c)
	if err := ctl.menuService.Update(&req, operatorID); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 删除菜单
// @Tags 菜单
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} common.Response
// @Router /system/menu/{id} [delete]
func (ctl *MenuController) Delete(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}

	if err := ctl.menuService.Delete(id); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 菜单详情
// @Tags 菜单
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} common.Response
// @Router /system/menu/{id} [get]
func (ctl *MenuController) FindByID(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}

	menu, err := ctl.menuService.FindByID(id)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, menu)
}

// @Summary 菜单树
// @Description 菜单管理页用；sys_menu 是全局表，不按租户过滤
// @Tags 菜单
// @Produce json
// @Security BearerAuth
// @Success 200 {object} common.Response
// @Router /system/menu/tree [get]
func (ctl *MenuController) FindTree(c *gin.Context) {
	menus, err := ctl.menuService.FindTreeForManage()
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, menus)
}

// @Summary 全部菜单（扁平）
// @Description 给角色授权树、下拉选项用
// @Tags 菜单
// @Produce json
// @Security BearerAuth
// @Success 200 {object} common.Response
// @Router /system/menu/all [get]
func (ctl *MenuController) FindAll(c *gin.Context) {
	menus, err := ctl.menuService.FindAll()
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, menus)
}
