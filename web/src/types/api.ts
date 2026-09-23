// Common API types
export interface Result<T = any> {
  code: number
  message: string
  data: T
}

export interface PageResult<T = any> {
  list: T[]
  total: number
  page: number
  pageSize: number
}

export interface PageParams {
  page: number
  pageSize: number
}

// User
export interface UserInfoResult {
  id: number
  username: string
  nickname: string
  avatar: string
  phone: string
  email: string
  roles: { id: number; name: string; code: string }[]
  buttons: string[]
  menus: MenuItem[]
}

export interface LoginResult {
  accessToken: string
  refreshToken: string
}

export interface UserItem {
  id: number
  username: string
  nickname: string
  phone: string
  email: string
  deptId: number
  roleIds: number[]
  status: number
  remark: string
  createdAt: string
}

// Role
export interface RoleItem {
  id: number
  name: string
  code: string
  sort: number
  status: number
  menuIds: number[]
  remark: string
  createdAt: string
}

// Menu
export interface MenuItem {
  id: number
  parentId: number
  title: string
  name: string
  path: string
  component: string
  icon: string
  type: number
  permission: string
  sort: number
  visible: number
  status: number
  isCache: number
  children?: MenuItem[]
}

// Department
export interface DeptItem {
  id: number
  parentId: number
  name: string
  leader: string
  phone: string
  email: string
  sort: number
  status: number
  children?: DeptItem[]
}

// Post
export interface PostItem {
  id: number
  code: string
  name: string
  sort: number
  status: number
  createdAt: string
}

// Config
export interface ConfigItem {
  id: number
  name: string
  key: string
  value: string
  type: number
  createdAt: string
}

// Dictionary
export interface DictTypeItem {
  id: number
  name: string
  type: string
  status: number
}

export interface DictDataItem {
  id: number
  label: string
  value: string
  sort: number
  status: number
  dictType: string
}

// Logs
export interface OperationLogItem {
  id: number
  title: string
  operatorName: string
  requestMethod: string
  requestUrl: string
  status: number
  ip: string
  costTime: number
  createdAt: string
}

export interface LoginLogItem {
  id: number
  username: string
  ip: string
  browser: string
  os: string
  status: number
  msg: string
  loginTime: string
}

// Dashboard
export interface DashboardStats {
  userCount: number
  roleCount: number
  menuCount: number
  deptCount: number
  postCount: number
  configCount: number
  logCount: number
}
