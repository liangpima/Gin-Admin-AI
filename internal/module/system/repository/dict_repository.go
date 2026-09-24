package repository

import (
	"fmt"

	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

type DictRepository interface {
	CreateType(dictType *model.SysDictType) error
	FindTypeByID(id uint) (*model.SysDictType, error)
	FindTypeByType(typ string) (*model.SysDictType, error)
	FindTypeList(name string, page, pageSize int) ([]model.SysDictType, int64, error)
	UpdateType(dictType *model.SysDictType) error
	DeleteType(id uint) error

	CreateData(dictData *model.SysDictData) error
	FindDataByID(id uint) (*model.SysDictData, error)
	FindDataByType(typ string) ([]model.SysDictData, error)
	FindDataList(typ string, page, pageSize int) ([]model.SysDictData, int64, error)
	// CountDataByValue 统计同类型下该键值是否已存在；excludeID > 0 时排除自身（更新场景）
	CountDataByValue(dictType, value string, excludeID uint) (int64, error)
	// CountDataByType 统计某类型下的字典数据条数（删除类型前校验子级）
	CountDataByType(dictType string) (int64, error)
	UpdateData(dictData *model.SysDictData) error
	DeleteData(id uint) error
}

type dictRepository struct {
	db *gorm.DB
}

func NewDictRepository() DictRepository {
	return &dictRepository{db: database.DB}
}

func (r *dictRepository) CreateType(dictType *model.SysDictType) error {
	if err := r.db.Create(dictType).Error; err != nil {
		// uk_type 是全局唯一索引（字典类型是全局表，type 即字典数据的外键键名）
		if database.IsDuplicateKey(err) {
			return fmt.Errorf("%w: %w", common.ErrDuplicateKey, err)
		}
		return err
	}
	return nil
}

func (r *dictRepository) FindTypeByID(id uint) (*model.SysDictType, error) {
	var dictType model.SysDictType
	err := r.db.First(&dictType, id).Error
	return &dictType, err
}

func (r *dictRepository) FindTypeByType(typ string) (*model.SysDictType, error) {
	var dictType model.SysDictType
	err := r.db.Where("type = ?", typ).First(&dictType).Error
	return &dictType, err
}

func (r *dictRepository) FindTypeList(name string, page, pageSize int) ([]model.SysDictType, int64, error) {
	var dictTypes []model.SysDictType
	var total int64

	query := r.db.Model(&model.SysDictType{})
	if name != "" {
		query = query.Where("name LIKE ?", "%"+common.EscapeLike(name)+"%")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).Order("id ASC").Find(&dictTypes).Error
	return dictTypes, total, err
}

func (r *dictRepository) UpdateType(dictType *model.SysDictType) error {
	return r.db.Model(dictType).Select("Name", "Type", "Status", "Remark", "UpdateBy").Updates(dictType).Error
}

// DeleteType 软删除字典类型。删除前改写 type 释放唯一索引占用，
// 否则同类型字典将无法再次创建。
func (r *dictRepository) DeleteType(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var dictType model.SysDictType
		if err := tx.First(&dictType, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.SysDictType{}).Where("id = ?", dictType.ID).
			Update("type", common.FreedUniqueValue(dictType.Type, dictType.ID, 128)).Error; err != nil {
			return err
		}
		return tx.Delete(&model.SysDictType{}, id).Error
	})
}

func (r *dictRepository) CreateData(dictData *model.SysDictData) error {
	if err := r.db.Create(dictData).Error; err != nil {
		// uk_dict_type_value 兜底：前置的 Count 校验存在时间窗口，并发下仍可能撞索引
		if database.IsDuplicateKey(err) {
			return fmt.Errorf("%w: %w", common.ErrDuplicateKey, err)
		}
		return err
	}
	return nil
}

func (r *dictRepository) FindDataByID(id uint) (*model.SysDictData, error) {
	var dictData model.SysDictData
	err := r.db.First(&dictData, id).Error
	return &dictData, err
}

func (r *dictRepository) FindDataByType(typ string) ([]model.SysDictData, error) {
	var dictData []model.SysDictData
	err := r.db.Where("dict_type = ? AND status = ?", typ, 1).Order("sort ASC, id ASC").Find(&dictData).Error
	return dictData, err
}

func (r *dictRepository) FindDataList(typ string, page, pageSize int) ([]model.SysDictData, int64, error) {
	var dictData []model.SysDictData
	var total int64

	query := r.db.Model(&model.SysDictData{})
	if typ != "" {
		query = query.Where("dict_type = ?", typ)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).Order("sort ASC, id ASC").Find(&dictData).Error
	return dictData, total, err
}

func (r *dictRepository) CountDataByValue(dictType, value string, excludeID uint) (int64, error) {
	var count int64
	query := r.db.Model(&model.SysDictData{}).
		Where("dict_type = ? AND value = ?", dictType, value)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *dictRepository) CountDataByType(dictType string) (int64, error) {
	var count int64
	if err := r.db.Model(&model.SysDictData{}).
		Where("dict_type = ?", dictType).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// UpdateData 更新字典数据。Select 里刻意不含 DictType：
// 归属类型不可变（改了会让选项在原类型下消失），见 dto.UpdateDictDataRequest 注释。
func (r *dictRepository) UpdateData(dictData *model.SysDictData) error {
	if err := r.db.Model(dictData).
		Select("Label", "Value", "Sort", "CssClass", "ListClass", "Status", "Remark", "UpdateBy").
		Updates(dictData).Error; err != nil {
		if database.IsDuplicateKey(err) {
			return fmt.Errorf("%w: %w", common.ErrDuplicateKey, err)
		}
		return err
	}
	return nil
}

// DeleteData 软删除字典数据。与 DeleteType 同理，删除前改写 value 释放
// uk_dict_type_value 占用，否则同类型下相同的键值再也建不出来。
// 记录不存在时返回 gorm.ErrRecordNotFound，由 Service 转成 404。
func (r *dictRepository) DeleteData(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var dictData model.SysDictData
		if err := tx.First(&dictData, id).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.SysDictData{}).Where("id = ?", dictData.ID).
			Update("value", common.FreedUniqueValue(dictData.Value, dictData.ID, 128)).Error; err != nil {
			return err
		}
		return tx.Delete(&model.SysDictData{}, id).Error
	})
}
