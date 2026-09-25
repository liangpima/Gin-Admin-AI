import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * 自动续期的单元测试（P3-B4）。
 *
 * 背景：`refreshToken()` 这个 API 早就定义在 `api/auth.ts` 里，但**从未被调用** ——
 * 后果是 access token 一过期（`jwt.access_expire: 7200`，2 小时）用户就被直接
 * 踢到登录页，而手里明明握着 7 天有效的 refresh token。
 * 这里补的是「401 之后先续期、再重放原请求」这条链路。
 *
 * 三个必须守住的行为，每个都对应一种真实故障：
 *   1. 并发 401 只能发**一次**续期请求 —— 否则 refresh token 被并发使用，
 *      可能被服务端判定为异常而吊销，用户反而被踢出去（比不续期更糟）
 *   2. 续期请求自身收到 401 时**不能**再触发续期 —— 否则无限递归
 *   3. 重放过的请求再 401 也不能再续期 —— 否则同样无限循环
 *
 * 用 vi.mock 替换 axios / element-plus / 路由 / token 工具，让被测模块能在
 * 不启动浏览器的情况下加载，并直接调用注册进来的处理函数。
 */

/**
 * 拦截器处理函数收到的参数形状。
 * 只声明测试真正会读到的字段 —— 用 `any` 会让「写错了字段名」也静默通过，
 * 而 `no-explicit-any` 在 CI 里是硬门槛。
 */
interface InterceptArg {
  data?: unknown
  config?: Record<string, unknown>
  response?: { status: number }
  message?: string
  /** 请求拦截器会收到的字段 */
  url?: string
  headers?: Record<string, string>
}

type AnyHandler = (arg: InterceptArg) => unknown

const handlers: {
  response?: AnyHandler
  responseError?: AnyHandler
  request?: AnyHandler
} = {}

/** 重放原请求时走的是 service.request */
const serviceRequest = vi.fn()
/** 续期时走的是 service.post('/auth/refresh', ...) */
const servicePost = vi.fn()

vi.mock('axios', () => {
  const instance = {
    interceptors: {
      request: {
        use: (ok: AnyHandler) => {
          handlers.request = ok
        },
      },
      response: {
        use: (ok: AnyHandler, err: AnyHandler) => {
          handlers.response = ok
          handlers.responseError = err
        },
      },
    },
    request: (...args: unknown[]) => serviceRequest(...args),
    post: (...args: unknown[]) => servicePost(...args),
    get: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  }
  return { default: { create: () => instance } }
})

const ElMessage = { error: vi.fn(), success: vi.fn(), warning: vi.fn() }
vi.mock('element-plus', () => ({ ElMessage }))

const clearLoginFlag = vi.fn()
// 只 mock 真实模块**实际导出**的东西（P3-B2 之后 @/utils/auth 只剩登录态标记
// 与 CSRF 令牌的读写）。
// 若实现回退去调 getToken / setToken，这里会得到 undefined 并直接抛错 ——
// 这比断言「调用了新接口」更能防回退：后者不阻止旧接口同时被调用。
vi.mock('@/utils/auth', () => ({
  clearLoginFlag: () => clearLoginFlag(),
  CSRFHeaderName: 'X-CSRF-Token',
  // 本文件关注续期链路，不关心 CSRF；返回 undefined 让请求拦截器不加头
  getCsrfToken: () => undefined,
}))

const routerPush = vi.fn(() => Promise.resolve())
vi.mock('@/router', () => ({ default: { push: routerPush } }))

const { BizError } = await import('@/api/index')

/** 续期接口成功时的响应体（已被成功拦截器解包过的形状） */
function refreshOk(access = 'new-access', refresh = 'new-refresh') {
  return { code: 0, message: 'success', data: { accessToken: access, refreshToken: refresh } }
}

/** 一个可手动控制完成时机的 Promise，用于构造「并发」场景 */
function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

/** 断言「会话被清理」：清登录态标记 + 跳登录页 */
async function expectSessionCleared() {
  expect(clearLoginFlag).toHaveBeenCalled()
  await vi.waitFor(() => expect(routerPush).toHaveBeenCalledWith('/login'))
}

beforeEach(() => {
  vi.clearAllMocks()
  servicePost.mockResolvedValue(refreshOk())
  serviceRequest.mockResolvedValue('replayed')
})

