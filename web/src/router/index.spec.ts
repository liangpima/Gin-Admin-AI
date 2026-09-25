// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * 路由守卫的单元测试（P3-B2）。
 *
 * 为什么在动 B2 之前先补这组用例：守卫是**登录态判定的唯一落点** ——
 * 它决定了「刷新页面还能不能停在原页面」。而 B2 把判定依据从
 * 「本地有没有 token」（`getToken()`）换成了「服务端下发的登录态标记」，
 * 换错了的现象是「登录成功但刷新一下就回登录页」或「死循环跳 /login」，
 * 两者都不会报错、只会让页面看起来坏掉。没有用例兜着，改这里等于盲改。
 *
 * 被测模块（`@/router/index`）在导入时就注册守卫，因此这里 mock 掉
 * vue-router，把注册进来的处理函数抓出来直接调用 —— 与 refresh.spec.ts 同一套手法。
 *
 * ⚠️ `@/utils/auth` 的 mock **只导出 `isLoggedIn`**。这是刻意的：
 * 若实现回退去调 `getToken()`，这里会得到 `undefined is not a function`，
 * 用例立刻失败。这比断言「调用了 isLoggedIn」更能防回退 ——
 * 后者只要求新接口被调用，不阻止旧接口同时被调用。
 */

const mocks = vi.hoisted(() => {
  type GuardFn = (to: unknown, from: unknown, next: (arg?: unknown) => void) => unknown
  return {
    guard: undefined as GuardFn | undefined,
    addedRoutes: [] as unknown[],
    routerPush: vi.fn(),
    isLoggedIn: vi.fn(),
    getInfo: vi.fn(),
    logout: vi.fn(),
    generateRoutes: vi.fn(),
    resetRoutes: vi.fn(),
    /** 守卫读的是 userStore.roles.length，所以这里要能按用例改 */
    userState: { roles: [] as string[] },
  }
})

vi.mock('vue-router', () => ({
  createRouter: () => ({
    beforeEach: (fn: never) => {
      mocks.guard = fn as unknown as typeof mocks.guard
    },
    afterEach: () => {},
    addRoute: (route: unknown) => {
      mocks.addedRoutes.push(route)
    },
    getRoutes: () => [],
    push: mocks.routerPush,
  }),
  createWebHistory: () => ({}),
}))

vi.mock('nprogress', () => ({ default: { start: vi.fn(), done: vi.fn() } }))

vi.mock('@/utils/auth', () => ({
  isLoggedIn: () => mocks.isLoggedIn(),
}))

vi.mock('@/store/modules/user', () => ({
  useUserStore: () => ({
    roles: mocks.userState.roles,
    getInfo: () => mocks.getInfo(),
    logout: () => mocks.logout(),
  }),
}))

vi.mock('@/store/modules/permission', () => ({
  usePermissionStore: () => ({
    generateRoutes: (...args: unknown[]) => mocks.generateRoutes(...args),
    resetRoutes: () => mocks.resetRoutes(),
  }),
}))

vi.mock('@/router/routes/static', () => ({ constantRoutes: [] }))

// 导入即注册守卫
await import('@/router/index')

interface GuardTo {
  path: string
  query?: Record<string, unknown>
  hash?: string
  meta?: Record<string, unknown>
}

/** 跑一次守卫并返回 next 的 mock（守卫是 async，必须 await 才能看到异步分支的结果） */
async function runGuard(to: Partial<GuardTo> & { path: string }) {
  const next = vi.fn()
  await mocks.guard!({ query: {}, hash: '', ...to }, {}, next)
  return next
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.addedRoutes.length = 0
  mocks.userState.roles = []
})

describe('路由守卫：未登录', () => {
  it('访问业务页面 → 跳登录页', async () => {
    mocks.isLoggedIn.mockReturnValue(false)

    const next = await runGuard({ path: '/dashboard' })

    expect(next).toHaveBeenCalledWith('/login')
    // 不该白拉一次用户信息
    expect(mocks.getInfo).not.toHaveBeenCalled()
  })

  it('访问白名单页面（/login、/404）→ 放行', async () => {
    mocks.isLoggedIn.mockReturnValue(false)

    for (const path of ['/login', '/404']) {
      const next = await runGuard({ path })
      // 无参调用 = 放行
      expect(next, `${path} 应放行`).toHaveBeenCalledWith()
    }
  })
})

