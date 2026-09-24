package model

import (
	"go-admin/internal/common"
)

// SysDept 部门。
//
// 部门是**租户内**的基础数据（与 sys_post 同类，区别于 sys_menu / sys_dict 这类全局表），
// 因此继承 TenantBaseModel，所有查询必须带 tenant_id 过滤。
//
// 为什么必须隔离：部门名、负责人、联系电话、邮箱都是租户的组织信息。
// 不隔离时租户 A 的管理员可以枚举、改名、删除租户 B 的部门 ——
// 而且 sys_user.dept_id 指向别的租户的部门时不会有任何报错，
// 只是用户归属悄悄错位，从界面上看不出来。
//
// 这里刻意**不加** `(tenant_id, name)` 唯一索引：sys_dept 原本就没有 name 上的
// 唯一约束，业务允许同名部门（例如集团下多个子公司都有「市场部」）。
// 加约束属于超出本次修复范围的行为变更，会让现有数据的插入突然失败。
type SysDept struct {
	common.TenantBaseModel
	ParentID uint      `gorm:"default:0;comment:父部门ID" json:"parentId"`
	Name     string    `gorm:"type:varchar(64);comment:部门名称" json:"name"`
	Sort     int       `gorm:"type:int;default:0;comment:排序" json:"sort"`
	Leader   string    `gorm:"type:varchar(64);comment:负责人" json:"leader"`
	Phone    string    `gorm:"type:varchar(16);comment:联系电话" json:"phone"`
	Email    string    `gorm:"type:varchar(128);comment:邮箱" json:"email"`
	Status   int8      `gorm:"type:tinyint;comment:状态" json:"status"`
	Children []SysDept `gorm:"-" json:"children"`
}

func (SysDept) TableName() string {
	return "sys_dept"
}
