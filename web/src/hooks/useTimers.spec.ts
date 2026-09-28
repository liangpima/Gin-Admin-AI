// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h } from 'vue'
import type { App } from 'vue'
import { useTimers } from './useTimers'

/**
 * useTimers 的单元测试。
 *
 * 守的是一件**不会报错**的事：定时器在组件卸载后仍会触发。
 * 回调里写已卸载组件的 ref 是静默无效的，带着请求（验证码失败后 refresh()）
 * 则表现为「切走又切回时多一次无人接收的请求」，setInterval 更会越积越多。
 *
 * 断言方式刻意用「真的挂载 → 卸载 → 等够时间 → 看回调有没有跑」，
 * 而不是断言「clearAll 被调用过」：后者在 onBeforeUnmount 压根没注册时
 * 也能通过（只要测试自己调一次 clearAll 就行），区分不出真正的缺陷。
 */

let app: App | null = null

function mountWithTimers() {
  const spy = { late: vi.fn(), tick: vi.fn() }
  let api: ReturnType<typeof useTimers> | null = null

  const Comp = defineComponent({
    setup() {
      api = useTimers()
      return () => h('div')
    },
  })

  const host = document.createElement('div')
  app = createApp(Comp)
  app.mount(host)
  return { spy, api: api as unknown as ReturnType<typeof useTimers> }
}

afterEach(() => {
  app?.unmount()
  app = null
  vi.useRealTimers()
})

describe('useTimers', () => {
  it('卸载后 setTimeout 回调不再执行', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const { api } = mountWithTimers()

    api.later(fn, 700)
    app?.unmount()
    app = null

    vi.advanceTimersByTime(2000)
    expect(fn).not.toHaveBeenCalled()
  })

  it('卸载后 setInterval 不再持续触发', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const { api } = mountWithTimers()

    api.every(fn, 16)
    vi.advanceTimersByTime(160)
    const before = fn.mock.calls.length
    expect(before).toBeGreaterThan(0)

    app?.unmount()
    app = null
    vi.advanceTimersByTime(1600)

    expect(fn.mock.calls.length).toBe(before)
  })

  it('未卸载时回调按预期触发（反向验证：不是把定时器整个禁掉了）', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const { api } = mountWithTimers()

    api.later(fn, 700)
    vi.advanceTimersByTime(699)
    expect(fn).not.toHaveBeenCalled()

    vi.advanceTimersByTime(2)
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('clearAll 只清一次，重复调用不报错且句柄被清空', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const { api } = mountWithTimers()

    api.later(fn, 100)
    api.clearAll()
    api.clearAll()

    vi.advanceTimersByTime(1000)
    expect(fn).not.toHaveBeenCalled()
  })

  it('在组件外调用不抛错（纯逻辑测试场景）', () => {
    vi.useFakeTimers()
    const fn = vi.fn()

    // 不在 setup 上下文中：getCurrentInstance() 为 null，
    // 这里不能抛「onBeforeUnmount is called when there is no active component」
    const api = useTimers()
    expect(() => api.later(fn, 10)).not.toThrow()

    vi.advanceTimersByTime(20)
    expect(fn).toHaveBeenCalledTimes(1)
  })
})
