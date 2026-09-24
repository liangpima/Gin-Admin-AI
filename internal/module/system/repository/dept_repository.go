package repository

import (
	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

// DeptRepository 部门数据访问。
//
// 所有方法都接收 tenantID 并施加 TenantScope —— 部门是租户内数据
// （见 model.SysDept 的注释）。签名层面强制传参是防漏传的第一道手段：
// 漏传时会拿到 0，而 `TenantScope(db, 0)` **不过滤**，于是静默退化成全表查询。
type DeptRepository interface {
	Create(dept *model.SysDept) error
	FindByID(tenantID, id uint) (*model.SysDept, error)
	FindAll(tenantID uint) ([]model.SysDept, error)
	Update(tenantID uint, dept *model.SysDept) error
	Delete(tenantID, id uint) error
	// CountByParentID 统计直属子部门数量，用于删除前校验
	CountByParentID(tenantID, parentID uint) (int64, error)
	// FindParentID 返回部门的父节点 ID，ok=false 表示部门不存在（用于父级成环校验）
	FindParentID(tenantID, id uint) (uint, bool, error)
}

type deptRepository struct {
	db *gorm.DB
}

func NewDeptRepository() DeptRepository {
	return &deptRepository{db: database.DB}
}

func (r *deptRepository) Create(dept *model.SysDept) error {
	return r.db.Create(dept).Error
}

func (r *deptRepository) FindByID(tenantID, id uint) (*model.SysDept, error) {
	var dept model.SysDept
	err := common.TenantScope(r.db, tenantID).First(&dept, id).Error
	return &dept, err
}

func (r *deptRepository) FindAll(tenantID uint) ([]model.SysDept, error) {
	var depts []model.SysDept
	err := common.TenantScope(r.db, tenantID).
		Where("status = ?", 1).
		Order("sort ASC, id ASC").
		Find(&depts).Error
	return depts, err
}

// Update 按租户范围更新，避免拿别租户的 ID 改到别人的部门。
//
// 条件里必须带 tenant_id：只靠主键定位的话，租户 A 传 B 的部门 ID
// 就能改掉 B 的部门（Service 层的 FindByID 已挡住，这里再加一道 ——
// 仓储是最后一道防线，不该假设调用方一定先查过）。
func (r *deptRepository) Update(tenantID uint, dept *model.SysDept) error {
	tx := common.TenantScope(r.db.Model(&model.SysDept{}), tenantID).
		Where("id = ?", dept.ID).
		Select("ParentID", "Name", "Sort", "Leader", "Phone", "Email", "Status", "Remark", "UpdateBy").
		Updates(dept)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *deptRepository) Delete(tenantID, id uint) error {
	tx := common.TenantScope(r.db, tenantID).Delete(&model.SysDept{}, id)
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *deptRepository) CountByParentID(tenantID, parentID uint) (int64, error) {
	var count int64
	err := common.TenantScope(r.db.Model(&model.SysDept{}), tenantID).
		Where("parent_id = ?", parentID).
		Count(&count).Error
	return count, err
}

// FindParentID 查询部门的父节点 ID。
// 带租户过滤：否则可用它探测其他租户的部门是否存在（ok=true 即表示该 ID 存在）。
func (r *deptRepository) FindParentID(tenantID, id uint) (uint, bool, error) {
	var parents []uint
	if err := common.TenantScope(r.db.Model(&model.SysDept{}), tenantID).
		Where("id = ?", id).
		Pluck("parent_id", &parents).Error; err != nil {
		return 0, false, err
	}
	if len(parents) == 0 {
		return 0, false, nil
	}
	return parents[0], true, nil
}
