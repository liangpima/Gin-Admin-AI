package dto

import "go-admin/internal/common"

type CreateUserRequest struct {
	Username string `json:"username" binding:"required,min=2,max=64"`
	Password string `json:"password" binding:"required,min=6,max=128"`
	Nickname string `json:"nickname" binding:"max=64"`
	Email    string `json:"email" binding:"omitempty,email"`
	Phone    string `json:"phone" binding:"omitempty,len=11"`
	Status   int8   `json:"status" binding:"oneof=0 1"`
	DeptID   uint   `json:"deptId" binding:"required"`
	RoleIds  []uint `json:"roleIds"`
	PostIds  []uint `json:"postIds"`
	Remark   string `json:"remark" binding:"max=500"`
}

type UpdateUserRequest struct//
// 与 UpdateMemberRequest / UpdateRoleRequest 同一套约定：**部分更新**。
// 数值字段与「可清空」字段用指针，以区分「未提供」与「显式设为 0 / 空串」——
// 用值类型的话，oneof=0 1 这类校验会对缺省零值生效，把部分更新请求挡在门外
// （角色与会员就各踩过一次，一个导致权限分配恒 400，一个把会员改成停用）。
{
	ID       uint    `json:"id" binding:"required"`
	Nickname *string `json:"nickname" binding:"omitempty,max=64"`
	Email    *string `json:"email" binding:"omitempty,email"`
	Phone    *string `json:"phone" binding:"omitempty,len=11"`
	Status   *int8   `json:"status" binding:"omitempty,oneof=0 1"`
	DeptID   *uint   `json:"deptId"`
	RoleIds  []uint  `json:"roleIds"`
	PostIds  []uint  `json:"postIds"`
	Remark   *string `json:"remark" binding:"omitempty,max=500"`
}

type ResetPasswordRequest struct {
	ID       uint   `json:"id" binding:"required"`
	Password string `json:"password" binding:"required,min=6,max=128"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required,min=6,max=128"`
}

type UpdateUserRolesRequest struct {
	ID      uint   `json:"id" binding:"required"`
	RoleIds []uint `json:"roleIds"`
}

type UpdateUserDeptRequest struct {
	ID     uint `json:"id" binding:"required"`
	DeptID uint `json:"deptId" binding:"required"`
}

type UserListRequest struct {
	Username string `json:"username" form:"username"`
	Phone    string `json:"phone" form:"phone"`
	Status   *int8  `json:"status" form:"status"`
	DeptID   uint   `json:"deptId" form:"deptId"`
	// 分页参数统一内嵌：绑定与归一化走 common.BindPage
	common.PageQuery
}
