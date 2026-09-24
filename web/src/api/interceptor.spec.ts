import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * 响应拦截器的单元测试（P2-4）。
 *
 * 拦截器是全站所有请求的必经之路，但它此前没有任何用例 ——
 * 而它的三个行为都有明确的业务含义：
 *   1. code === 0 时**解包** response.data（所以调用方拿到的是业务体，不是 axios 响应）
 *   2. code !== 0 时抛 BizError 并**保留业务码**（调用方才能按码分支）
 *   3. code === 401 时清会话跳登录（而不是弹一个「请求失败」）
 *
 * 用 vi.mock 把 axios / element-plus / 路由 / token 工具都替换掉，
 * 让被测模块能在不启动浏览器的情况下加载，并直接调用注册进来的处理函数。
 */

type ResponseHandler = (response: { data: unknown; config?: unknown }) => unknown

const handlers: {
  response?: ResponseHandler
  responseError?: (err: unknown) => unknown
  request?: (config: { headers: Record<string, string> }) => unknown
} = {}

vi.mock('axios', () => {
  const instance = {
    interceptors: {
      request: {
        use: (ok: typeof handlers.request) => {
          handlers.request = ok
        },
      },
      response: {
        use: (ok: ResponseHandler, err: (e: unknown) => unknown) => {
          handlers.response = ok
          handlers.responseError = err
        },
      },
    },
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  }
  return {
    default: { create: () => instance },
  }
})

const ElMessage = { error: vi.fn(), success: vi.fn(), warning: vi.fn() }
vi.mock('element-plus', () => ({ ElMessage }))

const removeToken = vi.fn()
const getToken = vi.fn()
vi.mock('@/utils/auth', () => ({
  getToken: () => getToken(),
  removeToken: () => removeToken(),
}))

const routerPush = vi.fn(() => Promise.resolve())
vi.mock('@/router', () => ({ default: { push: routerPush } }))

const { BizError, isBizError } = await import('@/api/index')

describe('响应拦截器：成功解包', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('code === 0 时返回 data 本身（而不是整个 axios 响应）', async () => {
    // 这是 `http.get<Result<X>>(url)` 能拿到业务体的前提。
    // 若改成返回 response，所有调用方都要多写一层 .data，且是静默出错
    const payload = { code: 0, message: 'success', data: { id: 1, name: '张三' } }
    const result = await handlers.response!({ data: payload, config: {} })

    expect(result).toEqual(payload)
  })
})

describe('响应拦截器：业务错误', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('code !== 0 时抛 BizError 并保留业务码', async () => {
    const payload = { code: 400, message: '用户名已存在', data: null }

    await expect(handlers.response!({ data: payload, config: {} })).rejects.toBeInstanceOf(BizError)

    try {
      await handlers.response!({ data: payload, config: {} })
    } catch (e) {
      // 保留业务码是关键：调用方要能区分 400/403/404 做差异化处理，
      // 只留文案的话后端改一个字就静默失效
      expect(isBizError(e)).toBe(true)
      expect((e as InstanceType<typeof BizError>).code).toBe(400)
      expect((e as Error).message).toBe('用户名已存在')
    }
    expect(ElMessage.error).toHaveBeenCalled()
  })

  it('403 也会弹提示但不跳登录页', async () => {
    await expect(
      handlers.response!({ data: { code: 403, message: '没有权限', data: null }, config: {} }),
    ).rejects.toBeInstanceOf(BizError)

    // 403 是「知道你是谁，但这件事不归你做」—— 重新登录没用，不该踢出登录
    expect(routerPush).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalled()
  })

  it('401 走清会话并跳登录，而不是弹「请求失败」', async () => {
    await expect(
      handlers.response!({ data: { code: 401, message: 'Token已失效', data: null }, config: {} }),
    ).rejects.toBeInstanceOf(BizError)

    expect(removeToken).toHaveBeenCalled()
    // 跳转是异步的（handleLogout 内部先清 token 再跳）
    await vi.waitFor(() => expect(routerPush).toHaveBeenCalledWith('/login'))
  })

  it('二进制下载里的错误 JSON 也会被识别出来', async () => {
    // 导出 Excel 时后端出错仍返回 HTTP 200 + JSON，
    // 若不识别，用户拿到的就是一个「内容其实是错误信息的 Excel」
    const blob = {
      type: 'application/json',
      text: () => Promise.resolve(JSON.stringify({ code: 400, message: '导出失败', data: null })),
    }

    await expect(
      handlers.response!({ data: blob, config: { responseType: 'blob' } }),
    ).rejects.toThrow('导出失败')
  })

  it('正常的二进制下载原样返回', async () => {
    const blob = { type: 'application/vnd.ms-excel', text: () => Promise.resolve('') }
    const result = await handlers.response!({ data: blob, config: { responseType: 'blob' } })

    expect(result).toEqual({ data: blob, config: { responseType: 'blob' } })
  })
})

describe('响应拦截器：网络层错误', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('HTTP 401 也走清会话跳登录', async () => {
    await expect(
      handlers.responseError!({ response: { status: 401 }, message: 'Unauthorized' }),
    ).rejects.toBeTruthy()

    expect(removeToken).toHaveBeenCalled()
    await vi.waitFor(() => expect(routerPush).toHaveBeenCalledWith('/login'))
  })

  it('其它网络错误只弹提示', async () => {
    await expect(
      handlers.responseError!({ response: { status: 500 }, message: '服务器错误' }),
    ).rejects.toBeTruthy()

    expect(routerPush).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalled()
  })
})

describe('请求拦截器：带 token', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('有 token 时写入 Authorization 头', () => {
    getToken.mockReturnValue('abc123')
    const config = { headers: {} as Record<string, string> }

    handlers.request!(config)

    expect(config.headers.Authorization).toBe('Bearer abc123')
  })

  it('没有 token 时不写该头（而不是写成 Bearer undefined）', () => {
    getToken.mockReturnValue(undefined)
    const config = { headers: {} as Record<string, string> }

    handlers.request!(config)

    expect(config.headers.Authorization).toBeUndefined()
  })
})
