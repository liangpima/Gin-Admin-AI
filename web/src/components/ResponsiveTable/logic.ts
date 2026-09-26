/**
 * ResponsiveTable 的纯逻辑。
 *
 * 为什么单独成文件：这些函数是组件里唯一「有分支、会算错」的部分
 * （树形摊平、key 生成、取值占位），但它们原本埋在 `<script setup>` 里，
 * 而 `<script setup>` 的东西不能 `export`、也就没法单测 ——
 * 项目里又没装 `@vue/test-utils`（只有 jsdom + vitest，用例都是纯逻辑风格）。
 * 抽出来既不引入新依赖，又让这些分支真的被测到。
 *
 * ⚠️ `treeAttrsOf` 尤其重要：**漏传 `row-key` 会让整棵树塌掉**，
 * 而这类漏传在页面里完全看不出来（表头在、表格在，就是子节点一行不显示）。
 * 实测漏传的后果：部门页 3 行 → 1 行、菜单页 67 行 → 4 行。
 */
import type { ResponsiveColumn } from './types'

/** 卡片要渲染的一行；树形时带层级，用于左侧缩进 */
export interface CardRow<T> {
  row: T
  level: number
  key: string
}

/** 取行主键的字符串形式；缺主键时退回下标，保证 key 稳定且唯一 */
export function rowKeyOf<T>(row: T, rowKey: string, index: number): string {
  const v = (row as Record<string, unknown>)[rowKey]
  return v === undefined || v === null ? `idx-${index}` : String(v)
}

/**
 * 卡片列表要渲染的行。
 *
 * 树形数据**必须按深度优先摊平**：表格里子节点靠展开箭头呈现，
 * 而卡片的 `v-for` 只遍历顶层数组 —— 不摊平的话子节点会整个消失，
 * 桌面端却一切正常。
 */
export function cardRowsOf<T>(
  data: T[],
  opts: { tree: boolean; treeChildren: string; rowKey: string },
): CardRow<T>[] {
  if (!opts.tree) {
    return data.map((row, i) => ({ row, level: 0, key: rowKeyOf(row, opts.rowKey, i) }))
  }
  const out: CardRow<T>[] = []
  const walk = (rows: T[], level: number) => {
    rows.forEach((row, i) => {
      out.push({ row, level, key: `${level}-${rowKeyOf(row, opts.rowKey, i)}` })
      const children = (row as Record<string, unknown>)[opts.treeChildren]
      if (Array.isArray(children) && children.length) {
        walk(children as T[], level + 1)
      }
    })
  }
  walk(data, 0)
  return out
}

/**
 * 列在 `v-for` 里的 key。
 *
 * `index` 是**必传**的：`prop` 与 `slot` 都可能缺失（纯展示列），
 * label 也可能重复 —— 那时只能靠下标兜底。原先的写法调用处没传 index，
 * 兜底值恒为 `` `${label}-0` ``，index 参数是死的，两列同名就会撞 key。
 */
export function columnKeyOf<T>(col: ResponsiveColumn<T>, index: number): string {
  return col.prop ?? col.slot ?? `${col.label}-${index}`
}

/**
 * 卡片里某一列的取值文案。
 *
 * 空值（`undefined` / `null` / 空串）统一给占位符，**formatter 分支也一样** ——
 * 原先只给非 formatter 分支补占位符，两条分支行为不一致。
 */
export function displayValue<T>(row: T, col: ResponsiveColumn<T>, placeholder = '-'): string {
  const v = col.formatter
    ? col.formatter(row)
    : col.prop
      ? (row as Record<string, unknown>)[col.prop]
      : ''
  return v === undefined || v === null || v === '' ? placeholder : String(v)
}

/**
 * 树形表要透传给 `el-table` 的属性。
 *
 * element-plus 的 `store/tree.mjs` 第一句是
 * `if (!watcherData.rowKey.value) return {}` —— 没有 `row-key`，树形归一化结果为空，
 * 表格按平表渲染，**子节点一行都不显示**（不是「层级变平」而是「数据看不见」）。
 *
 * 非树形时返回空对象：`row-key` 会让 el-table 启用行 key 归一化（走 tree store），
 * 对平表是没必要的既有行为变更。
 *
 * `defaultExpandAll` 不传时跟随 `tree`（树形表默认全展开，与改造前
 * dept/menu 的 `default-expand-all` 一致）；显式传 false 可改为折叠。
 */
export function treeAttrsOf(opts: {
  tree: boolean
  rowKey: string
  defaultExpandAll?: boolean
}): Record<string, unknown> {
  if (!opts.tree) return {}
  return { rowKey: opts.rowKey, defaultExpandAll: opts.defaultExpandAll ?? true }
}
