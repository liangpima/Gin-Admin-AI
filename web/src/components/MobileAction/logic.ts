/**
 * MobileAction 的 action 解析逻辑（纯函数，便于单测）。
 *
 * 为什么要单独拆出来：这段判定要同时决定三件事 —— `v-for` 的 key、
 * 桌面按钮的点击分发、移动端下拉的 command。三者若各写各的，就会漂移；
 * 早前它们都用 `action.label`，于是「同名标签」既产生重复 key，
 * 又无法区分动作。
 */

export interface MobileActionItem {
  label: string
  /**
   * 动作标识。**只有出现同名标签时才需要传**：
   * 默认回退到 label，「编辑/删除」这类唯一标签不必写。
   */
  command?: string
  /** 全局注册的图标组件名（须在 utils/icons.ts 的 appIcons 白名单里） */
  icon: string
  /** 语义色：桌面按钮的 type 与下拉图标的颜色都用它 */
  type?: 'primary' | 'success' | 'warning' | 'danger' | 'info'
}

/**
 * 取 action 的稳定标识，同时用作列表 key 与下拉分发的 command。
 *
 * 为什么不能直接用 label：同一组 actions 里出现两个同名标签时
 * （例如按状态给出「启用」与「禁用」之外的「查看」×2，或未来某个动态生成的
 * 列表），`v-for :key` 会撞成重复 key，Vue 只打一条开发期警告，
 * 运行时表现为列表项复用错乱；而 command 相同则意味着点哪个都走同一个分支，
 * 其中一条功能直接失效 —— 两者都不报错。
 *
 * `command` 是**可选**的：现有调用方都不传，行为与改动前一致（回退到 label）；
 * 真的有同名标签时再显式指定，不必为了类型好看把 4 个页面全改一遍。
 */
export function actionKey(action: MobileActionItem): string {
  return action.command || action.label
}

/**
 * 校验一组 actions 的标识是否互不重复。
 *
 * 供单测与未来的开发期断言使用：返回重复的标识列表，空数组表示无冲突。
 * 不抛错、不打印 —— 决定怎么处理是调用方的事。
 */
export function findDuplicateActionKeys(actions: MobileActionItem[]): string[] {
  const seen = new Set<string>()
  const dup: string[] = []
  for (const a of actions) {
    const key = actionKey(a)
    if (seen.has(key)) {
      if (!dup.includes(key)) dup.push(key)
      continue
    }
    seen.add(key)
  }
  return dup
}
