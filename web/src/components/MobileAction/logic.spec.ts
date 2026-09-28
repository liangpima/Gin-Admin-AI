import { describe, expect, it } from 'vitest'
import { actionKey, findDuplicateActionKeys, type MobileActionItem } from './logic'

function act(label: string, command?: string): MobileActionItem {
  return { label, command, icon: 'Edit' }
}

describe('MobileAction actionKey', () => {
  it('未指定 command 时回退到 label（现有 4 个页面都不传，行为必须不变）', () => {
    expect(actionKey(act('编辑'))).toBe('编辑')
  })

  it('指定 command 时优先用它', () => {
    expect(actionKey(act('查看', 'view-detail'))).toBe('view-detail')
  })

  it('空串 command 视为未指定，回退 label', () => {
    // `||` 而不是 `??`：空串当标识会让所有 action 撞成同一个 key，
    // 比「没传」更糟，所以这里刻意把空串也归到「未指定」。
    expect(actionKey(act('删除', ''))).toBe('删除')
  })

  it('同名标签可借 command 区分 —— 这正是修复前的失效场景', () => {
    // 修复前 key 与 command 都直接用 label：这两项会撞成同一个标识，
    // v-for 报重复 key，且点哪一个都走同一个分支（其中一条功能静默失效）。
    const dupLabel = [act('查看', 'view-order'), act('查看', 'view-user')]

    expect(actionKey(dupLabel[0])).not.toBe(actionKey(dupLabel[1]))
    expect(findDuplicateActionKeys(dupLabel)).toEqual([])
  })

  it('未加 command 的同名标签会被 findDuplicateActionKeys 指出', () => {
    const conflict = [act('查看'), act('查看')]

    expect(findDuplicateActionKeys(conflict)).toEqual(['查看'])
  })

  it('findDuplicateActionKeys 只报一次重复项并保持顺序', () => {
    const list = [act('A'), act('B'), act('A'), act('A'), act('B')]

    expect(findDuplicateActionKeys(list)).toEqual(['A', 'B'])
  })

  it('无重复时返回空数组', () => {
    expect(findDuplicateActionKeys([act('编辑'), act('删除')])).toEqual([])
  })
})
