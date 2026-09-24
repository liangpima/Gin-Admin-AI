package model

import (
	"go-admin/internal/common"
)

// 字典是**有意的平台级数据**：sys_dict_type / sys_dict_data 都不继承
// TenantBaseModel、没有 tenant_id，查询也**不要**套 TenantScope
// （套了会因无 tenant_id 列直接 SQL 报错）。
//
// 为什么必须全局共享：字典是「值 → 显示文案」的**共同词汇表**，
// 而字典类型编码是代码里的字面量（前端 `useDict('sys_user_status')`、
// 后端校验也按固定编码取选项）。按租户各存一份会让同一套编码在不同租户下
// 指向不同数据，代码里的字面量立刻失去确定性 —— 新增一个状态值时
// 还得保证每个租户都同步，实际上做不到。
//
// 由此带来的残留风险（已知并接受，记录在此以免后来者重新发现一遍）：
// 持有 dict:add/edit/delete 的角色改的是**所有租户共用**的文案与选项，
// 影响范围跨租户。当前默认只有 admin 角色持有这些权限码；
// 若某个部署要把字典维护下放给租户管理员，需要另行评估
// （或参照 config 的做法用 middleware.RequireAdminRole 收窄写入）。
//
// 2026-09-24 复核结论：**保持现状，不额外收窄**。理由有二 ——
//   1. 默认部署下没有实际暴露：dict 的写权限码默认只授予 admin
//      （init.sql 里除 admin 外没有任何角色持有它们）
//   2. 是否允许租户管理员维护字典属于产品决策，不是安全缺陷：
//      如果某个部署就是要让租户自己维护文案（哪怕数据是共享的），
//      单方面收窄会直接打断他们的流程
// 因此这里把风险写在注释里而不是写进代码：需要收窄时改 router.go 的
// 字典写路由为 protectedAdmin 即可（一行一处）。
//
// 读取侧不受影响：业务页面取选项走 `GET /dict/data/type/:type`（仅登录态），
// 因此任何登录用户都能拿到下拉/标签所需的引用数据。

type SysDictType struct {
	common.BaseModel
	Name   string `gorm:"type:varchar(128);comment:字典名称" json:"name"`
	Type   string `gorm:"type:varchar(128);uniqueIndex;comment:字典类型" json:"type"`
	Status int8   `gorm:"type:tinyint;comment:状态" json:"status"`
}

func (SysDictType) TableName() string {
	return "sys_dict_type"
}

type SysDictData struct {
	common.BaseModel
	// DictType + Value 组成复合唯一索引：同一类型下键值不允许重复。
	// 没有它的话，同一个 value 可以建出多个 label，前端按下拉取值时
	// 回显哪一条完全取决于查询顺序，属于随机行为。
	// 软删除时 DeleteData 会改写 value 释放该组合，故删除后同名可重建。
	DictType  string `gorm:"type:varchar(128);uniqueIndex:uk_dict_type_value;comment:字典类型" json:"dictType"`
	Label     string `gorm:"type:varchar(128);comment:字典标签" json:"label"`
	Value     string `gorm:"type:varchar(128);uniqueIndex:uk_dict_type_value;comment:字典键值" json:"value"`
	Sort      int    `gorm:"type:int;default:0;comment:排序" json:"sort"`
	CssClass  string `gorm:"type:varchar(128);comment:样式属性" json:"cssClass"`
	ListClass string `gorm:"type:varchar(128);comment:表格回显样式" json:"listClass"`
	Status    int8   `gorm:"type:tinyint;comment:状态" json:"status"`
}

func (SysDictData) TableName() string {
	return "sys_dict_data"
}
