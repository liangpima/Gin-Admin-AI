import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'

/**
 * 路由兜底的单元测试（P2-5 配套）。
 *
 * 背景：`constantRoutes` 里原先没有 `pathMatch` 通配路由，访问未匹配的路径
 * 只会得到「控制台警告 + 空白页」。这里守住两件事：
 *   1. 未匹配的路径必须落到 `/404`（而不是什么都不发生）
 *   2. 兜底**不能盖住**具体路径 —— 包括之后通过 `addRoute` 动态注册的业务路由
 *
 * 第 2 条是这条路由真正容易写错的地方：Vue Router 4 按路径具体度打分，
 * 所以 catch-all 放在数组最后只是可读性上的约定，**不是**它能正确工作的原因。
 * 万一将来有人把兜底改成前缀匹配（`/:pathMatch(.*)` 少一个 `*`）或用
 * 重定向链把它提到前面，具体路径就会被吞掉 —— 那是个「所有业务页面都变 404」
 * 的故障，值得用用例钉住。
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

describe('constantRoutes 的 404 兜底', () => {
  it('存在 pathMatch 通配路由', () => {
    const found = (constantRoutes as RouteRecordRaw[]).some((r) => r.path === '/:pathMatch(.*)*')
    expect(found, 'constantRoutes 里必须有 /:pathMatch(.*)* 兜底路由').toBe(true)
  })

  it('未匹配的路径重定向到 /404', async () => {
    const router = newRouter()
    await router.push('/this-path-does-not-exist')
    await router.isReady()

    expect(router.currentRoute.value.path).toBe('/404')
    expect(router.currentRoute.value.name).toBe('NotFound')
  })

  it('深层未匹配路径也落到 /404', async () => {
    const router = newRouter()
    await router.push('/a/b/c/d')
    await router.isReady()

    expect(router.currentRoute.value.path).toBe('/404')
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
    expect(router.currentRoute.value.path).toBe('/404')
  })

  it('/404 自身不会被兜底绕成死循环', async () => {
    const router = newRouter()
    await router.push('/404')
    await router.isReady()

    expect(router.currentRoute.value.name).toBe('NotFound')
  })
})
