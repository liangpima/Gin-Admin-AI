import { describe, expect, it } from 'vitest'
import { menuIndex, resolvePath } from './sidebarLogic'

/**
 * 侧边栏菜单 index 的回归测试。
 *
 * 缺陷形态（两条都不报错，只在界面上看得见）：
 *   ① 单可见子节点时 index 用的是**父路径**，而 `el-menu` 的 `default-active`
 *      是 `route.path` —— 两者永不相等，**「首页」永远不高亮**；
 *   ② 点该菜单会 push 到父路径。`/` 有 `redirect: '/dashboard'` 兜住，
 *      但没有 redirect 的单子节点动态父路由会渲染一个空的 Layout。
 *
 * 首页正是 ① 的现场：`constantRoutes` 里 `/`（Layout）下挂着 `dashboard`，
 * Sidebar.vue 传给顶层菜单的 basePath 是 `menuRoute.path` = `'/'`。
 */

describe('resolvePath', () => {
  it('basePath 为 "/" 时不产生双斜杠', () => {
    // 直接拼 `${basePath}/${childPath}` 会得到 '//dashboard'，
    // 它与 route.path 不相等 —— 这正是首页不高亮的直接原因
    expect(resolvePath('/', 'dashboard')).toBe('/dashboard')
  })

  it('普通父路径拼接', () => {
    expect(resolvePath('/system', 'user')).toBe('/system/user')
  })

  it('basePath 末尾多余的斜杠被吃掉', () => {
    expect(resolvePath('/system/', 'user')).toBe('/system/user')
    expect(resolvePath('/system///', 'user')).toBe('/system/user')
  })

  it('子路径是绝对路径时原样返回（菜单表里的 path 可能是完整的）', () => {
    expect(resolvePath('/system', '/member/list')).toBe('/member/list')
    expect(resolvePath('/', '/dashboard')).toBe('/dashboard')
  })

  it('basePath 为空 → 补一个前导斜杠', () => {
    expect(resolvePath('', 'dashboard')).toBe('/dashboard')
  })

  it('子路径为空 → 退回 basePath（不拼出多余的斜杠）', () => {
    expect(resolvePath('/system', '')).toBe('/system')
    expect(resolvePath('', '')).toBe('')
  })
})

describe('menuIndex', () => {
  it('单个可见子节点 → 用子路径，不是父路径', () => {
    // 首页：basePath '/', item.path '/', 唯一子节点 'dashboard'
    expect(menuIndex('/', '/', ['dashboard'])).toBe('/dashboard')
  })

  it('单个可见子节点的绝对路径子路由', () => {
    expect(menuIndex('/settings', '/settings', ['/settings/sms'])).toBe('/settings/sms')
  })

  it('多个可见子节点 → 用父路径（此时渲染的是 el-sub-menu，index 只需唯一）', () => {
    expect(menuIndex('/system', '/system', ['user', 'role'])).toBe('/system')
  })

  it('没有可见子节点 → 退回父路径，且不抛错', () => {
    expect(menuIndex('/system', '/system', [])).toBe('/system')
  })

  it('basePath 为空时退回 item.path', () => {
    expect(menuIndex('', '/system', ['user', 'role'])).toBe('/system')
  })
})
