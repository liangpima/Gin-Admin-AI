package dto

type CreateMenuRequest struct {
	ParentID   uint   `json:"parentId"`
	Name       string `json:"name" binding:"required,max=64"`
	Path       string `json:"path" binding:"max=200"`
	Component  string `json:"component" binding:"max=200"`
	Redirect   string `json:"redirect" binding:"max=200"`
	Icon       string `json:"icon" binding:"max=64"`
	Title      string `json:"title" binding:"max=64"`
	// Type 菜单类型：0 目录 / 1 菜单 / 2 按钮。
	//
	// ⚠️ 这里**不能**加 `required`：validator 的 required 判的是「不等于零值」，
	// 而 int8 的零值 0 恰好是合法取值（目录）。加上它之后「新增目录型菜单」
	// 会被 400 拒绝，报错还是 Field validation for 'Type' failed on the
	// 'required' tag 这种看不出所以然的话 —— 前端菜单表单默认 type=1，
	// 只有用户主动选「目录」时才会踩到，所以很容易长期没被发现。
	//
	// `oneof=0 1 2` 已经能挡掉非法取值（3、-1 等）；省略该字段时按 0（目录）处理，
	// 这是可接受的默认值（前端始终显式传该字段）。
	Type       int8   `json:"type" binding:"oneof=0 1 2"`
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
