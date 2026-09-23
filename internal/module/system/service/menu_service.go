package service

import (
	"context"
	"errors"

	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/logger"
	"go-admin/internal/middleware"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/repository"

	"gorm.io/gorm"
)

type MenuService interface {
	Create(req *dto.CreateMenuRequest, operatorID uint) error
	Update(req *dto.UpdateMenuRequest, operatorID uint) error
	Delete(id uint) error
	FindByID(id uint) (interface{}, error)
	FindAll() ([]model.SysMenu, error)
	FindTree() ([]model.SysMenu, error)
	FindTreeForManage() ([]model.SysMenu, error)
	FindMenusByRoleIDs(roleIDs []uint) ([]model.SysMenu, error)
}

type menuService struct {
	menuRepo repository.MenuRepository
}

func NewMenuService() MenuService {
	return &menuService{
		menuRepo: repository.NewMenuRepository(),
	}
}

func (s *menuService) Create(req *dto.CreateMenuRequest, operatorID uint) error {
	if err := s.ensureParentExists(req.ParentID); err != nil {
		return err
	}

	menu := &model.SysMenu{
		BaseModel: common.BaseModel{
			CreateBy: operatorID,
			UpdateBy: operatorID,
		},
		ParentID:   req.ParentID,
		Name:       req.Name,
		Path:       req.Path,
		Component:  req.Component,
		Redirect:   req.Redirect,
		Icon:       req.Icon,
		Title:      req.Title,
		Type:       req.Type,
		Permission: req.Permission,
		Sort:       req.Sort,
		Visible:    req.Visible,
		Status:     req.Status,
		IsExternal: req.IsExternal,
		IsCache:    req.IsCache,
	}

	if err := s.menuRepo.Create(menu); err != nil {
		return err
	}
	s.syncPolicies()
	return nil
}

// ensureParentExists 校验上级菜单存在（parentID 为 0 表示挂到根）。
//
// 不校验的后果很隐蔽：parent_id 指向一个不存在的 ID 时 INSERT 会成功，
// 但树是从 parent_id=0 出发构建的，这个节点永远不可达 ——
// 表现为「提示创建成功，列表里却找不到」，数据却真实留在库里。
func (s *menuService) ensureParentExists(parentID uint) error {
	if parentID == 0 {
		return nil
	}
	if _, err := s.menuRepo.FindByID(parentID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewBizError("上级菜单不存在")
		}
		return err
	}
	return nil
}

// ErrMenuHasChildren 删除菜单时存在下级菜单。
//
// 与 dept 模块的 ErrDeptHasChildren 对称：用可识别的业务错误暴露出来，
// 让 Controller 归为 400（前置条件不满足），而不是笼统的 500。
var ErrMenuHasChildren = common.NewBizError("存在下级菜单，请先删除下级菜单")

func (s *menuService) Update(req *dto.UpdateMenuRequest, operatorID uint) error {
	menu, err := s.menuRepo.FindByID(req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewNotFoundError("菜单不存在")
		}
		return err
	}

	// ParentID 为 nil 表示本次不移动菜单，父级相关校验一并跳过
	if req.ParentID != nil {
		if err := s.ensureParentExists(*req.ParentID); err != nil {
			return err
		}

		// 禁止把菜单挂到自己或自己的后代之下：会形成环，
		// 该子树将无法从根节点遍历到，等于从界面上消失却仍留在库里
		if cycle, err := hasCycleInHierarchy(req.ID, *req.ParentID, s.menuRepo.FindParentID); err != nil {
			return err
		} else if cycle {
			return common.NewBizError("不能将菜单移动到它自己或它的下级之下")
		}
		menu.ParentID = *req.ParentID
	}

	if req.Name != "" {
		menu.Name = req.Name
	}
	if req.Path != nil {
		menu.Path = *req.Path
	}
	if req.Component != nil {
		menu.Component = *req.Component
	}
	if req.Redirect != nil {
		menu.Redirect = *req.Redirect
	}
	if req.Icon != nil {
		menu.Icon = *req.Icon
	}
	if req.Title != "" {
		menu.Title = req.Title
	}
	if req.Type != nil {
		menu.Type = *req.Type
	}
	if req.Permission != nil {
		menu.Permission = *req.Permission
	}
	if req.Sort != nil {
		menu.Sort = *req.Sort
	}
	if req.Visible != nil {
		menu.Visible = *req.Visible
	}
	if req.Status != nil {
		menu.Status = *req.Status
	}
	if req.IsExternal != nil {
		menu.IsExternal = *req.IsExternal
	}
	if req.IsCache != nil {
		menu.IsCache = *req.IsCache
	}
	menu.UpdateBy = operatorID

	if err := s.menuRepo.Update(menu); err != nil {
		return err
	}
	s.syncPolicies()
	return nil
}

// Delete 删除菜单。
//
// 存在子菜单时拒绝删除：直接删父节点会让子菜单的 parent_id 悬空，
// 而树是从 parent_id=0 递归构建的，这棵子树会「从界面上消失」，
// 数据却还在库里，既看不见也删不掉，成为孤儿数据。
// 部门模块此前已有同样的保护，菜单这里漏了。
func (s *menuService) Delete(id uint) error {
	children, err := s.menuRepo.CountByParentID(id)
	if err != nil {
		return err
	}
	if children > 0 {
		return ErrMenuHasChildren
	}

	if err := s.menuRepo.Delete(id); err != nil {
		return err
	}
	s.syncPolicies()
	return nil
}

// syncPolicies 菜单的权限码增删改会影响策略，需重建并清理角色缓存。
func (s *menuService) syncPolicies() {
	if err := middleware.SyncPoliciesFromRoleMenus(); err != nil {
		logger.Log.Errorf("同步权限策略失败: %v", err)
	}
	if err := cache.DelByPrefix(context.Background(), "rbac:roles:"); err != nil {
		logger.Log.Warnf("清理角色缓存失败: %v", err)
	}
}

func (s *menuService) FindByID(id uint) (interface{}, error) {
	menu, err := s.menuRepo.FindByID(id)
	if err != nil {
		return nil, common.NotFoundOrErr(err, "菜单不存在")
	}
	return menu, nil
}

func (s *menuService) FindAll() ([]model.SysMenu, error) {
	return s.menuRepo.FindAll()
}

func (s *menuService) FindTree() ([]model.SysMenu, error) {
	menus, err := s.menuRepo.FindAll()
	if err != nil {
		return nil, err
	}
	return buildMenuTree(menus, 0), nil
}

func (s *menuService) FindTreeForManage() ([]model.SysMenu, error) {
	menus, err := s.menuRepo.FindAllForManage()
	if err != nil {
		return nil, err
	}
	return buildMenuTree(menus, 0), nil
}

func (s *menuService) FindMenusByRoleIDs(roleIDs []uint) ([]model.SysMenu, error) {
	return s.menuRepo.FindMenusByRoleIDs(roleIDs)
}
