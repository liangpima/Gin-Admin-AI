package repository

import (
	"fmt"

	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

type ConfigRepository interface {
	Create(config *model.SysConfig) error
	FindByID(id uint) (*model.SysConfig, error)
	FindByKey(key string) (*model.SysConfig, error)
	FindList(name string, page, pageSize int) ([]model.SysConfig, int64, error)
	Update(config *model.SysConfig) error
	Delete(id uint) error
	FindByKeyPrefix(prefix string) ([]model.SysConfig, error)
	UpsertByKey(config *model.SysConfig) error
}

type configRepository struct {
	db *gorm.DB
}

func NewConfigRepository() ConfigRepository {
	return &configRepository{db: database.DB}
}

func (r *configRepository) Create(config *model.SysConfig) error {
	if err := r.db.Create(config).Error; err != nil {
		// uk_config_key 是全局唯一索引
		if database.IsDuplicateKey(err) {
			return fmt.Errorf("%w: %w", common.ErrDuplicateKey, err)
		}
		return err
	}
	return nil
}

func (r *configRepository) FindByID(id uint) (*model.SysConfig, error) {
	var config model.SysConfig
	err := r.db.First(&config, id).Error
	return &config, err
}

func (r *configRepository) FindByKey(key string) (*model.SysConfig, error) {
	var config model.SysConfig
	err := r.db.Where("config_key = ?", key).First(&config).Error
	return &config, err
}

func (r *configRepository) FindList(name string, page, pageSize int) ([]model.SysConfig, int64, error) {
	var configs []model.SysConfig
	var total int64

	query := r.db.Model(&model.SysConfig{})
	if name != "" {
		query = query.Where("name LIKE ?", "%"+common.EscapeLike(name)+"%")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).Order("id ASC").Find(&configs).Error
	return configs, total, err
}

func (r *configRepository) Update(config *model.SysConfig) error {
	return r.db.Model(config).Select("Name", "ConfigKey", "Value", "Type", "Remark", "UpdateBy").Updates(config).Error
}

// Delete 软删除配置项。删除前改写 config_key 释放唯一索引占用，
// 否则同 key 的配置将无法再次创建。
func (r *configRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var cfg model.SysConfig
		if err := tx.First(&cfg, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.SysConfig{}).Where("id = ?", cfg.ID).
			Update("config_key", common.FreedUniqueValue(cfg.ConfigKey, cfg.ID, 191)).Error; err != nil {
			return err
		}
		return tx.Delete(&model.SysConfig{}, id).Error
	})
}

func (r *configRepository) FindByKeyPrefix(prefix string) ([]model.SysConfig, error) {
	var configs []model.SysConfig
	err := r.db.Where("config_key LIKE ?", common.EscapeLike(prefix)+"%").Order("id ASC").Find(&configs).Error
	return configs, err
}

func (r *configRepository) UpsertByKey(config *model.SysConfig) error {
	var existing model.SysConfig
	err := r.db.Where("config_key = ?", config.ConfigKey).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return r.db.Create(config).Error
	}
	if err != nil {
		return err
	}
	existing.Value = config.Value
	existing.UpdateBy = config.UpdateBy
	return r.db.Model(&existing).Select("Value", "UpdateBy").Updates(&existing).Error
}
