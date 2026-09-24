package repository

import (
	"fmt"
	"time"

	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

type UserRepository interface {
	Create(user *model.SysUser) error
	FindByID(tenantID, id uint) (*model.SysUser, error)
	FindByUsername(tenantID uint, username string) (*model.SysUser, error)
	FindByUsernameForAuth(username string) (*model.SysUser, error)
	FindList(tenantID uint, username, phone string, status *int8, deptID uint, page, pageSize int) ([]model.SysUser, int64, error)
	Update(user *model.SysUser) error
	Delete(tenantID, id uint) error
	UpdateStatus(tenantID, id uint, status int8) error
	ResetPassword(tenantID, id uint, password string) error
	UpdateLoginTime(tenantID, id uint, t time.Time) error
	ReplaceRoles(userID uint, roleIDs []uint) error
	ReplacePosts(userID uint, postIDs []uint) error
	FindRoleIDsByUserID(userID uint) ([]uint, error)
	FindRoleIDsByUserIDs(userIDs []uint) (map[uint][]uint, error)
	// CountByUsername 按用户名统计，**不做租户过滤**（详见实现处注释）
	CountByUsername(username string, excludeID uint) (int64, error)
}

type userRepository struct {
	db *gorm.DB
}

func NewUserRepository() UserRepository {
	return &userRepository{db: database.DB}
}

func (r *userRepository) Create(user *model.SysUser) error {
	if err := r.db.Create(user).Error; err != nil {
		// username 是全局唯一索引，冲突时交给 Service 转成业务提示。
		// 前置的 CountByUsername 存在时间窗口，并发下仍可能走到这里。
		if database.IsDuplicateKey(err) {
			return fmt.Errorf("%w: %w", common.ErrDuplicateKey, err)
		}
		return err
	}
	return nil
}

func (r *userRepository) FindByID(tenantID, id uint) (*model.SysUser, error) {
	var user model.SysUser
	err := common.TenantScope(r.db, tenantID).First(&user, id).Error
	return &user, err
}

func (r *userRepository) FindByUsername(tenantID uint, username string) (*model.SysUser, error) {
	var user model.SysUser
	err := common.TenantScope(r.db, tenantID).Where("username = ?", username).First(&user).Error
	return &user, err
}

func (r *userRepository) FindByUsernameForAuth(username string) (*model.SysUser, error) {
	var user model.SysUser
	err := r.db.Where("username = ?", username).First(&user).Error
	return &user, err
}

func (r *userRepository) FindList(tenantID uint, username, phone string, status *int8, deptID uint, page, pageSize int) ([]model.SysUser, int64, error) {
	var users []model.SysUser
	var total int64

	query := common.TenantScope(r.db.Model(&model.SysUser{}), tenantID)

	if username != "" {
		query = query.Where("username LIKE ?", "%"+common.EscapeLike(username)+"%")
	}
	if phone != "" {
		query = query.Where("phone LIKE ?", "%"+common.EscapeLike(phone)+"%")
	}
	if status != nil {
		query = query.Where("status = ?", *status)
	}
	if deptID > 0 {
		query = query.Where("dept_id = ?", deptID)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).Order("id ASC").Find(&users).Error
	return users, total, err
}

func (r *userRepository) Update(user *model.SysUser) error {
	return r.db.Model(user).Select("Username", "Nickname", "Phone", "Email", "Avatar", "Password", "Status", "DeptID", "Remark", "UpdateBy").Updates(user).Error
}

// Delete 软删除用户，并清理其角色/岗位关联。
//
// 删除前改写 username 释放唯一索引占用，否则同名用户将无法再次创建
// （唯一索引不区分记录是否已软删除）。
func (r *userRepository) Delete(tenantID, id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var user model.SysUser
		if err := common.TenantScope(tx, tenantID).First(&user, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.SysUser{}).Where("id = ?", user.ID).
			Update("username", common.FreedUniqueValue(user.Username, user.ID, 64)).Error; err != nil {
			return err
		}

		// 清理关联表，避免留下孤儿记录
		if err := tx.Where("user_id = ?", user.ID).Delete(&model.SysUserRole{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&model.SysUserPost{}).Error; err != nil {
			return err
		}

		return common.TenantScope(tx, tenantID).Delete(&model.SysUser{}, id).Error
	})
}

