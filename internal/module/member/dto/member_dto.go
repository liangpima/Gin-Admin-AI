package dto

import "go-admin/internal/common"

type CreateMemberRequest struct {
	Username string `json:"username" binding:"max=64"`
	Nickname string `json:"nickname" binding:"max=64"`
	Phone    string `json:"phone" binding:"required,len=11"`
	Avatar   string `json:"avatar" binding:"max=512"`
	Gender   int8   `json:"gender" binding:"oneof=0 1 2"`
	Birthday string `json:"birthday" binding:"omitempty"`
	LevelID  uint   `json:"levelId"`
	Status   int8   `json:"status" binding:"oneof=0 1"`
	TagIds   []uint `json:"tagIds"`
	Remark   string `json:"remark" binding:"max=500"`
}

// UpdateMemberRequest 会员更新，支持**部分更新**。
//
// 为什么数值字段用指针：前端的「修改等级」「修改标签」只提交 {id, levelId}
// 或 {id, tagIds}，其余字段是零值。用值类型会出事 —— 实测把某会员的
// status(1)/gender(2) 在只改等级后变成了 0/0，也就是**顺手把人停用了**；
// 而单纯放宽校验又会让用户无法把会员改成停用(status=0)。指针能区分
// 「未提供」与「显式设为 0」。
//
// Birthday 也用指针：空串在这里是有意义的（表示清空生日），
// 不能与「未提供」混为一谈。
type UpdateMemberRequest struct {
	ID       uint    `json:"id" binding:"required"`
	Username string  `json:"username" binding:"max=64"`
	Nickname string  `json:"nickname" binding:"max=64"`
	Phone    string  `json:"phone" binding:"omitempty,len=11"`
	Avatar   string  `json:"avatar" binding:"max=512"`
	Gender   *int8   `json:"gender" binding:"omitempty,oneof=0 1 2"`
	Birthday *string `json:"birthday"`
	LevelID  *uint   `json:"levelId"`
	Status   *int8   `json:"status" binding:"omitempty,oneof=0 1"`
	TagIds   []uint  `json:"tagIds"`
	Remark   *string `json:"remark" binding:"omitempty,max=500"`
}

type MemberListRequest struct {
	Phone    string `json:"phone" form:"phone"`
	Nickname string `json:"nickname" form:"nickname"`
	LevelID  uint   `json:"levelId" form:"levelId"`
	Status   *int8  `json:"status" form:"status"`
	// 分页参数统一内嵌：绑定与归一化走 common.BindPage
	common.PageQuery
}

type UpdateMemberStatusRequest struct {
	ID     uint `json:"id" binding:"required"`
	Status int8 `json:"status" binding:"oneof=0 1"`
}

type UpdateMemberTagsRequest struct {
	ID     uint   `json:"id" binding:"required"`
	TagIds []uint `json:"tagIds"`
}

type CreateMemberLevelRequest struct {
	Name      string  `json:"name" binding:"required,max=64"`
	MinPoints int64   `json:"minPoints"`
	Discount  float64 `json:"discount" binding:"min=1,max=10"`
	Icon      string  `json:"icon" binding:"max=256"`
	Sort      int     `json:"sort"`
	Status    int8    `json:"status" binding:"oneof=0 1"`
}

type UpdateMemberLevelRequest struct//
// 与 UpdateMemberRequest / UpdateRoleRequest 同一套约定：**部分更新**。
// 数值字段与「可清空」字段用指针，以区分「未提供」与「显式设为 0 / 空串」——
// 用值类型的话，oneof=0 1 这类校验会对缺省零值生效，把部分更新请求挡在门外
// （角色与会员就各踩过一次，一个导致权限分配恒 400，一个把会员改成停用）。
{
	ID        uint     `json:"id" binding:"required"`
	Name      string   `json:"name" binding:"omitempty,max=64"`
	MinPoints *int64   `json:"minPoints"`
	// 折扣率仍用浮点：它在库里就是浮点列，改整数需要迁移，
	// 且不参与金额计算（只作为展示与换算系数），风险低于改动面。
	Discount  *float64 `json:"discount" binding:"omitempty,min=1,max=10"`
	Icon      *string  `json:"icon" binding:"omitempty,max=256"`
	Sort      *int     `json:"sort"`
	Status    *int8    `json:"status" binding:"omitempty,oneof=0 1"`
}

type MemberLevelListRequest struct {
	Name     string `json:"name" form:"name"`
	// 分页参数统一内嵌：绑定与归一化走 common.BindPage
	common.PageQuery
}

type CreateMemberTagRequest struct {
	Name   string `json:"name" binding:"required,max=64"`
	Color  string `json:"color" binding:"max=20"`
	Sort   int    `json:"sort"`
	Status int8   `json:"status" binding:"oneof=0 1"`
}

type UpdateMemberTagRequest struct//
// 与 UpdateMemberRequest / UpdateRoleRequest 同一套约定：**部分更新**。
// 数值字段与「可清空」字段用指针，以区分「未提供」与「显式设为 0 / 空串」——
// 用值类型的话，oneof=0 1 这类校验会对缺省零值生效，把部分更新请求挡在门外
// （角色与会员就各踩过一次，一个导致权限分配恒 400，一个把会员改成停用）。
{
	ID     uint    `json:"id" binding:"required"`
	Name   string  `json:"name" binding:"omitempty,max=64"`
	Color  *string `json:"color" binding:"omitempty,max=20"`
	Sort   *int    `json:"sort"`
	Status *int8   `json:"status" binding:"omitempty,oneof=0 1"`
}

type MemberTagListRequest struct {
	Name     string `json:"name" form:"name"`
	// 分页参数统一内嵌：绑定与归一化走 common.BindPage
	common.PageQuery
}

type PointsLogListRequest struct {
	MemberID uint `json:"memberId" form:"memberId"`
	Type     int8 `json:"type" form:"type"`
	// 分页参数统一内嵌：绑定与归一化走 common.BindPage
	common.PageQuery
}
