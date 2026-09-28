// @vitest-environment jsdom

import { afterEach, describe, expect, it } from 'vitest'
import { createApp, defineComponent, h } from 'vue'
import type { App } from 'vue'
import { useBodyScrollLock } from './useBodyScrollLock'

/**
 * useBodyScrollLock 的单元测试。
 *
 * 守的是一个**很难复现、也不报错**的缺陷：预览弹层打开时锁了 body 滚动，
 * 但解锁只写在「关闭预览」的处理器里 —— 于是「预览开着直接切路由」
 * 这条路径上没人解锁，`body.style.overflow` 永远停在 hidden，
 * **整站滚动锁死**，刷新前不自愈，控制台也不会有任何提示。
 *
 * 因此关键用例是「卸载后自动解锁」，而不是「unlock() 能恢复」。
 */

let app: App | null = null

function mountWithLock() {
  let api: ReturnType<typeof useBodyScrollLock> | null = null

  const Comp = defineComponent({
    setup() {
      api = useBodyScrollLock()
      return () => h('div')
    },
  })

  const host = document.createElement('div')
  app = createApp(Comp)
  app.mount(host)
  return { api: api as unknown as ReturnType<typeof useBodyScrollLock>, host }
}

afterEach(() => {
  app?.unmount()
  app = null
  document.body.style.overflow = ''
})

describe('useBodyScrollLock', () => {
  it('lock 锁住 body 滚动', () => {
    const { api } = mountWithLock()

    api.lock()
    expect(document.body.style.overflow).toBe('hidden')
  })

  it('unlock 交还给 CSS 控制（置空而非 visible）', () => {
    const { api } = mountWithLock()

    api.lock()
    api.unlock()
    expect(document.body.style.overflow).toBe('')
  })

  it('组件卸载时自动解锁 —— 切路由不会把整站滚动锁死', () => {
    const { api } = mountWithLock()

    api.lock()
    expect(document.body.style.overflow).toBe('hidden')

    // 模拟「预览开着直接切走」：不调用 unlock，直接卸载
    app?.unmount()
    app = null

    expect(document.body.style.overflow).toBe('')
  })

  it('未锁定时卸载也不会留下异常状态', () => {
    mountWithLock()
    app?.unmount()
    app = null

    expect(document.body.style.overflow).toBe('')
  })

  it('在组件外调用不抛错（纯逻辑场景）', () => {
    const api = useBodyScrollLock()

    expect(() => api.lock()).not.toThrow()
    expect(document.body.style.overflow).toBe('hidden')

    api.unlock()
    expect(document.body.style.overflow).toBe('')
  })
})
