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
  type AfterFn = (to: unknown, from: unknown) => unknown
  return {
    guard: undefined as GuardFn | undefined,
    afterEachHook: undefined as AfterFn | undefined,
    addedRoutes: [] as unknown[],
    routerPush: vi.fn(),
    isLoggedIn: vi.fn(),
    getInfo: vi.fn(),
    /**
     * 只导出 clearSession，**刻意不导出 logout**。
     *
     * 守卫必须同步清会话（H10）：`logout()` 要等 `await logoutApi()` 的网络
     * 往返才清 `logged_in`，而它后面的 `next('/login')` 是同步执行的 ——
     * 标记还在 → 守卫重入 → 重定向死循环。若实现回退去调 `logout()`，
     * 这里会直接抛「logout is not a function」，比断言「没调用 logout」更硬。
     */
    clearSession: vi.fn(),
    generateRoutes: vi.fn(),
    resetRoutes: vi.fn(),
    /**
     * 守卫判定「动态路由是否已注册」的依据。
     *
     * 它**不能**是 `userStore.roles.length === 0`（曾经的写法）：
     * 合法但没有分配任何角色的账号，roles 恒为空数组，于是每次导航都会
     * 重新 getInfo + generateRoutes + addRoute。用例见
     * 「零角色账号…不重复拉用户信息」那条。
     */
    permissionState: { routesLoaded: false },
    /** 标签栏数据源（H8）：守卫之外，afterEach 也必须有调用点 */
    addView: vi.fn(),
  }
})

