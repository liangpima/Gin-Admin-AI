package repository

import (
	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

// AgreementRepository 协议数据访问。
//
// 所有方法都接收 tenantID 并施加 TenantScope —— 协议是租户内数据
// （见 model.SysAgreement 的注释）。签名层面强制传参是防漏传的第一道手段：
// 漏传时会拿到 0，而 `TenantScope(db, 0)` **不过滤**，于是静默退化成全表查询。
type AgreementRepository interface {
	Create(agreement *model.SysAgreement) error
	FindByID(tenantID, id uint) (*model.SysAgreement, error)
	// FindByType 取某类型下「排序最前且启用中」的那一条（用于前台展示）
	FindByType(tenantID uint, typ string) (*model.SysAgreement, error)
	FindList(tenantID uint, name, typ string, status *int8, page, pageSize int) ([]model.SysAgreement, int64, error)
	Update(tenantID uint, agreement *model.SysAgreement) error
	Delete(tenantID, id uint) error
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

func (r *agreementRepository) FindByID(tenantID, id uint) (*model.SysAgreement, error) {
	var agreement model.SysAgreement
	err := common.TenantScope(r.db, tenantID).First(&agreement, id).Error
	return &agreement, err
}

func (r *agreementRepository) FindByType(tenantID uint, typ string) (*model.SysAgreement, error) {
	var agreement model.SysAgreement
	err := common.TenantScope(r.db, tenantID).
		Where("type = ? AND status = 1", typ).
		Order("sort ASC, id DESC").
		First(&agreement).Error
	return &agreement, err
}

func (r *agreementRepository) FindList(tenantID uint, name, typ string, status *int8, page, pageSize int) ([]model.SysAgreement, int64, error) {
	var list []model.SysAgreement
	var total int64

	query := common.TenantScope(r.db.Model(&model.SysAgreement{}), tenantID)
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

// Update 按租户范围更新，避免用别租户的 ID 改到别人的协议。
func (r *agreementRepository) Update(tenantID uint, agreement *model.SysAgreement) error {
	tx := common.TenantScope(r.db.Model(&model.SysAgreement{}), tenantID).
		Where("id = ?", agreement.ID).
		Select("Title", "Content", "Type", "Sort", "Status", "Remark", "UpdateBy").
		Updates(agreement)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *agreementRepository) Delete(tenantID, id uint) error {
	tx := common.TenantScope(r.db, tenantID).Delete(&model.SysAgreement{}, id)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
