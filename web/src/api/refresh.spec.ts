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

const getToken = vi.fn()
const getRefreshToken = vi.fn()
const setToken = vi.fn()
const setRefreshToken = vi.fn()
const removeToken = vi.fn()
vi.mock('@/utils/auth', () => ({
  getToken: () => getToken(),
  getRefreshToken: () => getRefreshToken(),
  setToken: (...a: unknown[]) => setToken(...a),
  setRefreshToken: (...a: unknown[]) => setRefreshToken(...a),
  removeToken: () => removeToken(),
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

/** 断言「会话被清理」：清 token + 跳登录页 */
async function expectSessionCleared() {
  expect(removeToken).toHaveBeenCalled()
  await vi.waitFor(() => expect(routerPush).toHaveBeenCalledWith('/login'))
}

beforeEach(() => {
  vi.clearAllMocks()
  getRefreshToken.mockReturnValue('old-refresh')
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
    expect(removeToken).not.toHaveBeenCalled()
    expect(routerPush).not.toHaveBeenCalled()
  })

  it('刷新时同时更新 access 与 refresh 两个 token', async () => {
    // 后端刷新是**轮换**：只更新 access token 的话，
    // 下一次续期用的还是已被消费掉的旧 refresh token，第二次必然失败
    await handlers.responseError!({
      response: { status: 401 },
      config: { url: '/member/list' },
    })

    expect(setToken).toHaveBeenCalledWith('new-access')
    expect(setRefreshToken).toHaveBeenCalledWith('new-refresh')
  })

  it('HTTP 200 + body code 401 也走续期（不能只在 axios 的 error 分支处理）', async () => {
    const config = { url: '/member/list' }

    const result = await handlers.response!({
      data: { code: 401, message: 'Token已失效', data: null },
      config,
    })

    expect(result).toBe('replayed')
    expect(servicePost).toHaveBeenCalledTimes(1)
    expect(removeToken).not.toHaveBeenCalled()
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

  it('重放请求经过 request 拦截器时会带上刚换到的新 token', async () => {
    // 重放走的是同一个 axios 实例，因此 request 拦截器会重新读 cookie。
    // 这条断言把「重放拿到的必须是新 token」钉住 —— 若实现改成
    // 「把旧 config 原样再发一次」，请求头里就还是过期的 token，续期等于白做
    getToken.mockReturnValue('new-access')
    const config = { url: '/member/list', headers: {} as Record<string, string> }

    await handlers.responseError!({ response: { status: 401 }, config })
    // 手动跑一遍 request 拦截器，模拟重放时 axios 的真实行为
    handlers.request!(config)

    expect(config.headers.Authorization).toBe('Bearer new-access')
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
    expect(removeToken).not.toHaveBeenCalled()
  })

  it('一次续期失败后，下一次 401 会重新发起续期（而不是复用那个失败的 Promise）', async () => {
    // 单飞状态必须在结束时释放。若漏了这一步，第一次失败会让后续所有
    // 续期都立刻拿到同一个 rejection，用户永远续不上
    servicePost.mockRejectedValueOnce(new Error('boom'))

    await expect(
      handlers.responseError!({ response: { status: 401 }, config: { url: '/a' } }),
    ).rejects.toBeTruthy()
    expect(removeToken).toHaveBeenCalledTimes(1)

    vi.clearAllMocks()
    getRefreshToken.mockReturnValue('old-refresh')
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

  it('本地没有 refresh token（无从续期）', async () => {
    getRefreshToken.mockReturnValue(undefined)

    await expect(
      handlers.responseError!({ response: { status: 401 }, config: { url: '/member/list' } }),
    ).rejects.toBeTruthy()

    // 不该白跑一次注定失败的请求
    expect(servicePost).not.toHaveBeenCalled()
    await expectSessionCleared()
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
    getRefreshToken.mockReturnValue(undefined)

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
    expect(removeToken).not.toHaveBeenCalled()
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
