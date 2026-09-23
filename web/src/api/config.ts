import request, { http } from './index'
import type { Result } from './index'

export interface ConfigItem {
  id: number
  name: string
  key: string
  value: string
  type: number
  createdAt: string
}

export interface ConfigQuery {
  page: number
  pageSize: number
}

export function getConfigList(params: ConfigQuery) {
  return request.get('/system/config/list', { params })
}

export function createConfig(data: Partial<ConfigItem>) {
  return request.post('/system/config', data)
}

export function updateConfig(data: Partial<ConfigItem>) {
  return request.put('/system/config', data)
}

export function deleteConfig(id: number) {
  return request.delete(`/system/config/${id}`)
}

export function getConfigByPrefix(prefix: string) {
  return http.get<Result<ConfigItem[]>>('/system/config/prefix', { params: { prefix } })
}

export function batchSaveConfig(prefix: string, items: { key: string; value: string }[]) {
  return http.put<Result>('/system/config/batch', { prefix, items })
}

export function getSiteInfo() {
  return http.get<Result<Record<string, string>>>('/site/info')
}

export function uploadCert(file: File) {
  const formData = new FormData()
  formData.append('file', file)
  return http.post<Result<{ path: string; filename: string }>>('/system/config/upload', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
}
