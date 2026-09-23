import { http } from './index'
import type { Result } from './index'

export interface DeptItem {
  id: number
  parentId: number
  name: string
  sort: number
  leader: string
  phone: string
  email: string
  status: number
  children?: DeptItem[]
}

export function getDeptTree() {
  return http.get<Result<DeptItem[]>>('/system/dept/tree')
}

export function createDept(data: Partial<DeptItem>) {
  return http.post<Result>('/system/dept', data)
}

export function updateDept(data: Partial<DeptItem>) {
  return http.put<Result>('/system/dept', data)
}

export function deleteDept(id: number) {
  return http.delete<Result>(`/system/dept/${id}`)
}
