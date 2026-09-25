import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

/**
 * 路由兜底的单元测试（P2-5 配套；P3-2 引入兜底后回归，本文件随之修订）。
 *
 * 背景：`constantRoutes` 里原先没有 `pathMatch` 通配路由，访问未匹配的路径
 * 只会得到「控制台警告 + 空白页」。这里守住三件事：
 *   1. 未匹配的路径必须渲染 404，且**保留原始路径**
 *   2. 兜底**不能盖住**具体路径 —— 包括之后通过 `addRoute` 动态注册的业务路由
 *   3. 兜底**不能写成 `redirect`**（见下）
 *
 * 第 2 条是这条路由真正容易写错的地方：Vue Router 4 按路径具体度打分，
 * 所以 catch-all 放在数组最后只是可读性上的约定，**不是**它能正确工作的原因。
 *
 * 第 3 条是踩过的坑：兜底最初写成 `redirect: '/404'`，结果**所有业务页面都变 404**。
 * 因为 vue-router 的 `pushWithRedirect` 里，`handleRedirectRecord` 跑在
 * `navigate()`（`beforeEach` 在其中）**之前** —— 守卫拿到的 `to` 已经是 `/404`，
 * 原始路径只留在 `to.redirectedFrom` 里。于是「登录后 `addRoute` 再按 `to` 重导航」
 * 这一步永远回不到业务路由。改用 `component` 之后，守卫就能看见真实路径。
 *
 * ⚠️ 本文件测的是**路由表**，不含守卫接线：`router/index.ts` 依赖
 * `createWebHistory`（需要 window）并会拉起 Pinia/API，在 node 环境下跑不起来。
 * 守卫那一段（`addRoute` 之后必须按 `to.path` 重导航，**不能**展开 `{ ...to }`
 * 重放 —— 因为 `to.name` 仍是 `NotFoundCatchAll`，展开会按 name 再命中一次兜底）
 * 靠实机验证兜住。
 *
 * Layout 用 mock 顶掉：这条用例只关心路由匹配结果，把整个布局组件树
 * （Navbar/Sidebar/Element Plus…）拖进 node 环境没有意义，还会让用例
 * 因为与路由无关的原因变脆。
 */
vi.mock('@/layout/index.vue', () => ({
  default: { name: 'Layout', render: () => null },
}))

const { constantRoutes } = await import('./static')

function newRouter(extra: RouteRecordRaw[] = []) {
  return createRouter({
    history: createMemoryHistory(),
    routes: [...(constantRoutes as RouteRecordRaw[]), ...extra],
  })
}

/** 只关心「有没有 redirect / component」，不关心是哪种 RouteRecordRaw 变体 */
type CatchAllShape = { path?: unknown; redirect?: unknown; component?: unknown }

function findCatchAll(): CatchAllShape | undefined {
  return (constantRoutes as unknown as CatchAllShape[]).find((r) => r.path === '/:pathMatch(.*)*')
}

describe('constantRoutes 的 404 兜底', () => {
  it('存在 pathMatch 通配路由', () => {
    expect(findCatchAll(), 'constantRoutes 里必须有 /:pathMatch(.*)* 兜底路由').toBeDefined()
  })

  it('兜底必须用 component 渲染，不能写 redirect', () => {
    // redirect 会在 beforeEach **之前**被 handleRedirectRecord 解析掉，守卫因此
    // 看不到原始路径 —— 那正是「所有业务页面都变 404」的成因。
    expect(
      findCatchAll()?.redirect,
      '兜底不能写 redirect：它在守卫之前就被解析，会把原始路径吞掉',
    ).toBeUndefined()
    expect(findCatchAll()?.component, '兜底必须有 component').toBeDefined()
  })

  it('未匹配的路径落到兜底，并保留原始路径', async () => {
    const router = newRouter()
    await router.push('/this-path-does-not-exist')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('NotFoundCatchAll')
    // 保留原始路径，是守卫「addRoute 之后重导航回去」的前提
    expect(router.currentRoute.value.path).toBe('/this-path-does-not-exist')
  })

  it('深层未匹配路径也落到兜底', async () => {
    const router = newRouter()
    await router.push('/a/b/c/d')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('NotFoundCatchAll')
    expect(router.currentRoute.value.path).toBe('/a/b/c/d')
  })

  it('兜底不盖住静态路由', () => {
    const router = newRouter()

    // 用 resolve 而不是 push：这里只关心「匹配到哪条」，不需要真的加载组件。
    // push('/login') 会把登录页拉进来，而它经 @/api 间接 import 了真实的
    // @/router（createWebHistory 需要 window），在 node 环境下会炸 ——
    // 那是与本次断言无关的噪音。
    expect(router.resolve('/login').name).toBe('Login')
    expect(router.resolve('/').name).toBeUndefined()
    expect(router.resolve('/dashboard').name).toBe('Dashboard')
  })

  it('兜底不盖住之后动态注册的业务路由', async () => {
    const router = newRouter()

    // 模拟登录后 permissionStore.addRoute 的行为：业务路由是**之后**才注册的，
    // 而 catch-all 早已在 constantRoutes 里。若匹配按注册顺序走，这里就会
    // 命中兜底而不是业务路由。
    router.addRoute({
      path: '/system/user',
      name: 'SystemUser',
      component: { render: () => null },
    })

    await router.push('/system/user')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('SystemUser')

    // 反向确认：同一时刻未注册的兄弟路径仍然落到兜底
    await router.push('/system/not-registered')
    expect(router.currentRoute.value.name).toBe('NotFoundCatchAll')
    expect(router.currentRoute.value.path).toBe('/system/not-registered')
  })

  it('真实时序：先导航（未注册）再 addRoute，按原路径重导航能到达业务路由', async () => {
    const router = newRouter()

    // 用户直接访问 /system/user（刷新 / 分享链接）—— 此刻业务路由还没注册。
    // 这是上面那条用例漏掉的关键：它不是「先 addRoute 再 push」，而是
    // 「先 push（命中兜底）→ 守卫里才 addRoute → 再按原路径重导航」。
    await router.push('/system/user')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('NotFoundCatchAll')
    expect(router.currentRoute.value.path).toBe('/system/user')

    // 守卫拿到用户信息后注册业务路由，然后按**原始路径**重导航
    router.addRoute({
      path: '/system/user',
      name: 'SystemUser',
      component: { render: () => null },
    })
    await router.push(router.currentRoute.value.fullPath)

    expect(router.currentRoute.value.name).toBe('SystemUser')
  })

  it('/404 自身不会被兜底绕成死循环', async () => {
    const router = newRouter()
    await router.push('/404')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('NotFound')
  })
})
