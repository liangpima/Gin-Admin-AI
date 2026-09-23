import { http } from './index'
import type { Result, PageResult } from './index'

export interface DictTypeItem {
  id: number
  name: string
  type: string
  status: number
  remark?: string
}

export interface DictDataItem {
  id: number
  label: string
  value: string
  sort: number
  status: number
  dictType: string
  cssClass?: string
  listClass?: string
  remark?: string
}

export interface DictQuery {
  page: number
  pageSize: number
}

export interface CreateDictTypePayload {
  name: string
  type: string
}

export interface UpdateDictTypePayload {
  name: string
  status?: number
  remark?: string
}

export interface CreateDictDataPayload {
  dictType: string
  label: string
  value: string
  sort?: number
  cssClass?: string
  listClass?: string
  remark?: string
}

export interface UpdateDictDataPayload {
  label: string
  value: string
  sort?: number
  cssClass?: string
  listClass?: string
  status?: number
  remark?: string
}

export function getDictTypeList(params: DictQuery & { name?: string }) {
  return http.get<Result<PageResult<DictTypeItem>>>('/system/dict/type/list', { params })
}

export function createDictType(data: CreateDictTypePayload) {
  return http.post<Result>('/system/dict/type', data)
}

export function updateDictType(id: number, data: UpdateDictTypePayload) {
  return http.put<Result>(`/system/dict/type/${id}`, data)
}

export function deleteDictType(id: number) {
  return http.delete<Result>(`/system/dict/type/${id}`)
}

export function getDictDataList(params: DictQuery & { dictType?: string }) {
  return http.get<Result<PageResult<DictDataItem>>>('/system/dict/data/list', { params })
}

/**
 * 按类型取「启用中」的字典选项，供业务页面渲染下拉框与标签。
 * 只要求登录态（不需要 system:dict:list），因此普通操作员也能正常拿到选项。
 */
export function getDictDataByType(type: string) {
  return http.get<Result<DictDataItem[]>>(`/system/dict/data/type/${type}`)
}

export function createDictData(data: CreateDictDataPayload) {
  return http.post<Result>('/system/dict/data', data)
}

export function updateDictData(id: number, data: UpdateDictDataPayload) {
  return http.put<Result>(`/system/dict/data/${id}`, data)
}

export function deleteDictData(id: number) {
  return http.delete<Result>(`/system/dict/data/${id}`)
}
