package repository

import (
	"fmt"

	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

type RoleRepository interface {
	Create(role *model.SysRole) error
	FindByID(tenantID, id uint) (*model.SysRole, error)
	FindByCode(tenantID uint, code string) (*model.SysRole, error)
	FindList(tenantID uint, name, code string, status *int8, page, pageSize int) ([]model.SysRole, int64, error)
	Update(tenantID uint, role *model.SysRole) error
	Delete(tenantID, id uint) error
	UpdateStatus(tenantID, id uint, status int8) error
	ReplaceMenus(tenantID, roleID uint, menuIDs []uint) error
	FindMenusByRoleID(tenantID, roleID uint) ([]model.SysMenu, error)
	FindMenuIDsByRoleID(tenantID, roleID uint) ([]uint, error)
	// FindPermissionsByRoleIDs 返回这些角色当前持有的权限码（去重、忽略空值）。
	//
	// 用于授权收敛校验：把角色授予他人前，必须先确认该角色的权限集是
	// 操作者自身权限集的子集，否则低权管理员可以借「授予一个高权角色」提权。
	FindPermissionsByRoleIDs(roleIDs []uint) ([]string, error)
	// CountByCode 按角色编码统计，**不做租户过滤**（详见实现处注释）
	CountByCode(code string, excludeID uint) (int64, error)
	FindByIDs(tenantID uint, ids []uint) ([]model.SysRole, error)
}

type roleRepository struct {
	db *gorm.DB
}

func NewRoleRepository() RoleRepository {
	return &roleRepository{db: database.DB}
}

// Create 创建角色；TenantID 由 Service 层赋值
func (r *roleRepository) Create(role *model.SysRole) error {
	if err := r.db.Create(role).Error; err != nil {
		if database.IsDuplicateKey(err) {
			return fmt.Errorf("%w: %w", common.ErrDuplicateKey, err)
		}
		return err
	}
	return nil
}

func (r *roleRepository) FindByID(tenantID, id uint) (*model.SysRole, error) {
	var role model.SysRole
	err := common.TenantScope(r.db, tenantID).First(&role, id).Error
	return &role, err
}

func (r *roleRepository) FindByCode(tenantID uint, code string) (*model.SysRole, error) {
	var role model.SysRole
	err := common.TenantScope(r.db, tenantID).Where("code = ?", code).First(&role).Error
	return &role, err
}

func (r *roleRepository) FindList(tenantID uint, name, code string, status *int8, page, pageSize int) ([]model.SysRole, int64, error) {
	var roles []model.SysRole
	var total int64

	query := common.TenantScope(r.db, tenantID).Model(&model.SysRole{})

	if name != "" {
		query = query.Where("name LIKE ?", "%"+common.EscapeLike(name)+"%")
	}
	if code != "" {
		query = query.Where("code LIKE ?", "%"+common.EscapeLike(code)+"%")
	}
	if status != nil {
		query = query.Where("status = ?", *status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).Order("sort ASC, id ASC").Find(&roles).Error
	return roles, total, err
}

// Update 更新角色；先校验记录归属当前租户，再保存
func (r *roleRepository) Update(tenantID uint, role *model.SysRole) error {
	var count int64
	if err := common.TenantScope(r.db, tenantID).
		Model(&model.SysRole{}).
		Where("id = ?", role.ID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	// 改编码时可能撞 uk_code（全局唯一），交给 Service 转成业务提示
	if err := r.db.Model(role).Select("Name", "Code", "Sort", "Status", "DataScope", "Remark", "UpdateBy").Updates(role).Error; err != nil {
		if database.IsDuplicateKey(err) {
			return fmt.Errorf("%w: %w", common.ErrDuplicateKey, err)
		}
		return err
	}
	return nil
}

// Delete 软删除角色，并清理其用户/菜单关联。
//
// 删除前改写 code 释放唯一索引占用，否则同编码角色将无法再次创建
// （唯一索引不区分记录是否已软删除）。
func (r *roleRepository) Delete(tenantID, id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var role model.SysRole
		if err := common.TenantScope(tx, tenantID).First(&role, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.SysRole{}).Where("id = ?", role.ID).
			Update("code", common.FreedUniqueValue(role.Code, role.ID, 64)).Error; err != nil {
			return err
		}

		// 清理关联表，避免留下孤儿记录
		if err := tx.Where("role_id = ?", role.ID).Delete(&model.SysUserRole{}).Error; err != nil {
			return err
		}
		if err := tx.Where("role_id = ?", role.ID).Delete(&model.SysRoleMenu{}).Error; err != nil {
			return err
		}

		return common.TenantScope(tx, tenantID).Delete(&model.SysRole{}, id).Error
	})
}

func (r *roleRepository) UpdateStatus(tenantID, id uint, status int8) error {
	return common.TenantScope(r.db, tenantID).Model(&model.SysRole{}).Where("id = ?", id).Update("status", status).Error
}

// ReplaceMenus 重建角色-菜单关联；先校验角色归属，再在事务中执行
func (r *roleRepository) ReplaceMenus(tenantID, roleID uint, menuIDs []uint) error {
	var count int64
	if err := common.TenantScope(r.db, tenantID).
		Model(&model.SysRole{}).
		Where("id = ?", roleID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", roleID).Delete(&model.SysRoleMenu{}).Error; err != nil {
			return err
		}
		for _, menuID := range menuIDs {
			rm := model.SysRoleMenu{RoleID: roleID, MenuID: menuID}
			if err := tx.Create(&rm).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *roleRepository) FindMenusByRoleID(tenantID, roleID uint) ([]model.SysMenu, error) {
	var menus []model.SysMenu
	query := r.db.Joins("JOIN sys_role_menu ON sys_role_menu.menu_id = sys_menu.id").
		Where("sys_role_menu.role_id = ?", roleID)
	if tenantID > 0 {
		// 限定该角色必须属于当前租户，避免越权读取其他租户角色的菜单
		query = query.Where("sys_role_menu.role_id IN (?)",
			r.db.Model(&model.SysRole{}).Select("id").Where("tenant_id = ?", tenantID))
	}
	err := query.Order("sys_menu.sort ASC, sys_menu.id ASC").Find(&menus).Error
	return menus, err
}

func (r *roleRepository) FindMenuIDsByRoleID(tenantID, roleID uint) ([]uint, error) {
	menuIDs := make([]uint, 0)
	query := r.db.Model(&model.SysRoleMenu{}).Where("role_id = ?", roleID)
	if tenantID > 0 {
		query = query.Where("role_id IN (?)",
			r.db.Model(&model.SysRole{}).Select("id").Where("tenant_id = ?", tenantID))
	}
	err := query.Pluck("menu_id", &menuIDs).Error
	return menuIDs, err
}

// FindPermissionsByRoleIDs 汇总这些角色经「角色-菜单」关联到的权限码。
//
// 刻意不做租户过滤：调用方（Service）必须先用 FindByIDs 确认角色归属，
// 否则这里过滤掉越权角色会让它「看起来权限为空」从而通过收敛校验 ——
// 把拒绝伪装成放行是这类校验最危险的失败方向。
func (r *roleRepository) FindPermissionsByRoleIDs(roleIDs []uint) ([]string, error) {
	perms := make([]string, 0)
	if len(roleIDs) == 0 {
		return perms, nil
	}
	err := r.db.Model(&model.SysMenu{}).
		Joins("JOIN sys_role_menu ON sys_role_menu.menu_id = sys_menu.id").
		Where("sys_role_menu.role_id IN ?", roleIDs).
		Where("sys_menu.permission <> ''").
		Distinct().
		Pluck("sys_menu.permission", &perms).Error
	return perms, err
}

// CountByCode 统计同编码角色数，**刻意不做租户过滤**。
//
// sys_role 的 `uk_code` 是全局唯一索引，这一点同样是设计必需的：
// Casbin 策略的主体是**角色 code**，而域是常量 "default"
// （见 config/casbin/model.conf 与 middleware.SyncPoliciesFromRoleMenus）。
// 若允许两个租户使用相同角色 code，两条策略会合并成同一条，
// 持有该角色的用户就会继承对方租户授予的权限 —— 跨租户权限泄漏。
//
// 因此角色 code 必须全局唯一，重名校验也必须按全局来。
// 早前按租户过滤，导致租户 B 建同名角色时校验通过、插入却撞唯一索引，
// 对外是 500「服务器内部错误」。
//
// 返回 error 的理由同 CountByUsername：吞掉 Count 的失败等于把
// 「数据库故障」误判为「不重名」。
func (r *roleRepository) CountByCode(code string, excludeID uint) (int64, error) {
	var count int64
	query := r.db.Model(&model.SysRole{}).Where("code = ?", code)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *roleRepository) FindByIDs(tenantID uint, ids []uint) ([]model.SysRole, error) {
	roles := make([]model.SysRole, 0)
	if len(ids) == 0 {
		return roles, nil
	}
	err := common.TenantScope(r.db, tenantID).Where("id IN ?", ids).Find(&roles).Error
	return roles, err
}
