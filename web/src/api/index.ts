import axios, { type AxiosInstance, type AxiosResponse, type AxiosRequestConfig } from 'axios'
import { ElMessage } from 'element-plus'
import { getToken, removeToken } from '@/utils/auth'
import router from '@/router'

let isRedirecting = false

/**
 * 401 处理：清会话并跳登录页。
 *
 * 用动态 import 引入 user store：user store 依赖 api/auth（进而依赖本模块），
 * 静态引入会形成循环依赖。
 *
 * 必须调用 store 的 clearSession 而不是只 reset permission store ——
 * 后者不会 router.removeRoute，上一个高权限会话动态注册的路由会残留在
 * router 里，换个低权限账号登录后直接改 URL 就能打开（详见 clearSession 注释）。
 */
function handleLogout() {
  if (isRedirecting) return
  isRedirecting = true
  // 先立刻清掉本地 token，保证后续请求不会带上已失效的凭据
  removeToken()

  void import('@/store/modules/user').then(({ useUserStore }) => {
    useUserStore().clearSession()
  })

  router.push('/login').finally(() => {
    isRedirecting = false
  })
}

const service: AxiosInstance = axios.create({
  baseURL: '/api/v1',
  timeout: 30000,
})

service.interceptors.request.use(
  (config) => {
    const token = getToken()
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  (error) => Promise.reject(error)
)

service.interceptors.response.use(
  async (response: AxiosResponse) => {
    // 二进制下载（如导出 Excel）：正常时是文件流，直接交给调用方；
    // 但后端出错时（业务错误仍走 HTTP 200）会返回 JSON，必须识别出来，
    // 否则 Blob 会被当作正常响应返回，用户拿到一个内容为错误信息的「Excel」。
    if (response.config.responseType === 'blob') {
      const blob = response.data as Blob
      if (blob?.type?.includes('application/json')) {
        const text = await blob.text()
        try {
          const res = JSON.parse(text)
          if (res.code === 401) {
            handleLogout()
          } else {
            ElMessage.error(res.message || '导出失败')
          }
          return Promise.reject(new Error(res.message || '导出失败'))
        } catch {
          // 不是合法 JSON，按文件流处理
        }
      }
      return response
    }

    const res = response.data
    if (res.code !== 0) {
      if (res.code === 401) {
        handleLogout()
      } else {
        ElMessage.error(res.message || '请求失败')
      }
      // 用 BizError 而不是 new Error(res.message)：
      // 后者会把后端业务码整个丢掉，调用方只能靠比对文案做分支，
      // 文案一改就静默失效（例如「余额不足」需要引导去充值这类逻辑）。
      // BizError 继承自 Error，既有的 catch (e) { e.message } 写法治不受影响。
      return Promise.reject(new BizError(res.code, res.message || '请求失败', res.data))
    }
    return res
  },
  (error) => {
    if (error.response?.status === 401) {
      handleLogout()
    } else {
      ElMessage.error(error.message || '网络错误')
    }
    return Promise.reject(error)
  }
)

/**
 * http：对 service 的类型化门面。
 *
 * 为什么需要它：axios 的 get/post/put/delete 是**双泛型** `get<T, R>` ——
 * T 是响应体类型、R 是返回值类型。本项目在拦截器里已经把 response 解包成
 * 业务对象，所以调用方只关心「返回什么」，写 `request.get<any, Result<X>>`
 * 里的那个 any 纯粹是为了让 axios 的签名通过，属于噪音；更糟的是它掩盖了
 * 真正的问题 —— 有些调用干脆不写泛型，返回值就静默退化成 any。
 *
 * 门面把它收敛成单个泛型：`http.get<Result<X>>(url)`，一眼能看出返回什么。
 */
export const http = {
  get: <T>(url: string, config?: AxiosRequestConfig) =>
    service.get<unknown, T>(url, config),
  post: <T>(url: string, data?: unknown, config?: AxiosRequestConfig) =>
    service.post<unknown, T>(url, data, config),
  put: <T>(url: string, data?: unknown, config?: AxiosRequestConfig) =>
    service.put<unknown, T>(url, data, config),
  delete: <T>(url: string, config?: AxiosRequestConfig) =>
    service.delete<unknown, T>(url, config),
}

export default service

/**
 * 统一响应体。默认泛型用 unknown 而不是 any：
 * any 会让「忘了写泛型」的调用点静默获得任意属性访问能力，
 * 而 unknown 会在 `.data.xxx` 处直接报错，逼调用方补上真实类型。
 * 仓库里 `http.post<Result>(...)` 这类不关心 data 的写法的照常可用。
 */
export interface Result<T = unknown> {
  code: number
  message: string
  data: T
}

export interface PageResult<T = unknown> {
  list: T[]
  total: number
  page: number
  pageSize: number
}

/**
 * 后端业务错误，保留业务码。
 *
 * 拦截器原先统一 `reject(new Error(res.message))`，只留下文本消息：
 * 调用方无法区分「参数错误 400」「无权限 403」「资源不存在 404」，
 * 想做差异化处理就只能比对文案 —— 而后端改一个字就静默失效。
 *
 * 继承自 Error，因此既有的 `catch (e) { ElMessage.error(e.message) }`
 * 写法完全不受影响，需要按码分支的地方再用 isBizError 判断即可。
 */
export class BizError extends Error {
  readonly code: number
  readonly data?: unknown

  constructor(code: number, message: string, data?: unknown) {
    super(message)
    this.name = 'BizError'
    this.code = code
    this.data = data
  }
}

/** 判断捕获到的异常是否为后端返回的业务错误 */
export function isBizError(err: unknown): err is BizError {
  return err instanceof BizError
}
