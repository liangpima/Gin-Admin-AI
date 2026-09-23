import { http } from './index'
import type { Result, PageResult } from './index'

export interface MemberItem {
  id: number
  memberNo: string
  username: string
  nickname: string
  avatar: string
  phone: string
  gender: number
  birthday: string
  levelId: number
  status: number
  points: number
  wechatOpenid: string
  registerTime: string
  lastVisitTime: string
  tags: { id: number; name: string; color: string }[]
  createdAt: string
  remark?: string
}

export interface MemberLevelItem {
  id: number
  name: string
  minPoints: number
  discount: number
  icon: string
  sort: number
  status: number
}

export interface MemberTagItem {
  id: number
  name: string
  color: string
  sort: number
  status: number
}

export interface PointsLogItem {
  id: number
  memberId: number
  change: number
  type: number
  source: string
  orderNo: string
  createdAt: string
}

export function getMemberList(params: { phone?: string; nickname?: string; levelId?: number; status?: number; page: number; pageSize: number }) {
  return http.get<Result<PageResult<MemberItem>>>('/member/list', { params })
}

// 以下三个 *FormData 是创建/更新的请求体。
//
// 与 *Item 分开而不是用 Partial<X>：请求体不含 id/createdAt/points/tags
// 这类服务端生成或只读字段，却包含只在写入时出现的 tagIds 等。
// 混用会让「提交时漏传必填项」在编译期查不出来。
export interface MemberFormData {
  id?: number
  username?: string
  nickname?: string
  avatar?: string
  phone: string
  gender?: number
  birthday?: string
  levelId?: number
  tagIds?: number[]
  status?: number
  remark?: string
  wechatOpenid?: string
}

// MemberUpdateData 更新会员的请求体。
//
// 字段全部可选：更新接口支持**部分更新**，前端的「修改等级」「修改标签」
// 只提交 { id, levelId } / { id, tagIds }。后端按「未提供即不改」处理，
// 所以这里不能把 phone 之类声明为必填 —— 那会逼调用方编造字段值，
// 反而把真实请求掩盖掉。
export interface MemberUpdateData {
  id: number
  username?: string
  nickname?: string
  avatar?: string
  phone?: string
  gender?: number
  birthday?: string
  levelId?: number
  tagIds?: number[]
  status?: number
  remark?: string
}

export interface MemberLevelFormData {
  id?: number
  name: string
  minPoints?: number
  discount?: number
  icon?: string
  sort?: number
  status?: number
}

export interface MemberTagFormData {
  id?: number
  name: string
  color?: string
  sort?: number
  status?: number
}

export function createMember(data: MemberFormData) {
  return http.post<Result>('/member', data)
}

export function updateMember(data: MemberUpdateData) {
  return http.put<Result>('/member', data)
}

export function deleteMember(id: number) {
  return http.delete<Result>(`/member/${id}`)
}

export function updateMemberStatus(data: { id: number; status: number }) {
  return http.put<Result>('/member/status', data)
}

export function updateMemberTags(data: { id: number; tagIds: number[] }) {
  return http.put<Result>('/member/tags', data)
}

export function getMemberLevelList(params: { name?: string; page: number; pageSize: number }) {
  return http.get<Result<PageResult<MemberLevelItem>>>('/member/level/list', { params })
}

export function getAllMemberLevels() {
  return http.get<Result<MemberLevelItem[]>>('/member/level/all')
}

export function createMemberLevel(data: MemberLevelFormData) {
  return http.post<Result>('/member/level', data)
}

export function updateMemberLevel(data: MemberLevelFormData) {
  return http.put<Result>('/member/level', data)
}

export function deleteMemberLevel(id: number) {
  return http.delete<Result>(`/member/level/${id}`)
}

export function getMemberTagList(params: { name?: string; page: number; pageSize: number }) {
  return http.get<Result<PageResult<MemberTagItem>>>('/member/tag/list', { params })
}

export function getAllMemberTags() {
  return http.get<Result<MemberTagItem[]>>('/member/tag/all')
}

export function createMemberTag(data: MemberTagFormData) {
  return http.post<Result>('/member/tag', data)
}

export function updateMemberTag(data: MemberTagFormData) {
  return http.put<Result>('/member/tag', data)
}

export function deleteMemberTag(id: number) {
  return http.delete<Result>(`/member/tag/${id}`)
}

export function getPointsLogList(params: { memberId?: number; type?: number; page: number; pageSize: number }) {
  return http.get<Result<PageResult<PointsLogItem>>>('/member/points/list', { params })
}
