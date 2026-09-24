import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { MenuItem } from '@/api/menu'

/**
 * 动态路由生成的单元测试（P2-4）。
 *
 * `filterAsyncRoutes` 决定「后端返回的菜单树 → 前端能访问哪些路由」，
 * 是前端侧权限的落点。它的三条规则此前没有任何用例：
 *   1. type === 2（按钮）不生成路由，**子级也一样过滤**
 *   2. 有 component 的用真实视图；没有 component 但有可见子路由的用 Layout 包裹
 *   3. 两者都没有的**跳过**（生成出来就是一条打不开的空路由）
 *
 * 另外顺带守住 `import.meta.glob` 的路径拼接：视图查不到时路由会被静默跳过，
 * 表现为「菜单点进去空白」，而控制台不会报错。
 */

// Layout 与静态路由都在别处，这里只关心「菜单树怎么变成路由」，
// 替掉它们可以避免把整个布局（含 store、响应式 hook）拖进测试
vi.mock('@/layout/index.vue', () => ({ default: { name: 'MockLayout' } }))
vi.mock('@/router/routes/static', () => ({ constantRoutes: [{ path: '/login', name: 'Login' }] }))

const { usePermissionStore } = await import('@/store/modules/permission')

/** 造一个菜单节点 */
function menu(partial: Partial<MenuItem> & { name: string }): MenuItem {
  return {
    id: 1,
    parentId: 0,
    name: partial.name,
    path: partial.path ?? `/${partial.name}`,
    component: partial.component ?? '',
    icon: partial.icon ?? '',
    title: partial.title ?? partial.name,
    type: partial.type ?? 1,
    permission: '',
    sort: 0,
    visible: partial.visible ?? 1,
    status: 1,
    isExternal: 0,
    isCache: partial.isCache ?? 1,
    children: partial.children,
  } as MenuItem
}

describe('generateRoutes 菜单树转路由', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('按钮（type=2）不生成路由，嵌套的也一样', () => {
    const store = usePermissionStore()

    const routes = store.generateRoutes([
      menu({
        name: 'User',
        component: 'system/user/index',
        children: [
          menu({ name: 'UserAdd', type: 2 }), // 按钮：只用于 v-permission，不该变成路由
          menu({ name: 'UserEdit', type: 2 }),
        ],
      }),
    ])

    expect(routes).toHaveLength(1)
    // 按钮被过滤后没有可见子路由，因此不生成 children
    expect(routes[0].children).toBeUndefined()
  })

  it('没有 component 但有可见子路由的节点用 Layout 包裹', () => {
    const store = usePermissionStore()

    const routes = store.generateRoutes([
      menu({
        name: 'System',
        path: '/system',
        component: '',
        children: [menu({ name: 'User', path: 'user', component: 'system/user/index' })],
      }),
    ])

    expect(routes).toHaveLength(1)
    expect(routes[0].path).toBe('/system')
    expect(routes[0].component).toEqual({ name: 'MockLayout' })
    expect(routes[0].children).toHaveLength(1)
    expect(routes[0].children![0].path).toBe('user')
  })

  it('既没有 component 也没有可见子路由的节点被跳过', () => {
    const store = usePermissionStore()

    // 生成出来会是一条打不开的空路由；后端菜单配置里出现这种节点
    // （例如目录下只挂了按钮）时前端不该把它变成路由
    const routes = store.generateRoutes([
      menu({ name: 'Empty', component: '', children: [menu({ name: 'Btn', type: 2 })] }),
    ])

    expect(routes).toHaveLength(0)
  })

  it('component 指向不存在的视图时跳过而不是生成坏路由', () => {
    const store = usePermissionStore()

    // import.meta.glob 查不到该文件 → 拿不到组件 → 跳过。
    // 若不做这个判断，路由会注册成功但渲染为空，表现为「点菜单一片空白」
    const routes = store.generateRoutes([menu({ name: 'Ghost', component: 'not/exist/index' })])

    expect(routes).toHaveLength(0)
  })

  it('component 指向真实视图时能解析到组件（守住 glob 路径拼接）', () => {
    const store = usePermissionStore()

    const routes = store.generateRoutes([menu({ name: 'User', component: 'system/user/index' })])

    expect(routes).toHaveLength(1)
    expect(routes[0].component).toBeTruthy()
    // 组件应当是个动态 import 函数（glob 的产物），不是 undefined
    expect(typeof routes[0].component).not.toBe('string')
  })

  it('meta 由菜单字段映射而来', () => {
    const store = usePermissionStore()

    const routes = store.generateRoutes([
      menu({
        name: 'User',
        component: 'system/user/index',
        title: '用户管理',
        icon: 'User',
        visible: 0, // 隐藏
        isCache: 0, // 不缓存
      }),
    ])

    expect(routes[0].meta).toMatchObject({
      title: '用户管理',
      icon: 'User',
      hidden: true,
      noCache: true,
    })
  })

  it('setRoutes 同时维护 routes（含静态路由）与 addRoutes，并标记已加载', () => {
    const store = usePermissionStore()

    expect(store.routesLoaded).toBe(false)
    store.generateRoutes([])

    expect(store.routesLoaded).toBe(true)
    // routes 是「静态 + 动态」，用于侧边栏渲染；addRoutes 只有动态部分，用于 removeRoute
    expect(store.routes).toHaveLength(1)
    expect(store.addRoutes).toHaveLength(0)
  })

  it('resetRoutes 清空动态路由并复位已加载标记', () => {
    const store = usePermissionStore()
    store.generateRoutes([menu({ name: 'User', component: 'system/user/index' })])
    expect(store.routesLoaded).toBe(true)

    store.resetRoutes()

    // 换账号登录时必须复位：否则新会话会沿用上一个高权限会话的路由
    expect(store.routesLoaded).toBe(false)
    expect(store.routes).toHaveLength(0)
    expect(store.addRoutes).toHaveLength(0)
  })
})
