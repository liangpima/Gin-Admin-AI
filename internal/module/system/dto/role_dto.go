package dto

import "go-admin/internal/common"

type CreateRoleRequest struct {
	Name      string `json:"name" binding:"required,min=2,max=64"`
	Code      string `json:"code" binding:"required,min=2,max=64"`
	Sort      int    `json:"sort"`
	Status    int8   `json:"status" binding:"oneof=0 1"`
	DataScope int8   `json:"dataScope" binding:"oneof=1 2 3 4 5"`
	MenuIds   []uint `json:"menuIds"`
	Remark    string `json:"remark" binding:"max=500"`
}

// UpdateRoleRequest 角色更新，支持**部分更新**。
//
// 为什么数值字段用指针：前端的「保存权限」只提交 {id, menuIds}，
// 此时 sort/status/dataScope 都是零值。若用值类型，校验规则
// （oneof=1 2 3 4 5 等）会直接判失败 —— 事实上「权限分配」功能此前
// 一直是 400 报错、完全不可用；反过来若放宽校验并「零值即不改」，
// 又会让用户无法把角色改成「停用(status=0)」。
// 指针能同时区分「未提供」与「显式设为 0」。
type UpdateRoleRequest struct {
	ID        uint    `json:"id" binding:"required"`
	Name      string  `json:"name" binding:"omitempty,min=2,max=64"`
	Code      string  `json:"code" binding:"omitempty,min=2,max=64"`
	Sort      *int    `json:"sort"`
	Status    *int8   `json:"status" binding:"omitempty,oneof=0 1"`
	DataScope *int8   `json:"dataScope" binding:"omitempty,oneof=1 2 3 4 5"`
	MenuIds   []uint  `json:"menuIds"`
	Remark    *string `json:"remark" binding:"omitempty,max=500"`
}

type RoleListRequest struct {
	Name   string `json:"name" form:"name"`
	Code   string `json:"code" form:"code"`
	Status *int8  `json:"status" form:"status"`
	// 分页参数统一内嵌：绑定与归一化走 common.BindPage
	common.PageQuery
}
