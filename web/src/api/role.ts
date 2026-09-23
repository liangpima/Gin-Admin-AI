import { http } from './index'
import type { Result, PageResult } from './index'

export interface RoleItem {
  id: number
  name: string
  code: string
  sort: number
  status: number
  dataScope: number
  createdAt: string
  // 角色已授权的菜单/按钮 ID，由后端在列表里带出，用于回显授权树
  menuIds?: number[]
}

// RoleFormData 创建/更新角色的请求体。
//
// 字段全部可选是有意的：更新接口支持**部分更新**，
// 「保存权限」只提交 { id, menuIds }，不带 name/code/sort 等。
// 后端会按「未提供即不改」处理，因此这里不能把 name/code 声明为必填 ——
// 那会逼着调用方凭空编造字段值，反而掩盖真实请求。
export interface RoleFormData {
  id?: number
  name?: string
  code?: string
  sort?: number
  status?: number
  dataScope?: number
  menuIds?: number[]
  remark?: string
}

export interface RoleQuery {
  name?: string
  code?: string
  status?: number
  page: number
  pageSize: number
}

export function getRoleList(params: RoleQuery) {
  return http.get<Result<PageResult<RoleItem>>>('/system/role/list', { params })
}

export function getAllRoles() {
  return http.get<Result<RoleItem[]>>('/system/role/all')
}

export function getRoleById(id: number) {
  return http.get<Result<RoleItem>>(`/system/role/${id}`)
}

export function createRole(data: RoleFormData) {
  return http.post<Result>('/system/role', data)
}

export function updateRole(data: RoleFormData) {
  return http.put<Result>('/system/role', data)
}

export function deleteRole(id: number) {
  return http.delete<Result>(`/system/role/${id}`)
}
