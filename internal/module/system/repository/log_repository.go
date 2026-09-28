package repository

import (
	"context"
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
//
// tenantID == 0 时返回**业务错误**（403）而不是普通 error：这里拒绝的是一次
// 平台级账号发起的清空请求，属于「知道你是谁，但这件事不归你做」，
// 而不是系统故障。此前用 errors.New 会被 FailWith 归一成 500，
// 超管点「清空」永远只看到「服务器内部错误」，既清不掉也不知道为什么。
//
// 为什么守卫放在仓储而不是 Service：`TenantScope(db, 0)` 的语义是**不过滤**，
// 一旦这个条件漏出去，DELETE 会删掉全平台所有租户的日志。它是一条数据安全不变量，
// 放在最靠近 SQL 的位置才拦得住所有调用路径。
func (r *logRepository) ClearOperationLogs(tenantID uint) error {
	if tenantID == 0 {
		return common.NewForbiddenError("平台级账号不支持清空日志：日志按租户隔离，请切换到具体租户后再操作")
	}
	return common.TenantScope(r.db, tenantID).Delete(&model.SysOperationLog{}).Error
}

// ClearLoginLogs 清空当前租户的登录日志。
// 必须携带租户上下文，避免误删全平台数据。错误语义同 ClearOperationLogs。
func (r *logRepository) ClearLoginLogs(tenantID uint) error {
	if tenantID == 0 {
		return common.NewForbiddenError("平台级账号不支持清空登录日志：日志按租户隔离，请切换到具体租户后再操作")
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
