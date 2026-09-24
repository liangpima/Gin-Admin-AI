package model

import (
	"go-admin/internal/common"
)

// SysAgreement 用户协议 / 隐私政策等富文本内容。
//
// 这是**租户内**数据（继承 TenantBaseModel）：每个租户各有自己的协议版本与生效范围，
// 平台级协议属于平台租户（tenant_id=0）。改造前它是全局表，所有租户共用一份，
// 于是任一租户管理员改协议会直接改到其他租户的页面上。
//
// 富文本入库前会经 sanitize.RichText 净化（见 service 层）。
// 净化是必需的：内容由富文本编辑器产出，属于原始 HTML。
type SysAgreement struct {
	common.TenantBaseModel
	Title   string `gorm:"type:varchar(128);comment:标题" json:"title"`
	Content string `gorm:"type:longtext;comment:内容" json:"content"`
	Type    string `gorm:"type:varchar(32);index;comment:类型" json:"type"`
	Sort    int    `gorm:"type:int;default:0;comment:排序" json:"sort"`
	Status  int8   `gorm:"type:tinyint;comment:状态 0停用 1正常" json:"status"`
}

func (SysAgreement) TableName() string {
	return "sys_agreement"
}
