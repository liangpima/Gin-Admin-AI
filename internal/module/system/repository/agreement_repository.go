package repository

import (
	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

type AgreementRepository interface {
	Create(agreement *model.SysAgreement) error
	FindByID(id uint) (*model.SysAgreement, error)
	FindByType(typ string) (*model.SysAgreement, error)
	FindList(name, typ string, status *int8, page, pageSize int) ([]model.SysAgreement, int64, error)
	Update(agreement *model.SysAgreement) error
	Delete(id uint) error
}

type agreementRepository struct {
	db *gorm.DB
}

func NewAgreementRepository() AgreementRepository {
	return &agreementRepository{db: database.DB}
}

func (r *agreementRepository) Create(agreement *model.SysAgreement) error {
	return r.db.Create(agreement).Error
}

func (r *agreementRepository) FindByID(id uint) (*model.SysAgreement, error) {
	var agreement model.SysAgreement
	err := r.db.First(&agreement, id).Error
	return &agreement, err
}

func (r *agreementRepository) FindByType(typ string) (*model.SysAgreement, error) {
	var agreement model.SysAgreement
	err := r.db.Where("type = ? AND status = 1", typ).Order("sort ASC, id DESC").First(&agreement).Error
	return &agreement, err
}

func (r *agreementRepository) FindList(name, typ string, status *int8, page, pageSize int) ([]model.SysAgreement, int64, error) {
	var list []model.SysAgreement
	var total int64

	query := r.db.Model(&model.SysAgreement{})
	if name != "" {
		query = query.Where("title LIKE ?", "%"+common.EscapeLike(name)+"%")
	}
	if typ != "" {
		query = query.Where("type = ?", typ)
	}
	if status != nil {
		query = query.Where("status = ?", *status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).Order("sort ASC, id DESC").Find(&list).Error
	return list, total, err
}

func (r *agreementRepository) Update(agreement *model.SysAgreement) error {
	return r.db.Model(agreement).Select("Title", "Content", "Type", "Sort", "Status", "Remark", "UpdateBy").Updates(agreement).Error
}

func (r *agreementRepository) Delete(id uint) error {
	return r.db.Delete(&model.SysAgreement{}, id).Error
}
