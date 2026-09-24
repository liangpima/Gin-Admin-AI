package repository

import (
	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

type DashboardRepository interface {
	GetStats(tenantID uint) (*model.DashboardStats, error)
}

type dashboardRepository struct {
	db *gorm.DB
}

func NewDashboardRepository() DashboardRepository {
	return &dashboardRepository{db: database.DB}
}

// GetStats 汇总仪表盘统计数据。
//
// 每张表的统计口径必须与它的租户模型一致，否则数字只是「偏大」而不报错：
//
//	按租户过滤：sys_user / sys_role / sys_post / sys_operation_log / sys_dept
//	全局表：    sys_menu / sys_config
//
// 关于 sys_dept：它的模型继承 TenantBaseModel、表里有 tenant_id，
// deptRepository 的每个方法也都施加了 TenantScope（部门是租户内数据，
// 见 sql/migrations/2026-09-25-dept-tenant.sql）。这里**必须**一起过滤 ——
// 早前把它当成全局表，导致每个租户的仪表盘都显示全平台的部门数量，
// 既不准确，也顺带把平台规模泄漏给了租户。
//
// 注意 AGENTS.md 规则 7 的表清单里 sys_dept 仍被列为全局表，那是迁移前的
// 旧描述，以本文件与 deptRepository 为准。
func (r *dashboardRepository) GetStats(tenantID uint) (*model.DashboardStats, error) {
	stats := &model.DashboardStats{}

	counters := []struct {
		query *gorm.DB
		dest  *int64
	}{
		{common.TenantScope(r.db.Model(&model.SysUser{}), tenantID), &stats.UserCount},
		{common.TenantScope(r.db.Model(&model.SysRole{}), tenantID), &stats.RoleCount},
		{r.db.Model(&model.SysMenu{}), &stats.MenuCount},
		{common.TenantScope(r.db.Model(&model.SysDept{}), tenantID), &stats.DeptCount},
		{common.TenantScope(r.db.Model(&model.SysPost{}), tenantID), &stats.PostCount},
		{r.db.Model(&model.SysConfig{}), &stats.ConfigCount},
		{common.TenantScope(r.db.Model(&model.SysOperationLog{}), tenantID), &stats.LogCount},
	}

	for _, c := range counters {
		if err := c.query.Count(c.dest).Error; err != nil {
			return nil, err
		}
	}

	return stats, nil
}
