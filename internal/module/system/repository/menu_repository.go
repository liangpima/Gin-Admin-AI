package repository

import (
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

type MenuRepository interface {
	Create(menu *model.SysMenu) error
	FindByID(id uint) (*model.SysMenu, error)
	FindAll() ([]model.SysMenu, error)
	FindAllForManage() ([]model.SysMenu, error)
	FindMenusByRoleIDs(roleIDs []uint) ([]model.SysMenu, error)
	// FindPermissionsByIDs 返回这些菜单上配置的权限码（去重、忽略空值）。
	//
	// 菜单的 permission 字段即权限码（见 middleware.SyncPoliciesFromRoleMenus），
	// 因此「这次授权涉及哪些权限」等价于「这些菜单声明了哪些权限码」。
	// 供角色服务做授权收敛校验：操作者只能授予自己已持有的权限码。
	FindPermissionsByIDs(ids []uint) ([]string, error)
	Update(menu *model.SysMenu) error
	Delete(id uint) error
	// FindParentID 返回菜单的父节点 ID，ok=false 表示菜单不存在（用于父级成环校验）
	FindParentID(id uint) (uint, bool, error)
	// CountByParentID 统计直接子节点数（删除前确认没有下级）
	CountByParentID(id uint) (int64, error)
}

type menuRepository struct {
	db *gorm.DB
}

func NewMenuRepository() MenuRepository {
	return &menuRepository{db: database.DB}
}

func (r *menuRepository) Create(menu *model.SysMenu) error {
	return r.db.Create(menu).Error
}

func (r *menuRepository) FindByID(id uint) (*model.SysMenu, error) {
	var menu model.SysMenu
	err := r.db.First(&menu, id).Error
	return &menu, err
}

func (r *menuRepository) FindAll() ([]model.SysMenu, error) {
	var menus []model.SysMenu
	err := r.db.Where("status = ?", 1).Order("sort ASC, id ASC").Find(&menus).Error
	return menus, err
}

func (r *menuRepository) FindAllForManage() ([]model.SysMenu, error) {
	var menus []model.SysMenu
	err := r.db.Order("sort ASC, id ASC").Find(&menus).Error
	return menus, err
}

func (r *menuRepository) FindMenusByRoleIDs(roleIDs []uint) ([]model.SysMenu, error) {
	var menus []model.SysMenu
	err := r.db.Joins("JOIN sys_role_menu ON sys_role_menu.menu_id = sys_menu.id").
		Where("sys_role_menu.role_id IN ? AND sys_menu.status = ?", roleIDs, 1).
		Order("sys_menu.sort ASC, sys_menu.id ASC").
		Distinct().Find(&menus).Error
	return menus, err
}

// FindPermissionsByIDs 返回这些菜单声明的权限码（去重、忽略空值）。
//
// 只取 permission <> '' 的菜单：目录型菜单不承载权限，勾选它们不构成授权，
// 因此不参与「只能授予自己已有的权限」的比对（否则低权管理员连建目录都做不了）。
func (r *menuRepository) FindPermissionsByIDs(ids []uint) ([]string, error) {
	perms := make([]string, 0)
	if len(ids) == 0 {
		return perms, nil
	}
	err := r.db.Model(&model.SysMenu{}).
		Where("id IN ?", ids).
		Where("permission <> ''").
		Distinct().
		Pluck("permission", &perms).Error
	return perms, err
}

func (r *menuRepository) Update(menu *model.SysMenu) error {
	return r.db.Model(menu).Select("ParentID", "Name", "Path", "Component", "Redirect", "Icon", "Title", "Type", "Permission", "Sort", "Visible", "Status", "IsExternal", "IsCache", "UpdateBy", "Remark").Updates(menu).Error
}

// Delete 软删除菜单，并清理角色-菜单关联，避免留下孤儿记录
func (r *menuRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("menu_id = ?", id).Delete(&model.SysRoleMenu{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.SysMenu{}, id).Error
	})
}

// FindParentID 查询菜单的父节点 ID。
// 用 Pluck 而非 First：记录不存在时返回空切片，无需额外处理 ErrRecordNotFound。
func (r *menuRepository) FindParentID(id uint) (uint, bool, error) {
	var parents []uint
	if err := r.db.Model(&model.SysMenu{}).Where("id = ?", id).Pluck("parent_id", &parents).Error; err != nil {
		return 0, false, err
	}
	if len(parents) == 0 {
		return 0, false, nil
	}
	return parents[0], true, nil
}

// CountByParentID 统计菜单的直接子节点数。
//
// 只统计「直接」子节点即可：判断能否删除只需知道有没有下一层，
// 直接子节点为空时更深层必然也不存在，无需递归统计整棵子树。
func (r *menuRepository) CountByParentID(id uint) (int64, error) {
	var count int64
	if err := r.db.Model(&model.SysMenu{}).Where("parent_id = ?", id).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
