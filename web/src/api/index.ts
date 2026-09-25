import axios, { type AxiosInstance, type AxiosResponse, type AxiosRequestConfig } from 'axios'
import { ElMessage } from 'element-plus'
import { getToken, getRefreshToken, removeToken, setToken, setRefreshToken } from '@/utils/auth'
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
  (error) => Promise.reject(error),
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
            return refreshAndReplay(
              response.config as RetryableConfig,
              new Error(res.message || '导出失败'),
            )
          }
          ElMessage.error(res.message || '导出失败')
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
        // 后端部分路径以「HTTP 200 + body code 401」返回（见 common.Unauthorized 的用法），
        // 所以这条分支同样要走续期 —— 只在 axios 的 error 分支处理会漏掉它们
        return refreshAndReplay(
          response.config as RetryableConfig,
          new BizError(res.code, res.message || '登录已过期', res.data),
        )
      }
      ElMessage.error(res.message || '请求失败')
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
      return refreshAndReplay(error.config as RetryableConfig | undefined, error)
    }
    ElMessage.error(error.message || '网络错误')
    return Promise.reject(error)
  },
)

/**
 * 扩展的请求配置：自动续期用的两个内部标记。
 *
 * 用挂在 config 上的自定义字段而不是外部 Map 记录状态 —— axios 会把
 * `response.config` / `error.config` 原样交回来，重放时又把它合并进新请求，
 * 所以标记天然跟着请求走，不需要额外的登记表（也就不会泄漏）。
 */
interface RetryableConfig extends AxiosRequestConfig {
  /** 该请求已经历过一次「续期后重放」，再 401 就直接清会话（防无限循环） */
  authRetried?: boolean
  /** 该请求本身就是续期请求，不参与续期（防无限递归） */
  skipAuthRefresh?: boolean
}

/**
 * 不参与自动续期的路径。
 *
 * `/auth/refresh` 自身失败意味着 refresh token 也废了，再续期没有意义；
 * `/auth/login` 失败是凭据错，此时去续期只会拿一个陈旧的 refresh token
 * 多发一次注定失败的请求。
 */
const NO_REFRESH_PATHS = ['/auth/login', '/auth/refresh']

/**
 * 单飞（single-flight）：同一时刻只允许存在一次续期请求。
 *
 * 为什么必须有：一个页面同时发出 3 个请求、而 access token 恰好过期时，
 * 3 个请求会同时收到 401。若各自去续期，就会有 3 次刷新请求并发打到后端 ——
 * 而后端刷新是**轮换式**的（`authService.RefreshToken` 会先把旧 refresh token
 * 从 Redis 删掉、再发一个新的），于是第 2、3 次刷新拿的是**已被删除**的旧 token，
 * 后端返回「refresh token已过期」→ 401 → 前端清会话。结果是
 * **用户刚过 2 小时就被踢出登录**，比不做自动续期还糟。
 *
 * 后到的调用复用同一个在途 Promise，续期完成后各自重放自己的请求。
 */
let refreshInFlight: Promise<void> | null = null

/** 真正执行一次续期（不含单飞包装） */
async function doRefreshSession(): Promise<void> {
  const refreshToken = getRefreshToken()
  if (!refreshToken) {
    // 没有 refresh token 就没有续期手段。抛错让调用方走清会话分支，
    // 而不是发一个注定 401 的请求再绕一圈回来
    throw new Error('没有可用的 refresh token')
  }

  const res = await service.post<unknown, Result<{ accessToken: string; refreshToken: string }>>(
    '/auth/refresh',
    { refreshToken },
    // 打上标记：这次请求自己的 401 不再触发续期
    { skipAuthRefresh: true } as RetryableConfig,
  )

  // 后端刷新时**同时轮换**两个 token，必须都更新 ——
  // 只更新 access token 的话，下一次续期用的还是已被消费掉的旧 refresh token
  setToken(res.data.accessToken)
  setRefreshToken(res.data.refreshToken)
}

/** 续期（带单飞）。失败时抛出原因，由调用方决定是否清会话 */
function refreshSession(): Promise<void> {
  if (!refreshInFlight) {
    refreshInFlight = doRefreshSession().finally(() => {
      // 无论成败都要释放：漏了这一步，一次失败会让后续所有续期
      // 立刻拿到同一个 rejection，用户永远续不上
      refreshInFlight = null
    })
  }
  return refreshInFlight
}

/**
 * 判断这次 401 是否应该尝试自动续期。
 *
 * 四种情况必须直接放弃（走清会话）：无法重放的请求、续期请求自身、
 * 已经重放过一次的请求、以及本来就不该续期的路径。
 */
function shouldTryRefresh(config?: RetryableConfig): boolean {
  if (!config) return false
  if (config.skipAuthRefresh) return false
  if (config.authRetried) return false
  const url = config.url || ''
  return !NO_REFRESH_PATHS.some((path) => url.startsWith(path))
}

/**
 * 收到 401 后的统一处理：先续期、再重放原请求；续期不可行或失败则清会话。
 *
 * 抛出的始终是**原始错误**，调用方据此保持既有语义
 * （业务错误仍是 BizError，网络错误仍是 axios error）。
 */
async function refreshAndReplay(
  config: RetryableConfig | undefined,
  originalError: unknown,
): Promise<unknown> {
  if (!shouldTryRefresh(config)) {
    handleLogout()
    throw originalError
  }

  // 先打标记再续期：万一重放回来又是 401，不会再次触发续期
  config!.authRetried = true

  try {
    await refreshSession()
  } catch (err) {
    console.warn('[request] 自动续期失败，已清理本地会话', err)
    handleLogout()
    throw originalError
  }

  // 重放：request 拦截器会重新读 cookie，因此带的是刚换到的新 token
  return service.request(config!)
}

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
  get: <T>(url: string, config?: AxiosRequestConfig) => service.get<unknown, T>(url, config),
  post: <T>(url: string, data?: unknown, config?: AxiosRequestConfig) =>
    service.post<unknown, T>(url, data, config),
  put: <T>(url: string, data?: unknown, config?: AxiosRequestConfig) =>
    service.put<unknown, T>(url, data, config),
  delete: <T>(url: string, config?: AxiosRequestConfig) => service.delete<unknown, T>(url, config),
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
