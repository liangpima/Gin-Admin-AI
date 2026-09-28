import { h, type Component } from 'vue'

/**
 * 让 `<keep-alive :include>` 按**路由名**生效。
 *
 * ## 它修的是什么
 *
 * `<keep-alive>` 的 `include` / `exclude` 是按**组件自身的 name** 匹配的，
 * 而 `tagsView` store 里的 `cachedViews` 存的是**路由 name**（如 `SystemUser`）。
 * 两者本是两个不同的命名空间 —— 只有当组件名恰好等于路由名时才碰巧对上。
 *
 * 本项目所有视图文件都叫 `index.vue`（`system/user/index.vue` 等），
 * `@vue/compiler-sfc` 会据此把组件命名为 `index`；全仓也没有任何
 * `defineOptions({ name })`。于是：
 *
 *   `include: ['SystemUser']` 永远匹配不上名为 `index` 的组件
 *   → **缓存静默失效**，每次切路由都重新挂载，列表页的筛选/分页/滚动位置全丢，
 *     并且会重复发请求。没有任何报错，控制台也是干净的。
 *
 * ## 为什么是「运行时合成」而不是在每个视图里写 `defineOptions({ name })`
 *
 * 后者是更常见的做法（路由 name 与组件 name 都写死，人工保持一致），
 * 但在这个项目里会**必然漂移**：菜单的 `name` 是用户可以在「菜单管理」里
 * 改的字段（后端 `sys_menu.name`，前端菜单表单已补上该输入项）。
 * 一旦用户把 `User` 改成 `Users`，数据库里的路由 name 变了，
 * 而 `.vue` 里静态写死的组件名不会跟着变 —— 缓存再次静默失效，
 * 而且这次连「对照源码能看出来」都做不到。
 *
 * 因此这里以**路由 name 为唯一来源**，在渲染前合成一层同名包装组件，
 * 使 `include` 与真实组件名天然对齐，不存在第二处需要同步的命名。
 *
 * ## 为什么必须缓存包装组件
 *
 * Vue 判断「是不是同一个组件」用的是**组件对象的引用**。若每次渲染都新建
 * 一个 `{ name, render }` 字面量，Vue 会认为组件类型变了 → 销毁旧实例、
 * 重建新实例 —— 那正是 keep-alive 要避免的事（缓存会被反复清掉）。
 * 所以包装对象按 name 缓存复用。
 */
const wrapperCache = new Map<string, { source: unknown; wrapper: Component }>()

/**
 * 把组件包一层「以路由 name 命名」的壳，供 keep-alive 的 include 匹配。
 *
 * @param component router-view 解析后的组件（可能是 undefined，如路由还没匹配上）
 * @param name      路由 name；为空时无法命名，原样透传（该页面不参与缓存）
 */
export function wrapForCacheName(component: unknown, name: unknown): unknown {
  if (!component) return component
  if (typeof name !== 'string' || name === '') return component

  const hit = wrapperCache.get(name)
  // 同一个 name 对应同一个组件定义时才复用：若组件真的换了（例如登出后
  // 重新 addRoute 拿到了新的模块实例），必须换一个新包装 ——
  // 复用旧包装会渲染到已废弃的组件上。
  if (hit && hit.source === component) return hit.wrapper

  const source = component
  const wrapper: Component = {
    name,
    render() {
      return h(source as Component)
    },
  }
  wrapperCache.set(name, { source, wrapper })
  return wrapper
}

/**
 * 清空包装缓存。**仅供测试使用** —— 生产代码不应调用它，
 * 否则正在被缓存的页面会因为包装对象换新而被重建。
 */
export function resetCacheNameWrappers(): void {
  wrapperCache.clear()
}
