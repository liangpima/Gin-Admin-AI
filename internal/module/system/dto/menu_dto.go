package dto

type CreateMenuRequest struct {
	ParentID   uint   `json:"parentId"`
	Name       string `json:"name" binding:"required,max=64"`
	Path       string `json:"path" binding:"max=200"`
	Component  string `json:"component" binding:"max=200"`
	Redirect   string `json:"redirect" binding:"max=200"`
	Icon       string `json:"icon" binding:"max=64"`
	Title      string `json:"title" binding:"max=64"`
	Type       int8   `json:"type" binding:"required,oneof=0 1 2"`
	Permission string `json:"permission" binding:"max=200"`
	Sort       int    `json:"sort"`
	Visible    int8   `json:"visible" binding:"oneof=0 1"`
	Status     int8   `json:"status" binding:"oneof=0 1"`
	IsExternal int8   `json:"isExternal" binding:"oneof=0 1"`
	IsCache    int8   `json:"isCache" binding:"oneof=0 1"`
}

type UpdateMenuRequest struct//
// 与 UpdateMemberRequest / UpdateRoleRequest 同一套约定：**部分更新**。
// 数值字段与「可清空」字段用指针，以区分「未提供」与「显式设为 0 / 空串」——
// 用值类型的话，oneof=0 1 这类校验会对缺省零值生效，把部分更新请求挡在门外
// （角色与会员就各踩过一次，一个导致权限分配恒 400，一个把会员改成停用）。
{
	ID         uint    `json:"id" binding:"required"`
	ParentID   *uint   `json:"parentId"`
	Name       string  `json:"name" binding:"omitempty,max=64"`
	Path       *string `json:"path" binding:"omitempty,max=200"`
	Component  *string `json:"component" binding:"omitempty,max=200"`
	Redirect   *string `json:"redirect" binding:"omitempty,max=200"`
	Icon       *string `json:"icon" binding:"omitempty,max=64"`
	Title      string  `json:"title" binding:"omitempty,max=64"`
	Type       *int8   `json:"type" binding:"omitempty,oneof=0 1 2"`
	Permission *string `json:"permission" binding:"omitempty,max=200"`
	Sort       *int    `json:"sort"`
	Visible    *int8   `json:"visible" binding:"omitempty,oneof=0 1"`
	Status     *int8   `json:"status" binding:"omitempty,oneof=0 1"`
	IsExternal *int8   `json:"isExternal" binding:"omitempty,oneof=0 1"`
	IsCache    *int8   `json:"isCache" binding:"omitempty,oneof=0 1"`
}