describe('路由守卫：已登录', () => {
  it('访问 /login → 重定向到首页', async () => {
    mocks.isLoggedIn.mockReturnValue(true)

    const next = await runGuard({ path: '/login' })

    expect(next).toHaveBeenCalledWith('/')
  })

  it('角色已加载 → 直接放行，不重复拉用户信息', async () => {
    mocks.isLoggedIn.mockReturnValue(true)
    mocks.userState.roles = ['admin']

    const next = await runGuard({ path: '/dashboard' })

    expect(next).toHaveBeenCalledWith()
    expect(mocks.getInfo).not.toHaveBeenCalled()
  })

  it('角色为空（刷新页面后的冷启动）→ 拉用户信息、注册动态路由、按路径重导航', async () => {
    mocks.isLoggedIn.mockReturnValue(true)
    mocks.getInfo.mockResolvedValue({ menus: [{ id: 1 }] })
    mocks.generateRoutes.mockReturnValue([{ path: '/dashboard', name: 'Dashboard' }])

    const next = await runGuard({ path: '/dashboard' })

    expect(mocks.getInfo).toHaveBeenCalled()
    expect(mocks.addedRoutes).toHaveLength(1)
    // 按**路径**重导航而不是展开 `{ ...to }` 重放：
    // 兜底命中时 to.name 是 NotFoundCatchAll，而 vue-router 解析 location 时
    // name 优先于 path —— 展开重放会再命中一次兜底，业务页永远打不开
    expect(next).toHaveBeenCalledWith({ path: '/dashboard', query: {}, hash: '', replace: true })
  })

  it('菜单为空 → 清会话并跳 404（而不是停在空白布局里）', async () => {
    mocks.isLoggedIn.mockReturnValue(true)
    mocks.getInfo.mockResolvedValue({ menus: [] })
    mocks.generateRoutes.mockReturnValue([])

    const next = await runGuard({ path: '/dashboard' })

    expect(mocks.logout).toHaveBeenCalled()
    expect(next).toHaveBeenCalledWith('/404')
  })

  it('拉用户信息失败 → 清会话并跳登录页', async () => {
    mocks.isLoggedIn.mockReturnValue(true)
    mocks.getInfo.mockRejectedValue(new Error('401'))

    const next = await runGuard({ path: '/dashboard' })

    expect(mocks.logout).toHaveBeenCalled()
    expect(mocks.resetRoutes).toHaveBeenCalled()
    expect(next).toHaveBeenCalledWith('/login')
  })
})

describe('路由守卫：登录态判定依据（B2 的核心变更）', () => {
  it('放行与否完全由 isLoggedIn 决定，不再读本地 token', async () => {
    // 标记为真 → 走到「已登录」分支（拉用户信息），而不是被踢去 /login。
    // 若守卫还在调已删除的 getToken()，本文件会直接抛
    // 「getToken is not a function」，这就是我们要的失败方式。
    mocks.isLoggedIn.mockReturnValue(true)
    mocks.getInfo.mockResolvedValue({ menus: [{ id: 1 }] })
    mocks.generateRoutes.mockReturnValue([{ path: '/dashboard', name: 'Dashboard' }])

    await runGuard({ path: '/dashboard' })

    expect(mocks.isLoggedIn).toHaveBeenCalled()
    expect(mocks.getInfo).toHaveBeenCalled()
  })

  it('标记为真但会话已失效（userInfo 401）→ 仍然落到登录页', async () => {
    // 这是「标记不代表会话有效」的行为验证：cookie 可能过期或已在服务端被吊销，
    // 守卫必须允许这条路径走到失败分支，而不是因为「标记在」就放行到底。
    mocks.isLoggedIn.mockReturnValue(true)
    mocks.getInfo.mockRejectedValue(new Error('Token已失效'))

    const next = await runGuard({ path: '/system/user' })

    expect(next).toHaveBeenCalledWith('/login')
  })
})
