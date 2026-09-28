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
	if err := s.checkNameUnique(req.Name, 0); err != nil {
		return err
	}
	if err := validatePermissionCode(req.Permission); err != nil {
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

// validatePermissionCode 校验菜单上的权限标识是「真实存在且会被鉴权检查」的权限码。
//
// 背景（这是一条真实的提权链，不要删掉这个校验）：
//  1. 菜单的 permission 会被 middleware.SyncPoliciesFromRoleMenus 原样编译成
//     Casbin 策略 `{roleCode, default, permission, "*"}`；
//  2. model.conf 的 matcher 含 `p.obj == "*" || r.obj == p.obj`；
//  3. 因此把任一菜单的 permission 改成 `*`，持有该菜单的角色就获得
//     **全部 protected 路由**的通行权，而项目为此专门实现的授权收敛
//     （OperatorHoldsPermissions / checkMenusGrantable）会被完全旁路 ——
//     因为拿到 `*` 之后「授予是否越权」的所有判定都恒真。
//
// 除 `*` 之外的非法值不会造成放行（未登记的权限码永远匹配不到任何路由），
// 但会让菜单看起来「配了权限却不生效」，所以一并拒绝并给出明确文案。
func validatePermissionCode(code string) error {
	if code == "" {
		// 目录型菜单没有权限码，是合法取值
		return nil
	}
	if code == "*" {
		return common.NewBizError("权限标识不能为通配符 *，请填写具体权限码")
	}
	if !middleware.IsRegisteredPermission(code) {
		return common.NewBizError("权限标识不是有效的权限码：" + code)
	}
	return nil
}

// checkNameUnique 校验菜单标识（name）全局唯一。
//
// 为什么必须校验：name 会被原样写进 Vue Router 的 route.name，而项目有两处
// 直接依赖它：
//   - `store/modules/tagsView.ts` 的 addCachedView 用 route.name 维护
//     keep-alive 的 `include` 列表；
//   - `store/modules/user.ts` 登出时按 route.name 逐个 removeRoute。
//
// 重名时 vue-router 只保留最后一次注册，前者的缓存/清理都会落到别的页面上，
// 表现为「打开 A 页面却复用了 B 的缓存」这类极难定位的现象，且**没有任何报错**。
// 前端表单此前根本没有 name 输入项（见 H11），所以这条路径长期不可达；
// 补上输入项之后它就成了一个用户随手就能踩到的坑。
//
// 局限：`sys_menu` 上没有 name 的唯一索引（只有 idx_parent_id / idx_deleted_at），
// 因此这里只是应用层校验，并发创建仍可能穿透 —— 与角色 code 不同，后者有
// 唯一索引兜底。要彻底封死需补一条迁移加 `uk_name`，本次未做（改动面更大，
// 且既有数据需先确认无重名）。
func (s *menuService) checkNameUnique(name string, excludeID uint) error {
	if name == "" {
		return nil
	}
	count, err := s.menuRepo.CountByName(name, excludeID)
	if err != nil {
		return err
	}
	if count > 0 {
		return common.NewBizError("菜单标识已存在：" + name)
	}
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
		if err := s.checkNameUnique(req.Name, req.ID); err != nil {
			return err
		}
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
		if err := validatePermissionCode(*req.Permission); err != nil {
			return err
		}
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
		// 仓储在菜单不存在时返回 gorm.ErrRecordNotFound → 转 404，
		// 否则用户传了个不存在的 ID 会得到「服务器内部错误」
		return common.NotFoundOrErr(err, "菜单不存在")
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
