import axios, { type AxiosInstance, type AxiosResponse, type AxiosRequestConfig } from 'axios'
import { ElMessage } from 'element-plus'
import { clearLoginFlag, CSRFHeaderName, getCsrfToken } from '@/utils/auth'
import router from '@/router'

/**
 * 需要携带 CSRF 令牌的方法。
 *
 * 与后端 `isSafeMethod` 的白名单互补：那边列的是**免校验**的安全方法
 * （GET/HEAD/OPTIONS），这里列的是需要校验的。两处必须同步改 ——
 * 后端新增一个要校验的方法而这里漏了，现象是「该方法的请求全部 403」。
 */
const CSRF_METHODS = ['post', 'put', 'patch', 'delete']

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
 *
 * 这里清的是**登录态标记**（`logged_in`），不是 token（P3-B2）：
 * token 是 HttpOnly cookie，前端删不掉也不需要删 —— 被 401 踢出时它已经无效了。
 * 但标记必须清，否则刷新页面会被它骗回「已登录」分支，白拉一次 userInfo 再被踢一次。
 */
function handleLogout() {
  if (isRedirecting) return
  isRedirecting = true
  // 先立刻清掉本地登录态标记，保证路由守卫不会再把用户放行到业务页面
  clearLoginFlag()

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
  // 凭据现在靠 HttpOnly cookie 传递（P3-B2）。同源部署下（dev 走 Vite proxy、
  // prod 走 nginx /api）浏览器本来就会带上 cookie，这一行不改变行为；
  // 显式写出来是为了两件事：① 表明「跨域部署时也必须带凭据」这个意图；
  // ② 避免将来有人改成分域名部署时，因为缺这一行而让 cookie 静默不发 ——
  // 那种故障的表现是「登录成功但下一个请求就是未登录」，很难联想到配置。
  withCredentials: true,
})

service.interceptors.request.use(
  (config) => {
    // 这里原本会从 cookie 读 token 塞进 Authorization 头（P3-B2 删除）。
    // 现在 token 是 HttpOnly，JS 读不到，凭据由浏览器自动携带 —— 不需要注入任何东西。
    //
    // 注意这**不是**「回滚开关」：后端 security.token_transport 设成 header 时
    // 不再下发 cookie，而前端已经不再注入头，两边都拿不到凭据。
    // B2 之后回滚的粒度是「前后端一起回退到 B2 之前的提交」，不是改一个配置项。
    //
    // CSRF 令牌（P3-B3）：非 GET 请求带上 X-CSRF-Token，与服务端的
    // double-submit 校验配对（见 internal/middleware/csrf.go）。
    // 它**不是**凭据，只是「请求由本域脚本发出」的信物 —— 与 Authorization 头
    // 是两回事，别把两者混为一谈。
    //
    // 只对非安全方法附加：GET/HEAD/OPTIONS 在服务端本就免校验，
    // 而自定义头会触发 CORS 预检 —— 给每个 GET 都加一个头，等于把
    // 跨域部署下所有读请求都变成「先发一次 OPTIONS」，纯属性能损失。
    const method = (config.method || 'get').toLowerCase()
    if (CSRF_METHODS.includes(method)) {
      const csrf = getCsrfToken()
      if (csrf) {
        config.headers[CSRFHeaderName] = csrf
      }
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
  // 凭据由 HttpOnly cookie 携带（Path=/api/v1/auth，覆盖 /auth/refresh），
  // 前端读不到也不需要读，所以这里**不传任何凭据**。
  //
  // 特别注意不要试图「从 cookie 里读出来再放进 body」：读不到（HttpOnly），
  // 而且服务端的取值是 body 优先（authcookie.RefreshFromRequest），
  // 一旦传了空串，反而会把 cookie 里那份有效凭据顶掉 ——
  // 现象是「每次续期都失败」，而请求看起来明明发了。
  //
  // 新 token 同样由服务端 Set-Cookie 下发，前端无需（也无法）保存。
  //
  // 与 B2 之前相比这里少了一个「本地没有 refresh token 就直接抛错」的短路：
  // 前端已经无从判断有没有凭据了。代价是未登录状态下遇到 401 会多打一次
  // 注定失败的 /auth/refresh —— 但守卫已经拦住了未登录时的业务页面访问，
  // 实际很难走到；而漏判的代价（明明有凭据却不续期）要大得多。
  await service.post<unknown, Result<{ accessToken: string; refreshToken: string }>>(
    '/auth/refresh',
    {},
    // 打上标记：这次请求自己的 401 不再触发续期
    { skipAuthRefresh: true } as RetryableConfig,
  )
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

  // 重放：浏览器会自动带上刚由 Set-Cookie 换新的 access token，
  // request 拦截器不需要做任何事（它已经不再注入 Authorization 头）
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
