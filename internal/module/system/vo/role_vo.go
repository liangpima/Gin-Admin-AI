package vo

import "go-admin/internal/module/system/model"

// RoleDetailVO 角色详情（含已授权菜单 ID）。
//
// 为什么需要单独的 VO，而不是直接返回 model.SysRole：
// 前端「权限分配」对话框要靠 menuIds 回显已勾选的菜单树，而该字段来自
// sys_role_menu 关联表，model.SysRole 上并不存在。此前列表与详情都不返回它，
// 前端 `row.menuIds || []` 恒为 [] —— 用户打开对话框看到的是一片空白，
// 点「确定」就会提交空数组，把该角色**已有的全部授权静默清空**。
// 这不是显示缺陷，而是真实的数据丢失。
//
// 只在详情接口填充 menuIds，不在列表里填：列表一页 10 条，逐条查关联表
// 就是 10 次额外查询，而列表渲染本身并不需要这个字段。
type RoleDetailVO struct {
	model.SysRole
	MenuIds []uint `json:"menuIds"`
}
