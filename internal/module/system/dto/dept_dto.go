package dto

type CreateDeptRequest struct {
	ParentID uint   `json:"parentId"`
	Name     string `json:"name" binding:"required,max=64"`
	Sort     int    `json:"sort"`
	Leader   string `json:"leader" binding:"max=64"`
	Phone    string `json:"phone" binding:"max=16"`
	Email    string `json:"email" binding:"max=128"`
	Status   int8   `json:"status" binding:"oneof=0 1"`
}

type UpdateDeptRequest struct//
// 与 UpdateMemberRequest / UpdateRoleRequest 同一套约定：**部分更新**。
// 数值字段与「可清空」字段用指针，以区分「未提供」与「显式设为 0 / 空串」——
// 用值类型的话，oneof=0 1 这类校验会对缺省零值生效，把部分更新请求挡在门外
// （角色与会员就各踩过一次，一个导致权限分配恒 400，一个把会员改成停用）。
{
	ID       uint    `json:"id" binding:"required"`
	ParentID *uint   `json:"parentId"`
	Name     string  `json:"name" binding:"omitempty,max=64"`
	Sort     *int    `json:"sort"`
	Leader   *string `json:"leader" binding:"omitempty,max=64"`
	Phone    *string `json:"phone" binding:"omitempty,max=16"`
	Email    *string `json:"email" binding:"omitempty,max=128"`
	Status   *int8   `json:"status" binding:"omitempty,oneof=0 1"`
}
