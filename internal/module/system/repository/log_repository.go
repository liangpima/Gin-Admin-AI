package repository

import (
	"context"
	"errors"
	"time"

	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

type LogRepository interface {
	CreateOperationLog(log *model.SysOperationLog) error
	CreateLoginLog(log *model.SysLoginLog) error
	FindOperationLogList(tenantID uint, title string, status *int8, page, pageSize int) ([]model.SysOperationLog, int64, error)
	FindLoginLogList(tenantID uint, username string, status *int8, page, pageSize int) ([]model.SysLoginLog, int64, error)
	ClearOperationLogs(tenantID uint) error
	ClearLoginLogs(tenantID uint) error
	// 供定时任务使用：按时间清理全部租户的历史日志。
	// 接收 ctx 是为了能设超时 —— GORM 默认不给查询设超时，
	// 大表 DELETE 一旦阻塞会一直占着连接和行锁，直到数据库侧超时。
	DeleteOperationLogsBefore(ctx context.Context, before time.Time) (int64, error)
	DeleteLoginLogsBefore(ctx context.Context, before time.Time) (int64, error)
}

type logRepository struct {
	db *gorm.DB
}

func NewLogRepository() LogRepository {
	return &logRepository{db: database.DB}
}

// CreateOperationLog 创建操作日志；TenantID 由调用方赋值
func (r *logRepository) CreateOperationLog(log *model.SysOperationLog) error {
	return r.db.Create(log).Error
}

// CreateLoginLog 创建登录日志；TenantID 由调用方赋值
func (r *logRepository) CreateLoginLog(log *model.SysLoginLog) error {
	return r.db.Create(log).Error
}

func (r *logRepository) FindOperationLogList(tenantID uint, title string, status *int8, page, pageSize int) ([]model.SysOperationLog, int64, error) {
	var logs []model.SysOperationLog
	var total int64

	query := common.TenantScope(r.db, tenantID).Model(&model.SysOperationLog{})
	if title != "" {
		query = query.Where("title LIKE ?", "%"+common.EscapeLike(title)+"%")
	}
	if status != nil {
		query = query.Where("status = ?", *status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).Order("id DESC").Find(&logs).Error
	return logs, total, err
}

func (r *logRepository) FindLoginLogList(tenantID uint, username string, status *int8, page, pageSize int) ([]model.SysLoginLog, int64, error) {
	var logs []model.SysLoginLog
	var total int64

	query := common.TenantScope(r.db, tenantID).Model(&model.SysLoginLog{})
	if username != "" {
		query = query.Where("username LIKE ?", "%"+common.EscapeLike(username)+"%")
	}
	if status != nil {
		query = query.Where("status = ?", *status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).Order("id DESC").Find(&logs).Error
	return logs, total, err
}

// ClearOperationLogs 清空当前租户的操作日志。
// 必须携带租户上下文，避免误删全平台数据。
func (r *logRepository) ClearOperationLogs(tenantID uint) error {
	if tenantID == 0 {
		return errors.New("缺少租户上下文，拒绝清空操作日志")
	}
	return common.TenantScope(r.db, tenantID).Delete(&model.SysOperationLog{}).Error
}

// ClearLoginLogs 清空当前租户的登录日志。
// 必须携带租户上下文，避免误删全平台数据。
func (r *logRepository) ClearLoginLogs(tenantID uint) error {
	if tenantID == 0 {
		return errors.New("缺少租户上下文，拒绝清空登录日志")
	}
	return common.TenantScope(r.db, tenantID).Delete(&model.SysLoginLog{}).Error
}

// DeleteOperationLogsBefore 删除指定时间之前的操作日志。
// 不区分租户：供定时清理任务按保留期统一回收，避免日志表无限增长。
//
// 用 WithContext 而非直接 r.db：让 database/sql 能在 ctx 超时后
// 真正中断正在执行的 DELETE，而不是只把调用方放走、查询仍在库上跑。
func (r *logRepository) DeleteOperationLogsBefore(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Where("created_at < ?", before).Delete(&model.SysOperationLog{})
	return result.RowsAffected, result.Error
}

// DeleteLoginLogsBefore 删除指定时间之前的登录日志（不区分租户，供定时清理任务使用）
func (r *logRepository) DeleteLoginLogsBefore(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Where("login_time < ?", before).Delete(&model.SysLoginLog{})
	return result.RowsAffected, result.Error
}
