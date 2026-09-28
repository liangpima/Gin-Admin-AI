import { beforeEach, describe, expect, it } from 'vitest'
import { h } from 'vue'
import { resetCacheNameWrappers, wrapForCacheName } from './keepAlive'

/**
 * keep-alive 缓存名的回归测试（H9）。
 *
 * 缺陷形态：`<keep-alive :include="cachedViews">` 的 include 按**组件 name**
 * 匹配，而 `cachedViews` 存的是**路由 name**。本项目所有视图文件都叫
 * `index.vue`（组件名恒为 `index`），全仓也没有 `defineOptions({ name })` ——
 * 于是 include 永远匹配不上，缓存静默失效（无报错，只是每次切路由都重新挂载）。
 *
 * 这里锁定的是「包装后的组件名 == 路由 name」以及**包装对象的引用稳定性**：
 * 后者同样关键 —— 若每次渲染都新建一个字面量组件，Vue 会认为组件类型变了，
 * 销毁并重建实例，缓存照样保不住。
 */

const FakeView = { name: 'index', render: () => h('div', 'view') }

beforeEach(() => {
  resetCacheNameWrappers()
})

describe('wrapForCacheName', () => {
  it('包装后的组件名等于路由 name（include 才可能匹配上）', () => {
    const wrapped = wrapForCacheName(FakeView, 'SystemUser') as { name: string }

    expect(wrapped).not.toBe(FakeView)
    expect(wrapped.name).toBe('SystemUser')
  })

  it('同一个 name + 同一个组件 → 返回**同一个对象**', () => {
    // 这条是缓存能否生效的关键：Vue 用组件对象的引用判断「是不是同一个组件」，
    // 每次渲染都新建字面量会导致实例被销毁重建，keep-alive 形同虚设
    const first = wrapForCacheName(FakeView, 'SystemUser')
    const second = wrapForCacheName(FakeView, 'SystemUser')

    expect(second).toBe(first)
  })

  it('不同 name → 不同包装（否则两个页面会共用同一份缓存）', () => {
    const a = wrapForCacheName(FakeView, 'SystemUser')
    const b = wrapForCacheName(FakeView, 'SystemRole')

    expect(a).not.toBe(b)
    expect((a as { name: string }).name).toBe('SystemUser')
    expect((b as { name: string }).name).toBe('SystemRole')
  })

  it('同一 name 但组件换了 → 换新包装（不能渲染到已废弃的组件上）', () => {
    const other = { name: 'index', render: () => h('div', 'other') }

    const first = wrapForCacheName(FakeView, 'SystemUser')
    const second = wrapForCacheName(other, 'SystemUser')

    expect(second).not.toBe(first)
  })

  it('渲染包装 = 渲染原组件', () => {
    const wrapped = wrapForCacheName(FakeView, 'SystemUser') as {
      render: () => { type: unknown }
    }

    expect(wrapped.render().type).toBe(FakeView)
  })

  it('路由没有 name → 原样透传，不包装', () => {
    // 无名路由无法参与 include 匹配；透传即可，行为与修复前一致，不会更糟
    expect(wrapForCacheName(FakeView, undefined)).toBe(FakeView)
    expect(wrapForCacheName(FakeView, '')).toBe(FakeView)
    expect(wrapForCacheName(FakeView, null)).toBe(FakeView)
  })

  it('组件为空（路由还没匹配上）→ 原样返回，不抛错', () => {
    expect(wrapForCacheName(undefined, 'SystemUser')).toBeUndefined()
    expect(wrapForCacheName(null, 'SystemUser')).toBeNull()
  })
})
