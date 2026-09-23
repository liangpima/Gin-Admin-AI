import { http } from './index'
import type { Result } from './index'

export interface CaptchaGenerateResponse {
  token: string
  bg: string
  bgWidth: number
  bgHeight: number
  chars: string
}

export interface CaptchaPoint {
  x: number
  y: number
}

export interface CaptchaVerifyResponse {
  success: boolean
  token: string
  message: string
}

export function getCaptcha() {
  return http.get<Result<CaptchaGenerateResponse>>('/captcha/generate')
}

export function verifyCaptcha(data: { token: string; points: CaptchaPoint[] }) {
  return http.post<Result<CaptchaVerifyResponse>>('/captcha/verify', data)
}