describe('自动续期：成功路径', () => {
  it('HTTP 401 → 续期 → 重放原请求', async () => {
    const config = { url: '/member/list', method: 'get' }

    const result = await handlers.responseError!({ response: { status: 401 }, config })

    expect(result).toBe('replayed')
    expect(servicePost).toHaveBeenCalledTimes(1)
    expect(servicePost.mock.calls[0][0]).toBe('/auth/refresh')
    expect(serviceRequest).toHaveBeenCalledTimes(1)
    // 会话没被清：这正是「续期」相对「直接踢出」的意义
    expect(clearLoginFlag).not.toHaveBeenCalled()
    expect(routerPush).not.toHaveBeenCalled()
  })

  it('续期请求不带任何凭据（凭据在 HttpOnly cookie 里，前端读不到）', async () => {
    // 后端刷新是**轮换**式的，凭据由 cookie 携带、新值由 Set-Cookie 下发。
    // 这里必须断言「没有 refreshToken 字段」：服务端的取值是 body 优先
    // （authcookie.RefreshFromRequest），一旦前端传了空串，
    // 就会把 cookie 里那份有效凭据顶掉 —— 表现为「每次续期都失败」。
    await handlers.responseError!({
      response: { status: 401 },
      config: { url: '/member/list' },
    })

    const [, body] = servicePost.mock.calls[0]
    expect(body).not.toHaveProperty('refreshToken')
  })

  it('HTTP 200 + body code 401 也走续期（不能只在 axios 的 error 分支处理）', async () => {
    const config = { url: '/member/list' }

    const result = await handlers.response!({
      data: { code: 401, message: 'Token已失效', data: null },
      config,
    })

    expect(result).toBe('replayed')
    expect(servicePost).toHaveBeenCalledTimes(1)
    expect(clearLoginFlag).not.toHaveBeenCalled()
  })

  it('续期请求带 skipAuthRefresh 标记，避免它自己的 401 又触发续期', async () => {
    await handlers.responseError!({
      response: { status: 401 },
      config: { url: '/member/list' },
    })

    const cfg = servicePost.mock.calls[0][2]
    expect(cfg.skipAuthRefresh).toBe(true)
  })

  it('重放时给 config 打上 authRetried，作为「只重放一次」的依据', async () => {
    const config: Record<string, unknown> = { url: '/member/list' }

    await handlers.responseError!({ response: { status: 401 }, config })

    expect(config.authRetried).toBe(true)
    expect(serviceRequest).toHaveBeenCalledWith(config)
  })

  it('重放时不再注入 Authorization 头 —— 凭据由浏览器自动携带', () => {
    // B2 之后 request 拦截器不再读 cookie 拼头（token 是 HttpOnly，读不到）。
    // 重放之所以能拿到新 token，是因为服务端在续期响应里 Set-Cookie 了新值，
    // 浏览器会自动带上 —— 这一段在单测里覆盖不到（没有真实浏览器），
    // 由实机冒烟验证。这里钉住的是「别再注入」这个约定。
    const config = { url: '/member/list', headers: {} as Record<string, string> }

    handlers.request!(config)

    expect(config.headers.Authorization).toBeUndefined()
  })
})

describe('自动续期：并发只发一次', () => {
  it('3 个请求同时 401，只发一次续期请求，且各自都能重放', async () => {
    const d = deferred<unknown>()
    servicePost.mockReturnValue(d.promise)

    const configs = [{ url: '/a' }, { url: '/b' }, { url: '/c' }]
    const pending = configs.map((config) =>
      handlers.responseError!({ response: { status: 401 }, config }),
    )

    // 关键断言：续期还没完成时，后续的 401 必须复用同一个在途请求
    expect(servicePost).toHaveBeenCalledTimes(1)

    d.resolve(refreshOk())
    const results = await Promise.all(pending)

    expect(results).toEqual(['replayed', 'replayed', 'replayed'])
    expect(servicePost).toHaveBeenCalledTimes(1)
    expect(serviceRequest).toHaveBeenCalledTimes(3)
    expect(clearLoginFlag).not.toHaveBeenCalled()
  })

  it('一次续期失败后，下一次 401 会重新发起续期（而不是复用那个失败的 Promise）', async () => {
    // 单飞状态必须在结束时释放。若漏了这一步，第一次失败会让后续所有
    // 续期都立刻拿到同一个 rejection，用户永远续不上
    servicePost.mockRejectedValueOnce(new Error('boom'))

    await expect(
      handlers.responseError!({ response: { status: 401 }, config: { url: '/a' } }),
    ).rejects.toBeTruthy()
    expect(clearLoginFlag).toHaveBeenCalledTimes(1)

    vi.clearAllMocks()
    servicePost.mockResolvedValue(refreshOk())
    serviceRequest.mockResolvedValue('replayed')

    const result = await handlers.responseError!({
      response: { status: 401 },
      config: { url: '/b' },
    })

    expect(result).toBe('replayed')
    expect(servicePost).toHaveBeenCalledTimes(1)
  })
})

