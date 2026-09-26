import { describe, expect, it } from 'vitest'
import { cardRowsOf, columnKeyOf, displayValue, rowKeyOf, treeAttrsOf } from './logic'
import type { ResponsiveColumn } from './types'

/**
 * ResponsiveTable 纯逻辑的单元测试。
 *
 * 这些分支此前没有任何用例守着，而它们坏掉的后果都**只在手机端可见**
 * （卡片少一行、层级丢了、操作区不见了），桌面端一切正常 —— 最难发现的一类问题。
 * 其中最要紧的是 `treeAttrsOf`：漏传 `row-key` 会让树形表整棵塌掉，
 * 实测部门页 3 行→1 行、菜单页 67 行→4 行，而「表格在、表头在」的断言照样全绿。
 */

interface Row {
  id?: number
  name?: string
  sort?: number
  children?: Row[]
}

describe('rowKeyOf', () => {
  it('取到主键时转成字符串', () => {
    expect(rowKeyOf<Row>({ id: 7 }, 'id', 0)).toBe('7')
  })

  it('主键缺失/为 null 时退回下标，保证 key 稳定且唯一', () => {
    expect(rowKeyOf<Row>({}, 'id', 3)).toBe('idx-3')
    expect(rowKeyOf<Row>({ id: undefined }, 'id', 4)).toBe('idx-4')
    expect(rowKeyOf<Row>({ id: null as unknown as number }, 'id', 5)).toBe('idx-5')
  })
})

describe('cardRowsOf', () => {
  const opts = { tree: false, treeChildren: 'children', rowKey: 'id' }

  it('平表：原样映射，层级都是 0', () => {
    const rows = cardRowsOf<Row>([{ id: 1 }, { id: 2 }], opts)
    expect(rows.map((r) => r.level)).toEqual([0, 0])
    expect(rows.map((r) => r.key)).toEqual(['1', '2'])
  })

  it('树形：按深度优先摊平，子节点一个都不能少', () => {
    const data: Row[] = [
      { id: 1, children: [{ id: 2, children: [{ id: 3 }] }, { id: 4 }] },
      { id: 5 },
    ]
    const rows = cardRowsOf<Row>(data, { ...opts, tree: true })
    // 顺序必须是 1 → 2 → 3 → 4 → 5（深度优先），层级依次 0/1/2/1/0
    expect(rows.map((r) => (r.row as Row).id)).toEqual([1, 2, 3, 4, 5])
    expect(rows.map((r) => r.level)).toEqual([0, 1, 2, 1, 0])
  })

  it('树形：key 不重复（层级前缀是为了让不同层级下的同 id 也能区分）', () => {
    const data: Row[] = [{ id: 1, children: [{ id: 1 }] }]
    const keys = cardRowsOf<Row>(data, { ...opts, tree: true }).map((r) => r.key)
    expect(new Set(keys).size).toBe(keys.length)
  })

  it('树形：children 为空数组或非数组时都不展开', () => {
    const data = [
      { id: 1, children: [] },
      { id: 2, children: undefined },
      { id: 3, children: 'nope' as unknown as Row[] },
    ]
    expect(cardRowsOf<Row>(data, { ...opts, tree: true })).toHaveLength(3)
  })

  it('树形：不改动传入的数组（摊平结果写在本地数组里）', () => {
    const data: Row[] = [{ id: 1, children: [{ id: 2 }] }]
    cardRowsOf<Row>(data, { ...opts, tree: true })
    expect(data).toHaveLength(1)
  })
})

describe('columnKeyOf', () => {
  it('prop 优先，其次 slot', () => {
    expect(columnKeyOf<Row>({ label: '名称', prop: 'name' }, 0)).toBe('name')
    expect(columnKeyOf<Row>({ label: '状态', slot: 'status' }, 1)).toBe('status')
  })

  it('prop 与 slot 都缺时用 label + 下标 —— 下标必须真的参与，否则同名列会撞 key', () => {
    const a = columnKeyOf<Row>({ label: '备注' }, 0)
    const b = columnKeyOf<Row>({ label: '备注' }, 1)
    expect(a).not.toBe(b)
  })
})

describe('displayValue', () => {
  const col = (c: Partial<ResponsiveColumn<Row>>): ResponsiveColumn<Row> => ({
    label: 'x',
    ...c,
  })

  it('直接取 prop 的值', () => {
    expect(displayValue<Row>({ name: '张三' }, col({ prop: 'name' }))).toBe('张三')
  })

  it('0 与 false 是合法值，不能被当成空', () => {
    expect(displayValue<Row>({ sort: 0 }, col({ prop: 'sort' }))).toBe('0')
    expect(displayValue<Row>({ sort: 0 }, col({ prop: 'sort' }))).not.toBe('-')
  })

  it('undefined / null / 空串都换成占位符', () => {
    expect(displayValue<Row>({}, col({ prop: 'name' }))).toBe('-')
    expect(displayValue<Row>({ name: null as unknown as string }, col({ prop: 'name' }))).toBe('-')
    expect(displayValue<Row>({ name: '' }, col({ prop: 'name' }))).toBe('-')
  })

  it('formatter 优先于 prop', () => {
    const c = col({ prop: 'sort', formatter: (r) => `#${r.sort}` })
    expect(displayValue<Row>({ sort: 3 }, c)).toBe('#3')
  })

  it('formatter 返回空串时**也**给占位符（两条分支行为必须一致）', () => {
    const c = col({ prop: 'name', formatter: () => '' })
    expect(displayValue<Row>({ name: '张三' }, c)).toBe('-')
  })

  it('占位符可覆盖', () => {
    expect(displayValue<Row>({}, col({ prop: 'name' }), '—')).toBe('—')
  })
})

describe('treeAttrsOf', () => {
  it('⚠️ 树形时必须带上 rowKey —— 漏了 element-plus 的 tree store 会返回空，整棵树塌掉', () => {
    const attrs = treeAttrsOf({ tree: true, rowKey: 'id' })
    expect(attrs).toHaveProperty('rowKey', 'id')
  })

  it('树形时不传 defaultExpandAll 就跟随 tree（默认全展开）', () => {
    expect(treeAttrsOf({ tree: true, rowKey: 'id' })).toEqual({
      rowKey: 'id',
      defaultExpandAll: true,
    })
  })

  it('树形时显式传 false 可改为默认折叠', () => {
    expect(treeAttrsOf({ tree: true, rowKey: 'id', defaultExpandAll: false })).toEqual({
      rowKey: 'id',
      defaultExpandAll: false,
    })
  })

  it('非树形时一个都不传（row-key 会让 el-table 走 tree store，对平表是没必要的变更）', () => {
    expect(treeAttrsOf({ tree: false, rowKey: 'id' })).toEqual({})
  })
})
