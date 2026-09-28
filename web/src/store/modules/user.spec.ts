// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

/**
 * `userStore.clearSession()` 的单元测试。
 *
 * 这条路径是「主动登出」与「被 401 踢出」的公共出口，它做错事的后果都很隐蔽：
 *   - 删多了 → 常量路由（尤其是**兜底路由**）被摘掉且不会被加回来
 *     → 重新登录后访问任何未匹配路径都是**空白页**，只有整页刷新能恢复
 *   - 删少了 → 上一个高权限会话注册的路由留在 router 实例里
 *     → 换低权限账号登录后直接改 URL 就能打开那些页面
 *
 * 缺陷形态（本次修复的）：实现里用 `['Login','Dashboard','NotFound']`
 * 当白名单，而兜底路由的 name 实际是 **`NotFoundCatchAll`**
 * （见 `@/router/routes/static.ts`），两者对不上 —— 于是每次登出都会
 * `removeRoute('NotFoundCatchAll')`。它属于 `constantRoutes`，不会再被
 * `addRoute` 加回来，所以这个错误是不可自愈的。
 *
 * ⚠️ 刻意**不 mock** `@/router/routes/static`：白名单现在是从真实
 * `constantRoutes` 反推出来的，只有用真的路由表才能验证「改路由名不会让它失效」。
 * 若哪天有人把实现改回硬编码名字，这里的真实路由表会让断言立刻转红。
 */

const mocks = vi.hoisted(() => ({
  getRoutes: vi.fn(),
  removeRoute: vi.fn(),
  clearLoginFlag: vi.fn(),
  resetRoutes: vi.fn(),
}))

vi.mock('@/layout/index.vue', () => ({
  default: { name: 'Layout', render: () => null },
}))

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  getUserInfo: vi.fn(),
  logout: vi.fn(),
}))

vi.mock('@/utils/auth', () => ({
  clearLoginFlag: () => mocks.clearLoginFlag(),
}))

vi.mock('@/router', () => ({
  default: {
    getRoutes: () => mocks.getRoutes(),
    removeRoute: (name: string) => mocks.removeRoute(name),
  },
}))

vi.mock('@/store/modules/permission', () => ({
  usePermissionStore: () => ({
    resetRoutes: () => mocks.resetRoutes(),
  }),
}))

// tagsView 用**真实 store**（不 mock）：它没有 `import.meta.glob` 之类的重依赖，
// 用真的才能验证「清空」这个动作本身生效，而不是只验证「某个函数被调用过」。
import { useTagsViewStore } from './tagsView'
import { getUserInfo } from '@/api/auth'

const mockedGetUserInfo = vi.mocked(getUserInfo)

const { useUserStore } = await import('./user')

/**
 * router.getRoutes() 在「已登录并注册了动态路由」时的形状。
 *
 * 前四条是 constantRoutes（含 `/` 的**子路由** dashboard —— 它必须被保留，
 * 只取顶层会把 dashboard 误判成动态路由删掉，而它正是登录后的落地页），
 * 后两条是 addRoute 动态注册进去的业务路由。
 */
const REGISTERED_ROUTES = [
  { path: '/login', name: 'Login' },
  { path: '/dashboard', name: 'Dashboard' },
  { path: '/404', name: 'NotFound' },
  { path: '/:pathMatch(.*)*', name: 'NotFoundCatchAll' },
  { path: '/system/user', name: 'User' },
  { path: '/system/role', name: 'Role' },
]

function removedNames(): string[] {
  return mocks.removeRoute.mock.calls.map((c) => c[0] as string)
}

beforeEach(() => {
  vi.clearAllMocks()
  setActivePinia(createPinia())
  mocks.getRoutes.mockReturnValue([...REGISTERED_ROUTES])
})

describe('clearSession：只摘动态路由', () => {
  it('动态注册的业务路由被摘掉', () => {
    useUserStore().clearSession()

    expect(removedNames().sort()).toEqual(['Role', 'User'])
  })

  it('兜底路由 NotFoundCatchAll 必须保留（本次修复的缺陷）', () => {
    // 它属于 constantRoutes，被删掉后没有任何地方会把它加回来。
    // 现象：登出后再登录，访问任何未匹配路径渲染不出内容（空白页），
    // 只有整页刷新能恢复 —— 用户会以为「系统坏了」。
    useUserStore().clearSession()

    expect(removedNames()).not.toContain('NotFoundCatchAll')
  })

  it('常量路由（含子路由 Dashboard）必须保留', () => {
    // Dashboard 是 `/` 的**子路由**。判断依据若只遍历顶层，
    // 它会因为「不在顶层白名单里」被误删 —— 而它是登录后的落地页。
    useUserStore().clearSession()

    expect(removedNames()).not.toContain('Dashboard')
    expect(removedNames()).not.toContain('Login')
    expect(removedNames()).not.toContain('NotFound')
  })

  it('没有动态路由时不做任何删除', () => {
    mocks.getRoutes.mockReturnValue([...REGISTERED_ROUTES.slice(0, 4)])

    useUserStore().clearSession()

    expect(mocks.removeRoute).not.toHaveBeenCalled()
  })
})

