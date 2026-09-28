package repository

import (
	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

type FileRepository interface {
	Create(file *model.SysFile) error
	FindByID(tenantID, id uint) (*model.SysFile, error)
	FindList(tenantID uint, name, mimeType, sortOrder string, page, pageSize int) ([]model.SysFile, int64, error)
	Delete(tenantID, id uint) error
}

type fileRepository struct {
	db *gorm.DB
}

func NewFileRepository() FileRepository {
	return &fileRepository{db: database.DB}
}

// Create 创建文件记录；TenantID 由 Service 层赋值
func (r *fileRepository) Create(file *model.SysFile) error {
	return r.db.Create(file).Error
}

func (r *fileRepository) FindByID(tenantID, id uint) (*model.SysFile, error) {
	var file model.SysFile
	err := common.TenantScope(r.db, tenantID).First(&file, id).Error
	return &file, err
}

func (r *fileRepository) FindList(tenantID uint, name, mimeType, sortOrder string, page, pageSize int) ([]model.SysFile, int64, error) {
	var files []model.SysFile
	var total int64

	query := common.TenantScope(r.db, tenantID).Model(&model.SysFile{})
	if name != "" {
		query = query.Where("name LIKE ?", "%"+common.EscapeLike(name)+"%")
	}
	if mimeType != "" {
		query = query.Where("mime_type LIKE ?", common.EscapeLike(mimeType)+"%")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// order 仅取白名单值，避免 SQL 注入
	order := "id DESC"
	if sortOrder == "asc" {
		order = "id ASC"
	}

	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).Order(order).Find(&files).Error
	return files, total, err
}

// Delete 软删除文件记录。
//
// 必须检查影响行数：GORM 删除不存在的记录不报错、RowsAffected 为 0，
// 不检查就会「删除成功」而什么都没删。同仓的 agreement / dept 都已检查，
// 这里此前漏了。
func (r *fileRepository) Delete(tenantID, id uint) error {
	res := common.TenantScope(r.db, tenantID).Delete(&model.SysFile{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
