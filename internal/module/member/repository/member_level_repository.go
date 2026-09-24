package repository

import (
	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/member/model"

	"gorm.io/gorm"
)

type MemberLevelRepository interface {
	Create(level *model.MemberLevel) error
	Update(tenantID uint, level *model.MemberLevel) error
	Delete(tenantID, id uint) error
	FindByID(tenantID, id uint) (*model.MemberLevel, error)
	FindList(tenantID uint, name string, page, pageSize int) ([]model.MemberLevel, int64, error)
	FindAll(tenantID uint) ([]model.MemberLevel, error)
}

type memberLevelRepository struct{}

func NewMemberLevelRepository() MemberLevelRepository {
	return &memberLevelRepository{}
}

// Create 创建会员等级；TenantID 由 Service 层赋值
func (r *memberLevelRepository) Create(level *model.MemberLevel) error {
	return database.DB.Create(level).Error
}

// Update 更新会员等级；先校验记录归属当前租户，再保存
func (r *memberLevelRepository) Update(tenantID uint, level *model.MemberLevel) error {
	var count int64
	if err := common.TenantScope(database.DB, tenantID).
		Model(&model.MemberLevel{}).
		Where("id = ?", level.ID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return database.DB.Save(level).Error
}

func (r *memberLevelRepository) Delete(tenantID, id uint) error {
	return common.TenantScope(database.DB, tenantID).Delete(&model.MemberLevel{}, id).Error
}

func (r *memberLevelRepository) FindByID(tenantID, id uint) (*model.MemberLevel, error) {
	var level model.MemberLevel
	err := common.TenantScope(database.DB, tenantID).First(&level, id).Error
	return &level, err
}

func (r *memberLevelRepository) FindList(tenantID uint, name string, page, pageSize int) ([]model.MemberLevel, int64, error) {
	var levels []model.MemberLevel
	var total int64

	query := common.TenantScope(database.DB, tenantID).Model(&model.MemberLevel{})
	if name != "" {
		query = query.Where("name LIKE ?", "%"+common.EscapeLike(name)+"%")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("sort ASC, id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&levels).Error
	return levels, total, err
}

func (r *memberLevelRepository) FindAll(tenantID uint) ([]model.MemberLevel, error) {
	var levels []model.MemberLevel
	err := common.TenantScope(database.DB, tenantID).Where("status = 1").Order("sort ASC").Find(&levels).Error
	return levels, err
}
