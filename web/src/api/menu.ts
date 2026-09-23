import { http } from './index'
import type { Result } from './index'

export interface MenuItem {
  id: number
  parentId: number
  name: string
  path: string
  component: string
  redirect: string
  icon: string
  title: string
  type: number
  permission: string
  sort: number
  visible: number
  status: number
  isExternal: number
  isCache: number
  children?: MenuItem[]
}

export function getMenuTree() {
  return http.get<Result<MenuItem[]>>('/system/menu/tree')
}

export function getAllMenus() {
  return http.get<Result<MenuItem[]>>('/system/menu/all')
}

export function createMenu(data: Partial<MenuItem>) {
  return http.post<Result>('/system/menu', data)
}

export function updateMenu(data: Partial<MenuItem>) {
  return http.put<Result>('/system/menu', data)
}

export function deleteMenu(id: number) {
  return http.delete<Result>(`/system/menu/${id}`)
}
