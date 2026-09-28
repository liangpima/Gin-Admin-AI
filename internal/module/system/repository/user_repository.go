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
	Update(tenantID uint, user *model.SysUser) error
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
	// Transaction 在**单个**数据库事务内执行 fn，fn 收到的是绑定到该事务的仓储副本。
	//
	// 为什么需要它：一次「建用户」实际要写三张表
	// （sys_user + sys_user_role + sys_user_post）。仓储的每个方法各自开事务
	// 只能保证「自己那一笔」原子 —— 主表已落库、关联表失败时仍会留下半成品：
	// 用户建好了、角色是空的，调用方拿到 500 以为整次操作失败，
	// 实际上那个账号已经能用（且没有任何权限，排查时现象离根因很远）。
	//
	// 把 `*gorm.DB` 交给 Service 会破坏分层（Service 不应感知存储细节），
	// 因此由仓储提供事务边界，Service 只负责组合调用。
	// 实现内部会开 SAVEPOINT 而非新事务（GORM 对已处于事务中的会话自动降级），
	// 所以 fn 里继续调用 ReplaceRoles 这类自带事务的方法也是安全的。
	Transaction(fn func(tx UserRepository) error) error
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

// Transaction 见接口注释。fn 收到的仓储共享同一个事务，
// 因此其中任何一步失败都会把先前的写入一并回滚。
func (r *userRepository) Transaction(fn func(tx UserRepository) error) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return fn(&userRepository{db: tx})
	})
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

// Update 更新用户的可编辑字段。
//
// 两个要点，都是「调用方看着没事、换个调用方就出事」的那类：
//
//  1. **必须带租户条件**（规则 7）。当前两个调用方（`userService.Update` /
//     `UpdateDept`）都先做了 `FindByID(tenantID, ...)`，所以「看起来」不会
//     跨租户。但那层校验一旦被删掉或绕过，这里就是最后一道闸 ——
//     仓储层不该把隔离性寄托在调用方的自觉上。
//     不传 tenantID 时（tenantID==0）TenantScope 不过滤，保持平台级调用可用。
//
//  2. **Select 刻意不含 Password**。`user` 是从库里读出来的，它的 Password
//     是**读取那一刻**的哈希；并发场景下（管理员改资料的同时该用户自己重置了
//     密码）把它写回去，会把新哈希**静默回滚**成旧值 —— 用户改完密码发现
//     旧密码又能用了，且没有任何报错。密码只能走 ResetPassword。
//     `UpdateLoginTime` 处已有同类的明文警告。
func (r *userRepository) Update(tenantID uint, user *model.SysUser) error {
	return common.TenantScope(r.db, tenantID).
		Model(&model.SysUser{}).
		Where("id = ?", user.ID).
		Select("Username", "Nickname", "Phone", "Email", "Avatar", "Status", "DeptID", "Remark", "UpdateBy").
		Updates(user).Error
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

// UpdateStatus 修改用户状态，并**确认目标确实属于本租户**。
//
// 为什么不能只靠 UPDATE 的影响行数：
// 本项目的 DSN 没有开启 `clientFoundRows`，MySQL 返回的 RowsAffected 是
// 「实际发生变化的行数」而非「匹配的行数」。于是把已经是停用的用户再停用一次
// （status 值没变）会得到 0 行，被误判成「用户不存在」而返回 404 —— 那是把
// 幂等操作变成了报错。因此这里先用一条按租户过滤的 COUNT 确认归属，
// 再执行更新：语义明确，且与 clientFoundRows 的取值无关。
//
// 归属校验本身是必须的：TenantScope 命中 0 行时 GORM **不返回错误**，
// 若不检查，调用方会以为「改成功」，随后依据这次「成功」去吊销 Token
// （用的还是原始 ID）—— 那就是跨租户强制下线。
func (r *userRepository) UpdateStatus(tenantID, id uint, status int8) error {
	var count int64
	if err := common.TenantScope(r.db, tenantID).
		Model(&model.SysUser{}).
		Where("id = ?", id).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}

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
		// 去重（sys_user_role 是 (user_id, role_id) 复合主键）：
		// Service 层目前也会去重，但仓储层不该依赖调用方的自觉 ——
		// 重复 ID 撞主键是 500，而它本可以是一次正常的幂等写入
		for _, roleID := range common.UniqueNonZeroIDs(roleIDs) {
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
		// 同 ReplaceRoles：sys_user_post 也是 (user_id, post_id) 复合主键
		for _, postID := range common.UniqueNonZeroIDs(postIDs) {
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
