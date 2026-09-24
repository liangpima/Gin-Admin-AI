import { http } from './index'
import type { Result, PageResult } from './index'

export interface FileItem {
  id: number
  name: string
  storageName: string
  path: string
  url: string
  size: number
  mimeType: string
  storageType: number
  createBy: number
  createdAt: string
}

export function uploadFile(file: File) {
  const formData = new FormData()
  formData.append('file', file)
  return http.post<Result<{ id: number; name: string; url: string; size: number }>>(
    '/system/file/upload',
    formData,
  )
}

export function getFileList(params: {
  name?: string
  mimeType?: string
  sortOrder?: string
  page: number
  pageSize: number
}) {
  return http.get<Result<PageResult<FileItem>>>('/system/file/list', { params })
}

export function deleteFile(id: number) {
  return http.delete<Result>(`/system/file/${id}`)
}