vi.mock('vue-router', () => ({
  createRouter: () => ({
    beforeEach: (fn: never) => {
      mocks.guard = fn as unknown as typeof mocks.guard
    },
    afterEach: (fn: never) => {
      mocks.afterEachHook = fn as unknown as typeof mocks.afterEachHook
    },
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
  // ⚠️ 这个 mock **只导出守卫真正会用到的两个方法**。
  //
  // 刻意**不提供 `roles`**：守卫若回退成 `userStore.roles.length === 0` 判定，
  // 这里会直接抛「Cannot read properties of undefined (reading 'length')」，
  // 比断言「没读 roles」更硬 —— 与 `@/utils/auth` 只导出 isLoggedIn 同一手法。
  useUserStore: () => ({
    getInfo: () => mocks.getInfo(),
    clearSession: () => mocks.clearSession(),
  }),
}))

vi.mock('@/store/modules/permission', () => ({
  usePermissionStore: () => ({
    get routesLoaded() {
      return mocks.permissionState.routesLoaded
    },
    generateRoutes: (...args: unknown[]) => mocks.generateRoutes(...args),
    resetRoutes: () => mocks.resetRoutes(),
  }),
}))

vi.mock('@/store/modules/tagsView', () => ({
  useTagsViewStore: () => ({
    addView: (view: unknown) => mocks.addView(view),
  }),
}))

vi.mock('@/router/routes/static', () => ({ constantRoutes: [] }))

// 导入即注册守卫
await import('@/router/index')

interface GuardTo {
  path: string
  name?: string
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
  mocks.permissionState.routesLoaded = false
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

  it('动态路由已注册 → 直接放行，不重复拉用户信息', async () => {
    mocks.isLoggedIn.mockReturnValue(true)
    mocks.permissionState.routesLoaded = true

    const next = await runGuard({ path: '/dashboard' })

    expect(next).toHaveBeenCalledWith()
    expect(mocks.getInfo).not.toHaveBeenCalled()
  })

  it('零角色账号在路由已注册后不得重复拉用户信息', async () => {
    // 回归用例：哨兵原先是 `userStore.roles.length === 0`。
    // 一个合法但**没有分配任何角色**的账号 roles 恒为空数组，于是每一次导航
    // 都会重新 getInfo() + generateRoutes() + addRoute() —— 菜单被重复注册，
    // 且每次导航都多一次用户信息请求。切换成 permissionStore.routesLoaded
    // 后，判定依据才是「路由注册过没有」这件事本身。
    //
    // 注：userStore 的 mock 已经把 roles 整个拿掉了，所以回退写法会直接抛错，
    // 不只这一条用例会红。
    mocks.isLoggedIn.mockReturnValue(true)
    mocks.permissionState.routesLoaded = true
    mocks.getInfo.mockResolvedValue({ menus: [], roles: [] })

    const next = await runGuard({ path: '/dashboard' })

    expect(mocks.getInfo).not.toHaveBeenCalled()
    expect(next).toHaveBeenCalledWith()
  })

  it('路由未注册（刷新页面后的冷启动）→ 拉用户信息、注册动态路由、按路径重导航', async () => {
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

    expect(mocks.clearSession).toHaveBeenCalled()
    expect(next).toHaveBeenCalledWith('/404')
  })

  it('拉用户信息失败 → 清会话并跳登录页', async () => {
    mocks.isLoggedIn.mockReturnValue(true)
    mocks.getInfo.mockRejectedValue(new Error('401'))

    const next = await runGuard({ path: '/dashboard' })

    expect(mocks.clearSession).toHaveBeenCalled()
    expect(next).toHaveBeenCalledWith('/login')
  })
})

describe('路由守卫：失败分支必须**同步**清会话（H10）', () => {
  /**
   * 这条用例断言的是**调用顺序**，而不是「有没有调用」。
   *
   * 缺陷形态：`userStore.logout()` 是异步的 —— `clearSession()`（清
   * `logged_in` 标记的那一步）要等 `await logoutApi()` 的网络往返。
   * 而它后面的 `next('/login')` 是同步执行的：执行时标记仍在，守卫重入后
   * 走「loggedIn && to.path === '/login' → next('/')」→ 回业务路径 →
   * 再次 getInfo() 失败 → 重定向死循环 + 每轮一个注定失败的请求。
   *
   * 「调用了 clearSession」在异步写法下同样成立（只是晚了一点），
   * 所以只有顺序能区分两种实现。
   */
  async function recordOrder(to: Partial<GuardTo> & { path: string }) {
    const order: string[] = []
    mocks.clearSession.mockImplementation(() => {
      order.push('clearSession')
    })
    const next = vi.fn((arg?: unknown) => {
      order.push(`next:${String(arg)}`)
    })
    await mocks.guard!({ query: {}, hash: '', ...to }, {}, next)
    return order
  }

  it('getInfo 失败：先清会话，再跳登录页', async () => {
    mocks.isLoggedIn.mockReturnValue(true)
    mocks.getInfo.mockRejectedValue(new Error('后端 500'))

    const order = await recordOrder({ path: '/system/user' })

    expect(order).toEqual(['clearSession', 'next:/login'])
  })

  it('菜单为空：先清会话，再跳 404', async () => {
    mocks.isLoggedIn.mockReturnValue(true)
    mocks.getInfo.mockResolvedValue({ menus: [] })
    mocks.generateRoutes.mockReturnValue([])

    const order = await recordOrder({ path: '/dashboard' })

    expect(order).toEqual(['clearSession', 'next:/404'])
  })

  it('成功后按路径重导航时**不**清会话', async () => {
    // 反向验证：别把「同步清会话」加到了正常分支上，
    // 那会让每次冷启动都被踢回登录页
    mocks.isLoggedIn.mockReturnValue(true)
    mocks.getInfo.mockResolvedValue({ menus: [{ id: 1 }] })
    mocks.generateRoutes.mockReturnValue([{ path: '/dashboard', name: 'Dashboard' }])

    const order = await recordOrder({ path: '/dashboard' })

    expect(order).toEqual(['next:[object Object]'])
    expect(mocks.clearSession).not.toHaveBeenCalled()
  })
})

describe('标签栏数据源（H8）', () => {
  /**
   * `addView` 此前全仓没有任何调用点 —— 于是 `visitedViews` 恒为空：
   * 标签栏渲染 0 项，且 `<keep-alive :include="cachedViews">` 收到空数组，
   * 任何页面都不缓存（每次切路由都重新挂载，列表页筛选/分页/滚动位置全丢）。
   *
   * 这里断言的是「afterEach 里确实有调用点」，因为整条链路唯一的缺口就是它。
   */
  it('导航完成后把路由加入标签栏', () => {
    mocks.afterEachHook!({ path: '/system/user', name: 'User', meta: { title: '管理员' } }, {})

    expect(mocks.addView).toHaveBeenCalledTimes(1)
    expect(mocks.addView).toHaveBeenCalledWith(
      expect.objectContaining({ path: '/system/user', name: 'User' }),
    )
  })

  it('登录页 / 404 / 兜底路由不进标签栏', () => {
    // 这些都不是「用户真正打开过的工作页面」：进标签栏既无意义，
    // 关闭它们还会把用户带到一个空状态
    mocks.afterEachHook!({ path: '/login', name: 'Login' }, {})
    mocks.afterEachHook!({ path: '/404', name: 'NotFound' }, {})
    mocks.afterEachHook!({ path: '/typo-path', name: 'NotFoundCatchAll' }, {})

    expect(mocks.addView).not.toHaveBeenCalled()
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