describe('自动续期：这些情况必须直接清会话，不能再续期', () => {
  it('带 skipAuthRefresh 标记的请求 401 时不再续期', async () => {
    // 刻意用一个**不在** NO_REFRESH_PATHS 里的路径：这样这条用例只可能
    // 被「标记守卫」拦住。若路径也放在排除表里，两个守卫会同时生效，
    // 去掉任何一个用例都照样通过 —— 那样的用例没有区分力（变异验证抓出来的）
    await expect(
      handlers.responseError!({
        response: { status: 401 },
        config: { url: '/custom/path', skipAuthRefresh: true },
      }),
    ).rejects.toBeTruthy()

    expect(servicePost).not.toHaveBeenCalled()
    await expectSessionCleared()
  })

  it('即使忘了打标记，/auth/refresh 路径的 401 也不会续期（安全网）', async () => {
    // 这层路径判断是**安全网**：`api/auth.ts` 里另有一个 refreshToken() 封装，
    // 它调 /auth/refresh 时并不会带 skipAuthRefresh 标记。若哪天有人改用它，
    // 少了这层判断就会多发一轮注定失败的续期请求
    await expect(
      handlers.responseError!({
        response: { status: 401 },
        config: { url: '/auth/refresh' },
      }),
    ).rejects.toBeTruthy()

    expect(servicePost).not.toHaveBeenCalled()
    await expectSessionCleared()
  })

  it('已经重放过一次的请求又 401（防无限循环）', async () => {
    await expect(
      handlers.responseError!({
        response: { status: 401 },
        config: { url: '/member/list', authRetried: true },
      }),
    ).rejects.toBeTruthy()

    expect(servicePost).not.toHaveBeenCalled()
    await expectSessionCleared()
  })

  it('前端已无从判断有没有凭据，所以仍会尝试一次续期', async () => {
    // B2 之后前端读不到 refresh token（HttpOnly），无法再做「本地没凭据就别白跑
    // 一次」的短路。这是刻意接受的代价：漏判的代价（明明有凭据却不续期，
    // 用户照样被踢出去）比多发一次注定失败的请求大得多。
    // 这里把新行为钉住，防止有人「顺手」用 logged_in 标记加回一个短路 ——
    // 那个标记不代表会话有效，用它做判断就会漏续期。
    const result = await handlers.responseError!({
      response: { status: 401 },
      config: { url: '/member/list' },
    })

    expect(result).toBe('replayed')
    expect(servicePost).toHaveBeenCalledTimes(1)
    expect(clearLoginFlag).not.toHaveBeenCalled()
  })

  it('登录接口的 401 不触发续期（此时去续期只会拿陈旧凭据白跑一次）', async () => {
    await expect(
      handlers.responseError!({ response: { status: 401 }, config: { url: '/auth/login' } }),
    ).rejects.toBeTruthy()

    expect(servicePost).not.toHaveBeenCalled()
    await expectSessionCleared()
  })

  it('没有 config（无法重放）时直接清会话', async () => {
    await expect(handlers.responseError!({ response: { status: 401 } })).rejects.toBeTruthy()

    expect(servicePost).not.toHaveBeenCalled()
    await expectSessionCleared()
  })
})

describe('自动续期：失败与其它错误', () => {
  it('续期失败时抛的是**原始错误**，且清会话', async () => {
    servicePost.mockRejectedValue(new Error('网络断了'))
    const original = { response: { status: 401 }, message: 'Unauthorized', config: { url: '/a' } }

    await expect(handlers.responseError!(original)).rejects.toBe(original)

    await expectSessionCleared()
  })

  it('body code 401 续期失败时抛的仍是 BizError（调用方按码分支的写法不受影响）', async () => {
    servicePost.mockRejectedValue(new Error('refresh token 已失效'))

    await expect(
      handlers.response!({
        data: { code: 401, message: 'Token已失效', data: null },
        config: { url: '/a' },
      }),
    ).rejects.toBeInstanceOf(BizError)

    await expectSessionCleared()
  })

  it('非 401 的 HTTP 错误只弹提示，不清会话也不续期', async () => {
    await expect(
      handlers.responseError!({ response: { status: 500 }, message: '服务器错误' }),
    ).rejects.toBeTruthy()

    expect(servicePost).not.toHaveBeenCalled()
    expect(clearLoginFlag).not.toHaveBeenCalled()
    expect(routerPush).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalled()
  })

  it('非 401 的业务错误弹提示并抛 BizError', async () => {
    await expect(
      handlers.response!({ data: { code: 400, message: '参数错误', data: null }, config: {} }),
    ).rejects.toBeInstanceOf(BizError)

    expect(servicePost).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalled()
  })
})
