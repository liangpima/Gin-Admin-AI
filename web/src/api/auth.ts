import type { MenuItem } from '@/api/menu'
import { http } from './index'
import type { Result } from './index'

export interface LoginParams {
  username: string
  password: string
  /** 验证码校验通过后返回的 token，后端据此确认本次登录已过人机校验 */
  captchaToken: string
}

export interface LoginResult {
  accessToken: string
  refreshToken: string
  expiresIn: number
  tokenType: string
}

export interface UserInfoResult {
  id: number
  username: string
  nickname: string
  avatar: string
  email: string
  phone: string
  roles: { id: number; name: string; code: string }[]
  buttons: string[]
  menus: MenuItem[]
}

export function login(data: LoginParams) {
  return http.post<Result<LoginResult>>('/auth/login', data)
}

// 这里原本还有一个 refreshToken(data) 封装，P3-B2 时删掉了。
// 原因：自动续期由 api/index.ts 的响应拦截器直接打 /auth/refresh，
// 凭据来自 HttpOnly cookie，调用方**不需要也无法**传 token 进来。
// 留着它只会重演 B4 修过的那类缺陷 —— 定义了但没人调用的接口，
// 会让「token 一过期就被踢出去」看起来像是没有续期机制。

export function logout() {
  // 刻意不传 refreshToken：它由 HttpOnly cookie 携带
  // （Path=/api/v1/auth，覆盖本接口），服务端用 authcookie.RefreshFromRequest 自己取。
  // 传空串会让服务端认为「本次登出无需吊销」，把 refresh token 留在 Redis 里
  // 直到自然过期 —— 等于没登出。
  //
  // 仍然显式发一个空对象 body：后端容忍完全空的请求体（io.EOF），
  // 但发 {} 可以少走一条边界分支。
  return http.post<Result>('/auth/logout', {})
}

export function getUserInfo() {
  return http.get<Result<UserInfoResult>>('/auth/userInfo')
}