func (r *userRepository) UpdateStatus(tenantID, id uint, status int8) error {
	return common.TenantScope(r.db, tenantID).Model(&model.SysUser{}).Where("id = ?", id).Update("status", status).Error
}

func (r *userRepository) ResetPassword(tenantID, id uint, password string) error {
	return common.TenantScope(r.db, tenantID).Model(&model.SysUser{}).Where("id = ?", id).Update("password", password).Error
}

// UpdateLoginTime 只更新"最后登录时间"这一个字段。
//
// 不能用 Update(user) 代劳，它有两个致命问题：
//  1. Update 的 Select 列表里**没有 login_time** —— 那次调用压根不会更新该字段，
//     是一次无效写入（登录时间永远是 NULL），却看起来像写成功了；
//  2. Select 列表里**有 password** —— 会把登录时读到的旧密码哈希整行写回。
//     若管理员在这期间重置了该用户的密码，重置结果会被静默回滚，
//     用户仍能用旧密码登录（或被重置掉的旧密码反而生效）。
func (r *userRepository) UpdateLoginTime(tenantID, id uint, t time.Time) error {
	return common.TenantScope(r.db, tenantID).
		Model(&model.SysUser{}).
		Where("id = ?", id).
		Update("login_time", t).Error
}

func (r *userRepository) ReplaceRoles(userID uint, roleIDs []uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&model.SysUserRole{}).Error; err != nil {
			return err
		}
		for _, roleID := range roleIDs {
			ur := model.SysUserRole{UserID: userID, RoleID: roleID}
			if err := tx.Create(&ur).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *userRepository) ReplacePosts(userID uint, postIDs []uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&model.SysUserPost{}).Error; err != nil {
			return err
		}
		for _, postID := range postIDs {
			up := model.SysUserPost{UserID: userID, PostID: postID}
			if err := tx.Create(&up).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *userRepository) FindRoleIDsByUserID(userID uint) ([]uint, error) {
	var roleIDs []uint
	err := r.db.Model(&model.SysUserRole{}).
		Where("user_id = ?", userID).
		Pluck("role_id", &roleIDs).Error
	return roleIDs, err
}

// FindRoleIDsByUserIDs 批量查询多个用户的角色ID，返回 userID -> roleIDs 映射。
// 用于列表场景一次性取回关联关系，避免逐个用户查询（N+1）。
func (r *userRepository) FindRoleIDsByUserIDs(userIDs []uint) (map[uint][]uint, error) {
	result := make(map[uint][]uint, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}

	var rels []model.SysUserRole
	if err := r.db.Where("user_id IN ?", userIDs).Find(&rels).Error; err != nil {
		return nil, err
	}
	for _, rel := range rels {
		result[rel.UserID] = append(result[rel.UserID], rel.RoleID)
	}
	return result, nil
}

// CountByUsername 统计同名用户数，**刻意不做租户过滤**。
//
// sys_user 的 `uk_username` 是全局唯一索引，这一点是设计必需的：
// 登录接口（POST /auth/login）只接收 username + password，**没有租户字段**，
// FindByUsernameForAuth 也只能按用户名全局定位用户。
// 因此「用户名全局唯一」是登录流程成立的前提。
//
// 既然约束是全局的，重名校验就必须是全局的 —— 早前这里按租户过滤，
// 于是租户 B 建同名用户时校验通过、插入却撞唯一索引，
// 对外表现为 500「服务器内部错误」，用户完全不知道是自己重名了。
//
// 返回 error 而不是直接丢给调用方一个 int64：Count 失败时 count 保持 0，
// 若把错误吞掉，调用方会把「数据库故障」读成「不重名」，
// 放行一次注定失败的 INSERT，真正的故障点因此离根因很远。
func (r *userRepository) CountByUsername(username string, excludeID uint) (int64, error) {
	var count int64
	query := r.db.Model(&model.SysUser{}).Where("username = ?", username)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
