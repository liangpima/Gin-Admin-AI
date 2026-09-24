package repository

import (
	"fmt"

	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

// PostRepository 岗位数据访问。
//
// sys_post 是**多租户表**（含 tenant_id 列），所有方法都必须接收并透传 tenantID，
// 查询一律经 common.TenantScope 过滤。
// 注意 TenantScope(db, 0) 表示"不过滤"（平台级账号），所以漏传 tenantID
// 会静默退化为全表查询 —— 这是该类问题最典型的成因。
type PostRepository interface {
	Create(post *model.SysPost) error
	FindByID(tenantID, id uint) (*model.SysPost, error)
	// FindByIDs 按租户过滤后批量查询，供「给用户分配岗位」时校验岗位归属
	FindByIDs(tenantID uint, ids []uint) ([]model.SysPost, error)
	FindAll(tenantID uint) ([]model.SysPost, error)
	FindList(tenantID uint, name string, status *int8, page, pageSize int) ([]model.SysPost, int64, error)
	Update(post *model.SysPost) error
	Delete(tenantID, id uint) error
	CountByCode(tenantID uint, code string, excludeID uint) (int64, error)
}

type postRepository struct {
	db *gorm.DB
}

func NewPostRepository() PostRepository {
	return &postRepository{db: database.DB}
}

// Create 创建岗位；TenantID 由 Service 层赋值
func (r *postRepository) Create(post *model.SysPost) error {
	if err := r.db.Create(post).Error; err != nil {
		// 唯一索引是 (tenant_id, code)，同租户内编码重复会走到这里
		if database.IsDuplicateKey(err) {
			return fmt.Errorf("%w: %w", common.ErrDuplicateKey, err)
		}
		return err
	}
	return nil
}

func (r *postRepository) FindByID(tenantID, id uint) (*model.SysPost, error) {
	var post model.SysPost
	err := common.TenantScope(r.db, tenantID).First(&post, id).Error
	return &post, err
}

func (r *postRepository) FindByIDs(tenantID uint, ids []uint) ([]model.SysPost, error) {
	posts := make([]model.SysPost, 0)
	if len(ids) == 0 {
		return posts, nil
	}
	err := common.TenantScope(r.db, tenantID).Where("id IN ?", ids).Find(&posts).Error
	return posts, err
}

func (r *postRepository) FindAll(tenantID uint) ([]model.SysPost, error) {
	var posts []model.SysPost
	err := common.TenantScope(r.db, tenantID).Where("status = ?", 1).
		Order("sort ASC, id ASC").Find(&posts).Error
	return posts, err
}

func (r *postRepository) FindList(tenantID uint, name string, status *int8, page, pageSize int) ([]model.SysPost, int64, error) {
	var posts []model.SysPost
	var total int64

	query := common.TenantScope(r.db.Model(&model.SysPost{}), tenantID)
	if name != "" {
		query = query.Where("name LIKE ?", "%"+common.EscapeLike(name)+"%")
	}
	if status != nil {
		query = query.Where("status = ?", *status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).Order("sort ASC, id ASC").Find(&posts).Error
	return posts, total, err
}

// Update 更新岗位。Select 列表刻意不含 TenantID，
// 避免把调用方对象里可能为 0 的 tenant_id 写回，导致岗位"漂移"到平台租户。
func (r *postRepository) Update(post *model.SysPost) error {
	return r.db.Model(post).Select("Code", "Name", "Sort", "Status", "Remark", "UpdateBy").Updates(post).Error
}

// Delete 软删除岗位，并清理用户-岗位关联。
//
// 删除前改写 code 释放唯一索引占用（唯一索引是 (tenant_id, code)，
// FreedUniqueValue 会把 ID 拼进编码，从而在租户内保持唯一），
// 否则同编码岗位将无法再次创建。
//
// sys_user_post 是纯关联表（只有 user_id / post_id，无 tenant_id），
// 不能对它套 TenantScope，隔离性由「先按租户确认岗位归属」保证。
func (r *postRepository) Delete(tenantID, id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var post model.SysPost
		if err := common.TenantScope(tx, tenantID).First(&post, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.SysPost{}).Where("id = ?", post.ID).
			Update("code", common.FreedUniqueValue(post.Code, post.ID, 64)).Error; err != nil {
			return err
		}

		if err := tx.Where("post_id = ?", post.ID).Delete(&model.SysUserPost{}).Error; err != nil {
			return err
		}

		return common.TenantScope(tx, tenantID).Delete(&model.SysPost{}, id).Error
	})
}

// CountByCode 统计同租户内的同编码岗位数。
//
// 与用户/角色不同，岗位是租户内表（唯一约束是 (tenant_id, code) 复合索引），
// 因此这里必须按租户过滤 —— 不同租户可以各自拥有同名岗位。
func (r *postRepository) CountByCode(tenantID uint, code string, excludeID uint) (int64, error) {
	var count int64
	query := common.TenantScope(r.db.Model(&model.SysPost{}), tenantID).Where("code = ?", code)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
