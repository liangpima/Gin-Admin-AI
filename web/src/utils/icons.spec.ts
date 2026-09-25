import * as AllIcons from '@element-plus/icons-vue'
import { describe, expect, it } from 'vitest'
import { appIcons, MENU_ICON_NAMES, registerAppIcons } from './icons'

/**
 * 图标白名单的守护用例。
 *
 * 为什么值得单独一个文件：本项目有**多处按字符串名解析图标**的站点
 * （侧边栏、菜单管理页、仪表盘卡片、用户页操作菜单、登录页 `prefix-icon`），
 * 它们的共同失效方式是**静默**的 —— `<component :is="不存在的名字">` 不报错，
 * 只是那个位置什么都没有。侧边栏少一个图标、控制台一片安静，没人会发现。
 *
 * 所以这里不去逐个列举「我觉得用到了哪些」，而是**扫描全仓源码**把站点里的
 * 图标名都抽出来，逐个断言在白名单里。将来任何人加了新图标却忘了更新白名单，
 * 这里会直接红。
 *
 * 用 vite 的 `import.meta.glob(..., { query: '?raw' })` 读源码而不是 node 的 `fs`：
 * 本项目没装 `@types/node`（tsconfig 的 lib 只有 ES2020 + DOM），
 * 用 fs 会让 `vue-tsc` 在 build 阶段报 `Cannot find module 'node:fs'`。
 */

const rawSources = import.meta.glob('../**/*.{vue,ts}', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

/** 跳过测试自身与生成物（`*.d.ts` 由 unplugin 维护，不是人手写的引用） */
const sources = Object.entries(rawSources)
  .filter(([path]) => !path.endsWith('.spec.ts') && !path.endsWith('.d.ts'))
  .map(([path, text]) => ({ path: path.replace(/^\.\.\//, ''), text }))

/** 图标包导出的全部名字（293 个），用来把「组件标签」与「图标名」区分开 */
const ALL_ICON_NAMES = new Set(Object.keys(AllIcons))

/** 模板里当标签用：`<Check />`、`<Refresh>` */
function tagUsages(): Map<string, string[]> {
  const found = new Map<string, string[]>()
  const re = /<\s*([A-Z][A-Za-z0-9]*)(?=[\s/>])/g
  for (const { path, text } of sources) {
    for (const m of text.matchAll(re)) {
      if (!ALL_ICON_NAMES.has(m[1])) continue
      found.set(m[1], [...(found.get(m[1]) || []), path])
    }
  }
  return found
}

/** 当字符串传：`icon: 'Key'`、`prefix-icon="User"`、`:is="'Plus'"` */
function stringUsages(): Map<string, string[]> {
  const found = new Map<string, string[]>()
  const patterns = [
    /icon\s*:\s*'([A-Z][A-Za-z0-9]*)'/g, // 对象字面量里的 icon: 'Key'
    /icon\s*=\s*"([A-Z][A-Za-z0-9]*)"/g, // prefix-icon="User" / suffix-icon / icon="X"
    /:is\s*=\s*"'([A-Z][A-Za-z0-9]*)'"/g, // 内联字面量的 :is
  ]
  for (const { path, text } of sources) {
    for (const re of patterns) {
      for (const m of text.matchAll(re)) {
        if (!ALL_ICON_NAMES.has(m[1])) continue
        found.set(m[1], [...(found.get(m[1]) || []), path])
      }
    }
  }
  return found
}

const tagUsed = tagUsages()
const stringUsed = stringUsages()

/** 把「名字 → 出现位置」渲染成便于定位的失败信息 */
function where(found: Map<string, string[]>, name: string): string {
  return [...new Set(found.get(name))].join(', ')
}

describe('图标白名单：覆盖性', () => {
  it('模板里当标签用的图标都在白名单里', () => {
    const missing = [...tagUsed.keys()].filter((name) => !(name in appIcons))
    expect(
      missing,
      `这些图标在模板里以 <Xxx /> 形式使用但不在 appIcons 中：\n` +
        missing.map((n) => `  ${n}  ← ${where(tagUsed, n)}`).join('\n'),
    ).toEqual([])
  })

  it('当字符串传的图标都在白名单里（这些站点失效时是静默的）', () => {
    const missing = [...stringUsed.keys()].filter((name) => !(name in appIcons))
    expect(
      missing,
      `这些图标以字符串形式引用但不在 appIcons 中：\n` +
        missing.map((n) => `  ${n}  ← ${where(stringUsed, n)}`).join('\n'),
    ).toEqual([])
  })

  it('数据库 sys_menu.icon 在用的图标都在白名单里', () => {
    // 这一条是**最重要**的：这些名字的来源不在代码里，grep 找不到它们。
    // 往菜单表加了新图标却忘了更新白名单，侧边栏那个位置会静默变空白
    const missing = MENU_ICON_NAMES.filter((name) => !(name in appIcons))
    expect(missing, `数据库菜单在用的图标不在白名单里：${missing.join(', ')}`).toEqual([])
  })
})

describe('图标白名单：精简性', () => {
  it('白名单里每一项都是有效组件（不是 undefined）', () => {
    // 覆盖性用例查的是「键在不在」（`name in appIcons`）。若有人删掉了上面的
    // `import { X } from '@element-plus/icons-vue'` 却留着对象里的 `X,` 键，
    // 键仍然存在、值为 undefined —— 覆盖性用例会照样通过，
    // 而 Vue 注册 undefined 组件是**静默不渲染**，又是一个「没报错但图标不见了」。
    // 所以键存在性之外还要单独验一次值。
    const invalid = Object.entries(appIcons)
      .filter(([, component]) => !component)
      .map(([name]) => name)

    expect(
      invalid,
      `这些图标在白名单里但值是 undefined（多半是 import 被删了）：${invalid.join(', ')}`,
    ).toEqual([])
  })

  it('白名单里没有用不到的图标（避免它只增不减）', () => {
    // 反向约束同样重要：如果只断言「用到的都在表里」，清单会一路膨胀回
    // 「近似全量注册」，这次改造的收益就慢慢漏回去了
    const referenced = new Set([...tagUsed.keys(), ...stringUsed.keys(), ...MENU_ICON_NAMES])
    const dead = Object.keys(appIcons).filter((name) => !referenced.has(name))

    expect(dead, `这些图标在白名单里但全仓没有任何引用，应当删掉：${dead.join(', ')}`).toEqual([])
  })

  it('白名单远小于图标包总量（守住改造收益）', () => {
    // 293 → 39 是本次改造的核心收益。若哪天有人图省事把清单补回全量，
    // 这条会红 —— 而不是让主包悄悄涨回 270 kB 而无人察觉
    expect(Object.keys(appIcons).length).toBeLessThan(80)
  })
})

describe('图标白名单：注册函数', () => {
  it('registerAppIcons 把每个白名单图标都注册成全局组件', () => {
    const registered: Record<string, unknown> = {}
    // 只提供 registerAppIcons 真正会用到的那个方法，形状与 Vue 的 app 一致
    registerAppIcons({
      component: (name: string, component: unknown) => {
        registered[name] = component
      },
    })

    expect(Object.keys(registered).sort()).toEqual(Object.keys(appIcons).sort())
    // 注册的必须是真组件对象而不是 undefined —— import 名字写错时会是后者，
    // 而 Vue 对 undefined 组件是静默不渲染，又是一个「没报错但图标不见了」
    for (const [name, component] of Object.entries(registered)) {
      expect(component, `${name} 注册的不是有效组件`).toBeTruthy()
    }
  })
})
