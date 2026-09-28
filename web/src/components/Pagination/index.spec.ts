// @vitest-environment jsdom

/* eslint-disable vue/one-component-per-file --
   本文件是测试，需要同时定义「el-pagination 的 stub」与「被测组件的挂载入口」
   两个组件创建表达式。这条规则针对的是「一个文件里放了两个业务组件」，
   在测试文件里没有意义（vitest.config.ts 已排除 Components 插件，
   组件必须靠 stub 顶掉，否则要把整个 Element Plus 拉进测试）。 */

import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createApp, defineComponent, h, nextTick } from 'vue'
import type { App } from 'vue'
import Pagination from './index.vue'

/**
 * 分页组件的单元测试。
 *
 * 守住两件都**不会报错**、只是「看起来不对劲」的事：
 *
 * 1. 手机端页码数量。旧实现把 `window.innerWidth < 992 ? 5 : 7` 写成
 *    `withDefaults` 的默认值 —— 默认值只在**模块求值时算一次**，之后窗口怎么
 *    缩放都不会重算，于是手机端仍渲染 7 个页码、横向挤不下。
 *    断言方式是「改变窗口宽度后组件收到的 pagerCount 必须跟着变」：
 *    只断言初始值是区分不出「响应式」与「算一次」的。
 *
 * 2. 切换每页条数要回到第一页。否则从第 5 页切到每页 100 条时，
 *    第 5 页早已超出总页数，接口返回空列表 —— 用户看到「数据全没了」。
 *
 * ⚠️ 本文件不引入 Element Plus：`vitest.config.ts` 刻意不带 Components 插件，
 * 模板里的 `<el-pagination>` 会走 `resolveComponent`，因此可以用一个全局
 * stub 顶掉它 —— 这样既能拿到组件传下去的 props，又不用把整个 EP 拉进测试。
 */

/** 每次 stub 渲染时记录的 pagerCount（响应式变化会触发重渲染） */
let renderedPagerCounts: number[] = []
/** stub 触发事件用的回调（在渲染时刷新，保证指向当前实例） */
let triggerSizeChange: ((v: number) => void) | undefined

const ElPaginationStub = defineComponent({
  name: 'ElPagination',
  props: {
    currentPage: Number,
    pageSize: Number,
    pagerCount: Number,
    total: Number,
    pageSizes: Array,
    layout: String,
    background: Boolean,
  },
  emits: ['size-change', 'current-change', 'update:currentPage', 'update:pageSize'],
  setup(props, { emit }) {
    return () => {
      // 记录本次渲染拿到的值：旧实现永远是同一个数，新实现会随宽度变化
      if (typeof props.pagerCount === 'number') {
        renderedPagerCounts.push(props.pagerCount)
      }
      triggerSizeChange = (v: number) => emit('size-change', v)
      return h('div', { class: 'el-pagination-stub' })
    }
  },
})

interface EmittedEvent {
  event: string
  payload: unknown
}

let mountedApps: App[] = []

function mountPagination(props: Record<string, unknown> = {}) {
  const emitted: EmittedEvent[] = []
  const host = document.createElement('div')
  document.body.appendChild(host)

  // 直接把被测组件当根组件挂载（第二个参数就是它的 props），
  // 不再额外定义一个宿主组件 —— 那会触发 vue/one-component-per-file。
  const app = createApp(Pagination, {
    total: 100,
    page: 1,
    limit: 10,
    ...props,
    onPagination: (payload: unknown) => emitted.push({ event: 'pagination', payload }),
    'onUpdate:page': (v: unknown) => emitted.push({ event: 'update:page', payload: v }),
  })

  // 注册成 PascalCase：lint 的 vue/component-definition-name-casing 不接受 kebab。
  // Vue 的 resolveAsset 会在 `assets[name]` 未命中时依次尝试 camelize /
  // capitalize(camelize)，因此模板里的 `<el-pagination>` 依然能解析到这里。
  app.component('ElPagination', ElPaginationStub)
  app.mount(host)
  mountedApps.push(app)

  return { app, emitted }
}

/** 最近一次渲染收到的 pagerCount */
function lastPagerCount(): number | undefined {
  return renderedPagerCounts[renderedPagerCounts.length - 1]
}

/** 模拟窗口尺寸变化并等一次重渲染 */
async function resizeTo(width: number) {
  window.innerWidth = width
  window.dispatchEvent(new Event('resize'))
  await nextTick()
  await nextTick()
}

beforeEach(() => {
  renderedPagerCounts = []
  triggerSizeChange = undefined
  mountedApps = []
  // jsdom 默认 innerWidth 是 1024（桌面），显式设一次避免用例间互相影响
  window.innerWidth = 1024
})

afterEach(() => {
  mountedApps.forEach((app) => app.unmount())
  document.body.innerHTML = ''
})

describe('页码数量随窗口宽度变化', () => {
  it('桌面宽度渲染 7 个页码', async () => {
    mountPagination()
    await nextTick()

    expect(lastPagerCount()).toBe(7)
  })

  it('缩到手机宽度后页码数量跟着变（旧实现在模块求值时算一次，永远不变）', async () => {
    mountPagination()
    await nextTick()
    expect(lastPagerCount()).toBe(7)

    await resizeTo(375)

    expect(lastPagerCount()).toBe(5)
  })

  it('再从手机放大回桌面，仍能跟随变化', async () => {
    await resizeTo(375)
    mountPagination()
    await nextTick()
    expect(lastPagerCount()).toBe(5)

    await resizeTo(1280)

    expect(lastPagerCount()).toBe(7)
  })

  it('平板宽度（768–1024）按桌面处理，与 768 断点口径一致', async () => {
    // 旧实现用的是 992：在 768–992 这段会给出 5，与 @include mobile(768)
    // 的口径不一致，同一个宽度下「样式是手机、分页是桌面」。
    await resizeTo(900)
    mountPagination()
    await nextTick()

    expect(lastPagerCount()).toBe(7)
  })

  it('显式传入 pagerCount 时以传入值为准，不被宽度覆盖', async () => {
    mountPagination({ pagerCount: 9 })
    await nextTick()
    expect(lastPagerCount()).toBe(9)

    await resizeTo(375)

    expect(lastPagerCount()).toBe(9)
  })
})

describe('切换每页条数必须回到第一页', () => {
  it('size-change 发出 page=1', async () => {
    const { emitted } = mountPagination({ page: 5, limit: 10 })
    await nextTick()

    triggerSizeChange!(100)
    await nextTick()

    const pagination = emitted.find((e) => e.event === 'pagination')
    expect(pagination, '应发出 pagination 事件触发重新加载').toBeDefined()
    expect(pagination!.payload).toEqual({ page: 1, limit: 100 })

    // 页码也要同步复位，否则父组件的 v-model:page 仍停在 5
    expect(emitted.find((e) => e.event === 'update:page')?.payload).toBe(1)
  })
})
