package repository

import (
	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/member/model"

	"gorm.io/gorm"
)

type MemberTagRepository interface {
	Create(tag *model.MemberTag) error
	Update(tenantID uint, tag *model.MemberTag) error
	Delete(tenantID, id uint) error
	FindByID(tenantID, id uint) (*model.MemberTag, error)
	FindByIDs(tenantID uint, ids []uint) ([]model.MemberTag, error)
	FindList(tenantID uint, name string, page, pageSize int) ([]model.MemberTag, int64, error)
	FindAll(tenantID uint) ([]model.MemberTag, error)
}

type memberTagRepository struct{}

func NewMemberTagRepository() MemberTagRepository {
	return &memberTagRepository{}
}

// Create 创建会员标签；TenantID 由 Service 层赋值
func (r *memberTagRepository) Create(tag *model.MemberTag) error {
	return database.DB.Create(tag).Error
}

// Update 更新会员标签；先校验记录归属当前租户，再保存
func (r *memberTagRepository) Update(tenantID uint, tag *model.MemberTag) error {
	var count int64
	if err := common.TenantScope(database.DB, tenantID).
		Model(&model.MemberTag{}).
		Where("id = ?", tag.ID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return database.DB.Save(tag).Error
}

// Delete 软删除会员标签，并清理会员-标签关联，避免留下孤儿记录
func (r *memberTagRepository) Delete(tenantID, id uint) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tag_id = ?", id).Delete(&model.MemberTagRel{}).Error; err != nil {
			return err
		}
		return common.TenantScope(tx, tenantID).Delete(&model.MemberTag{}, id).Error
	})
}

func (r *memberTagRepository) FindByID(tenantID, id uint) (*model.MemberTag, error) {
	var tag model.MemberTag
	err := common.TenantScope(database.DB, tenantID).First(&tag, id).Error
	return &tag, err
}

func (r *memberTagRepository) FindByIDs(tenantID uint, ids []uint) ([]model.MemberTag, error) {
	tags := make([]model.MemberTag, 0)
	if len(ids) == 0 {
		return tags, nil
	}
	err := common.TenantScope(database.DB, tenantID).Where("id IN ?", ids).Find(&tags).Error
	return tags, err
}

func (r *memberTagRepository) FindList(tenantID uint, name string, page, pageSize int) ([]model.MemberTag, int64, error) {
	var tags []model.MemberTag
	var total int64

	query := common.TenantScope(database.DB, tenantID).Model(&model.MemberTag{})
	if name != "" {
		query = query.Where("name LIKE ?", "%"+common.EscapeLike(name)+"%")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("sort ASC, id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&tags).Error
	return tags, total, err
}

func (r *memberTagRepository) FindAll(tenantID uint) ([]model.MemberTag, error) {
	var tags []model.MemberTag
	err := common.TenantScope(database.DB, tenantID).Where("status = 1").Order("sort ASC").Find(&tags).Error
	return tags, err
}