describe('clearSession：清理状态与标记', () => {
  it('清空 userInfo / roles / buttons，并清登录态标记', () => {
    const store = useUserStore()
    store.userInfo = { roles: [], buttons: [] } as never
    store.roles = ['admin']
    store.buttons = ['system:user:add']

    store.clearSession()

    expect(store.userInfo).toBeNull()
    expect(store.roles).toEqual([])
    expect(store.buttons).toEqual([])
    expect(mocks.clearLoginFlag).toHaveBeenCalled()
  })

  it('重置 permission store（否则 routesLoaded 仍为 true，守卫不再拉菜单）', () => {
    useUserStore().clearSession()

    expect(mocks.resetRoutes).toHaveBeenCalled()
  })
})

describe('clearSession：清理标签栏', () => {
  /**
   * 标签栏（visitedViews / cachedViews）是**上一个会话**的状态。
   *
   * 不清的后果分两种，都很隐蔽：
   *   - 标签对应的是上一账号的动态路由，而那条路由已被 removeRoute 摘掉
   *     → 点标签进 404（用户以为「页面坏了」）
   *   - 标签对应的路由仍在（同权限账号）→ 页面能开，但 cachedViews 里
   *     残留的组件实例属于上一会话，`keep-alive` 会直接复用旧实例，
   *     列表页的筛选条件/分页/已加载数据全部串到新账号身上
   */
  it('visitedViews / cachedViews 都被清空', () => {
    const tagsView = useTagsViewStore()
    tagsView.visitedViews = [
      { path: '/system/user', name: 'User', title: '用户管理' },
      { path: '/system/role', name: 'Role', title: '角色管理' },
    ]
    tagsView.cachedViews = ['User', 'Role']

    useUserStore().clearSession()

    expect(tagsView.visitedViews).toEqual([])
    expect(tagsView.cachedViews).toEqual([])
  })

  it('被 401 踢出（直接调 clearSession，不经过 logout）同样清空', () => {
    // api/index.ts 的 handleLogout 走的就是这条路，它绕过 logout()
    const tagsView = useTagsViewStore()
    tagsView.visitedViews = [{ path: '/system/user', name: 'User' }]
    tagsView.cachedViews = ['User']

    useUserStore().clearSession()

    expect(tagsView.visitedViews).toEqual([])
    expect(tagsView.cachedViews).toEqual([])
  })
})

describe('getInfo：数组字段缺失不抛 TypeError', () => {
  /**
   * 后端 `vo.UserInfoResponse` 用 `make([]T, 0)` 构造，正常不会是 null。
   * 这些用例防的是**契约漂移**（网关裁剪字段 / 后端换实现 / 新增字段未同步）：
   * 一旦某个数组缺失，`roles.map` 与守卫里的 `menus` 消费点会抛 TypeError，
   * 而它被守卫的 catch 吞掉后表现为「刚登录就被踢回登录页」——
   * 一个和根因完全对不上的现象。
   */
  it('roles / buttons / menus 全部缺失 → 兜底为空数组，不抛错', async () => {
    mockedGetUserInfo.mockResolvedValue({
      code: 0,
      message: 'ok',
      data: { id: 1, username: 'admin' },
    } as never)

    const store = useUserStore()
    const info = await store.getInfo()

    expect(store.roles).toEqual([])
    expect(store.buttons).toEqual([])
    // 返回值也必须归一化：守卫直接拿 info.menus 去 generateRoutes，
    // 若原样返回 undefined，`menus.forEach` 仍会抛错，兜底等于没做
    expect(info.menus).toEqual([])
  })

  it('只有 roles 缺失时其余字段照常保留', async () => {
    mockedGetUserInfo.mockResolvedValue({
      code: 0,
      message: 'ok',
      data: { id: 7, username: 'bob', buttons: ['system:user:add'] },
    } as never)

    const store = useUserStore()
    const info = await store.getInfo()

    expect(store.roles).toEqual([])
    expect(store.buttons).toEqual(['system:user:add'])
    expect(info.id).toBe(7)
    expect(info.username).toBe('bob')
  })

  it('正常响应：roles 提取 code、buttons 原样写入', async () => {
    mockedGetUserInfo.mockResolvedValue({
      code: 0,
      message: 'ok',
      data: {
        id: 1,
        username: 'admin',
        roles: [
          { id: 1, name: '超级管理员', code: 'admin' },
          { id: 2, name: '运营', code: 'operator' },
        ],
        buttons: ['system:user:add'],
        menus: [],
      },
    } as never)

    const store = useUserStore()
    await store.getInfo()

    expect(store.roles).toEqual(['admin', 'operator'])
    expect(store.buttons).toEqual(['system:user:add'])
    expect(store.hasRole('admin')).toBe(true)
    expect(store.hasButton('system:user:add')).toBe(true)
  })
})
