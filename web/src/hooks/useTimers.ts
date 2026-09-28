import { onBeforeUnmount, getCurrentInstance } from 'vue'

/**
 * 组件内的延时任务登记表：注册进来的 setTimeout / setInterval
 * 会在组件卸载时统一清掉。
 *
 * # 为什么需要它
 *
 * 组件里安排的延时任务，回调常常落在「用户可能已经离开」的时间点上 ——
 * 验证码成功后 0.7s 关闭弹窗、失败后 1.2s 重新拉图、看板统计每 16ms 推一次
 * 数字动画。组件卸载时这些定时器**不会自动消失**，于是：
 *
 *   - 回调仍会执行，去写已经卸载组件的 ref（Vue 不报错，静默无效）
 *   - 里面若带着网络请求（`refresh()`），就会发一次无人接收的请求
 *   - setInterval 更糟：它永远不停，一个页面反复进出会越积越多
 *
 * 而这些**全都不报错**，只表现为「切走又切回时偶发多一次请求」，
 * 靠手动 review 记住「新加定时器要配 onBeforeUnmount」不可靠。
 *
 * # 用法
 *
 * ```ts
 * const { later, every } = useTimers()
 * later(() => (visible.value = false), 700)
 * every(() => (tick.value++), 16)
 * ```
 *
 * 在组件 setup 里调用时自动注册 onBeforeUnmount；在组件外调用（纯逻辑测试）
 * 也不会抛错，此时需要自己调 clearAll()。
 */
export function useTimers() {
  const timeouts: number[] = []
  const intervals: number[] = []

  function later(fn: () => void, ms: number) {
    timeouts.push(window.setTimeout(fn, ms))
  }

  function every(fn: () => void, ms: number) {
    intervals.push(window.setInterval(fn, ms))
  }

  function clearAll() {
    timeouts.forEach((t) => clearTimeout(t))
    intervals.forEach((t) => clearInterval(t))
    // 清空数组而不只是清定时器：否则反复 mount/unmount 会无限增长，
    // 而这些句柄已经无效，留着只会让 clearAll 越跑越慢
    timeouts.length = 0
    intervals.length = 0
  }

  // getCurrentInstance 为空说明不在组件上下文中（例如直接调用做纯逻辑测试），
  // 此时注册 onBeforeUnmount 会抛警告，跳过即可 —— 调用方自行清理。
  if (getCurrentInstance()) {
    onBeforeUnmount(clearAll)
  }

  return { later, every, clearAll }
}
