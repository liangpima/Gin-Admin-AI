import { onBeforeUnmount, getCurrentInstance } from 'vue'

/**
 * body 滚动锁：锁住 / 解锁整页滚动，并在组件卸载时**自动解锁**。
 *
 * # 为什么不是直接写 document.body.style.overflow
 *
 * 直接改 body 样式有个必然的疏漏点：解锁只写在「关闭」的处理器里，
 * 而组件还可能在不走「关闭」流程的情况下被卸载 —— 切路由、被 keep-alive
 * 之外的条件销毁、父组件 v-if 变化。那条路径上没人解锁，
 * 于是**整站滚动被永久锁死**，刷新之前不会自愈，且控制台一片安静。
 *
 * 把「锁」与「卸载时解锁」绑在同一个 hook 里，这个疏漏点就不存在了：
 * 新的锁动作天然带着配套的解锁，不需要作者记得再补一处清理。
 *
 * # 用法
 *
 * ```ts
 * const { lock, unlock } = useBodyScrollLock()
 * // 打开预览
 * lock()
 * // 关闭预览
 * unlock()
 * ```
 */
export function useBodyScrollLock() {
  function lock() {
    document.body.style.overflow = 'hidden'
  }

  function unlock() {
    // 置空而不是恢复成 'visible'：置空等于交还给 CSS 控制，
    // 而 'visible' 会覆盖样式表里可能存在的其它 overflow 设定。
    document.body.style.overflow = ''
  }

  // 不在组件上下文里（纯逻辑测试）时跳过注册，由调用方自行 unlock
  if (getCurrentInstance()) {
    onBeforeUnmount(unlock)
  }

  return { lock, unlock }
}
