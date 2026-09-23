import { http } from './index'
import type { Result, PageResult } from './index'

// UserFormData 创建/更新用户的请求体。
//
// 与 UserItem 分开而不是用 Partial<UserItem>：
// 请求体不含 id/createdAt/roles 这类服务端生成或只读字段，
// 却包含只在写入时出现的 password。混用会让「提交时漏传必填项」
// 这类错误在编译期查不出来。
export interface UserFormData {
  id?: number
  username: string
  password?: string
  nickname?: string
  avatar?: string
  phone?: string
  email?: string
  deptId?: number
  roleIds?: number[]
  status: number
  remark?: string
}

export interface UserItem {
  id: number
  username: string
  nickname: string
  email: string
  phone: string
  avatar: string
  status: number
  deptId: number
  roles: { id: number; name: string; code: string }[]
  createdAt: string
  remark?: string
}

export interface UserListParams {
  username?: string
  phone?: string
  status?: number
  deptId?: number
  page: number
  pageSize: number
}

export function getUserList(params: UserListParams) {
  return http.get<Result<PageResult<UserItem>>>('/system/user/list', { params })
}

export function getUserById(id: number) {
  return http.get<Result<UserItem>>(`/system/user/${id}`)
}

export function createUser(data: UserFormData) {
  return http.post<Result>('/system/user', data)
}

export function updateUser(data: UserFormData) {
  return http.put<Result>('/system/user', data)
}

export function deleteUser(id: number) {
  return http.delete<Result>(`/system/user/${id}`)
}

export function updateUserStatus(data: { id: number; status: number }) {
  return http.put<Result>('/system/user/status', data)
}

export function updateUserRoles(data: { id: number; roleIds: number[] }) {
  return http.put<Result>('/system/user/roles', data)
}

export function updateUserDept(data: { id: number; deptId: number }) {
  return http.put<Result>('/system/user/dept', data)
}

export function resetPassword(data: { id: number; password: string }) {
  return http.put<Result>('/system/user/resetPwd', data)
}

export function changePassword(data: { oldPassword: string; newPassword: string }) {
  return http.put<Result>('/system/user/changePwd', data)
}

/**
 * 导出用户列表为 Excel。
 *
 * 返回二进制流，不走统一的 JSON 解包（见 api/index.ts 对 blob 的单独处理），
 * 调用方需要自行创建 Blob URL 触发下载。
 * 筛选条件与列表一致，但不传分页参数 —— 导出是整份数据（后端有行数上限保护）。
 */
export function exportUsers(params: Omit<UserListParams, 'page' | 'pageSize'>) {
  return http.get<Blob>('/system/user/export', {
    params,
    responseType: 'blob',
  })
}
