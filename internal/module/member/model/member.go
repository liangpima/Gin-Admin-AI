package model

import (
	"time"

	"go-admin/internal/common"

	"gorm.io/gorm"
)

// Member 会员。
//
// 会员是**租户内**数据（继承 TenantBaseModel），但表上两个唯一索引的
// 范围**刻意不同**，改动前务必先看清楚：
//
//   - `uk_tenant_phone`(tenant_id, phone)：手机号**租户内**唯一。
//     两个租户可以各自拥有同一手机号的会员，与 `FindByPhone(tenantID, ...)`
//     的按租户查重一致。这里**不能**给 Phone 打 `uniqueIndex` 标签 ——
//     GORM 无法把索引字段指到嵌入结构体里的 TenantID（定义在
//     common.TenantBaseModel），只给 Phone 打标签会建出一个**全局**唯一索引，
//     正好是反的。真正的复合唯一索引由 sql/init.sql 与
//     sql/migrations/2026-09-28-member-phone-tenant.sql 的 DDL 建立。
//
//   - `uk_member_no`(member_no)：会员编号**全平台**唯一，由全平台共用的
//     序列发出（见 memberRepository.FindMaxMemberNo）。因此 MemberNo 上的
//     `uniqueIndex` 标签与生产语义一致，保留。
type Member struct {
	common.TenantBaseModel
	MemberNo      string         `gorm:"type:varchar(32);uniqueIndex;comment:会员编号" json:"memberNo"`
	Username      string         `gorm:"type:varchar(64);comment:用户名" json:"username"`
	Nickname      string         `gorm:"type:varchar(64);comment:昵称" json:"nickname"`
	Avatar        string         `gorm:"type:varchar(512);comment:头像" json:"avatar"`
	Phone         string         `gorm:"type:varchar(20);comment:手机号" json:"phone"`
	Gender        int8           `gorm:"type:tinyint;default:0;comment:性别 0未知 1男 2女" json:"gender"`
	Birthday      *time.Time     `gorm:"comment:出生日期" json:"birthday"`
	LevelID       uint           `gorm:"comment:等级ID" json:"levelId"`
	Status        int8           `gorm:"type:tinyint;comment:状态 0停用 1正常" json:"status"`
	Points        int64          `gorm:"comment:积分" json:"points"`
	WechatOpenid  string         `gorm:"type:varchar(128);index;comment:微信小程序openid" json:"wechatOpenid"`
	RegisterTime  time.Time      `gorm:"comment:注册时间" json:"registerTime"`
	LastVisitTime *time.Time     `gorm:"comment:最后访问时间" json:"lastVisitTime"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Member) TableName() string {
	return "pay_member"
}

type MemberLevel struct {
	common.TenantBaseModel
	Name      string  `gorm:"type:varchar(64);not null;comment:等级名称" json:"name"`
	MinPoints int64   `gorm:"comment:最低积分" json:"minPoints"`
	Discount  float64 `gorm:"type:decimal(3,1);default:10.0;comment:折扣 10表示不打折 8表示八折" json:"discount"`
	Icon      string  `gorm:"type:varchar(256);comment:等级图标" json:"icon"`
	Sort      int     `gorm:"comment:排序" json:"sort"`
	Status    int8    `gorm:"type:tinyint;comment:状态" json:"status"`
}

func (MemberLevel) TableName() string {
	return "pay_member_level"
}

type MemberTag struct {
	common.TenantBaseModel
	Name   string `gorm:"type:varchar(64);not null;comment:标签名称" json:"name"`
	Color  string `gorm:"type:varchar(20);default:#409eff;comment:标签颜色" json:"color"`
	Sort   int    `gorm:"comment:排序" json:"sort"`
	Status int8   `gorm:"type:tinyint;comment:状态" json:"status"`
}

func (MemberTag) TableName() string {
	return "pay_member_tag"
}

type MemberTagRel struct {
	MemberID uint `gorm:"primaryKey;comment:会员ID" json:"memberId"`
	TagID    uint `gorm:"primaryKey;comment:标签ID" json:"tagId"`
}

func (MemberTagRel) TableName() string {
	return "pay_member_tag_rel"
}

type PointsLog struct {
	common.TenantBaseModel
	MemberID uint   `gorm:"comment:会员ID" json:"memberId"`
	Change   int64  `gorm:"column:points_change;comment:变更积分" json:"change"`
	Type     int8   `gorm:"type:tinyint;default:1;comment:类型 1获取 2消费" json:"type"`
	Source   string `gorm:"type:varchar(64);comment:来源" json:"source"`
	OrderNo  string `gorm:"type:varchar(64);comment:关联订单号" json:"orderNo"`
}

func (PointsLog) TableName() string {
	return "pay_points_log"
}
