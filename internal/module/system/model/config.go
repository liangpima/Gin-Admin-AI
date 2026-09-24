package model

import (
	"go-admin/internal/common"
)

// SysConfig 平台级配置。
//
// **有意的全局表**：不继承 TenantBaseModel、没有 tenant_id，且**不要**给查询加
// TenantScope（加会因无 tenant_id 列直接 SQL 报错）。
//
// 为什么它必须全局：这里放的是 OSS / 支付 / 短信等**平台级凭据**与站点开关，
// 同一份配置服务所有租户。按租户复制一份会让每个租户各自持有一套密钥，
// 而「平台的支付账号」在业务上本来就只有一套。
//
// 由此带来的安全边界（见 router.go）：
//   - 读：任何持有 config:list 的角色都能读，但敏感项的值在 Service 层打码
//     （maskConfig / maskedValue），不会把密钥原文吐出去
//   - 写：走 protectedAdmin —— 权限码之外再要求 admin 角色。
//     否则任何拿到 config:edit 的租户管理员都能把全平台共用的凭据换成自己的，
//     影响范围是所有租户，而不只是他自己
//
// 若将来需要「租户可自行维护的站点展示配置」，正确做法是**另开一张租户级表**
// （继承 TenantBaseModel），而不是给本表加 tenant_id —— 那会让平台级凭据
// 变成「每个租户一份」，与它的用途冲突。
type SysConfig struct {
	common.BaseModel
	Name     string `gorm:"type:varchar(128);comment:参数名称" json:"name"`
	ConfigKey string `gorm:"type:varchar(191);uniqueIndex;column:config_key;comment:参数键名" json:"key"`
	Value    string `gorm:"type:text;comment:参数键值" json:"value"`
	Type     int8   `gorm:"type:tinyint;comment:系统内置 0是 1否" json:"type"`
}

func (SysConfig) TableName() string {
	return "sys_config"
}
