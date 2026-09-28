/**
 * 侧边栏菜单的纯逻辑。
 *
 * 抽出来的原因：项目没装 `@vue/test-utils`，`<script setup>` 里的 computed
 * 无法直接单测。而这两个函数恰好是「错了也不报错、只在界面上少一个高亮」
 * 的那类逻辑 —— 只能靠用例钉住（与 `components/ResponsiveTable/logic.ts` 同一考虑）。
 */

/**
 * 子路径 → 完整路径。
 *
 * ⚠️ 必须处理 `basePath === '/'`：直接拼 `${basePath}/${childPath}` 会得到
 * `//dashboard`，它与 `route.path`（`/dashboard`）不相等，
 * 而 `el-menu` 的 `default-active` 正是 `route.path` —— 结果是首页永远不高亮。
 * 顶层菜单的 basePath 恰好就是 `'/'`（Sidebar.vue 传的是 `menuRoute.path`），
 * 所以这条分支是主路径，不是边界。
 */
export function resolvePath(basePath: string, childPath: string): string {
  if (!childPath) return basePath || ''
  if (childPath.startsWith('/')) return childPath

  const base = (basePath || '').replace(/\/+$/, '')
  return base ? `${base}/${childPath}` : `/${childPath}`
}

/**
 * 菜单项的 `index`（也就是点击后要导航到的路径）。
 *
 * 只有一个可见子节点时，本节点在菜单里退化成**叶子**，index 必须是
 * **子路由解析后的路径**，不能是父路径。用父路径会同时坏掉两件事，都不报错：
 *
 *   ① `el-menu` 的 `default-active` 拿到的是 `route.path`（如 `/dashboard`），
 *      而 index 是 `'/'`，两者永不相等 → 「首页」永远不高亮；
 *   ② 点它会 push 到父路径。`/` 恰好有 `redirect: '/dashboard'` 兜住，
 *      但对没有 redirect 的动态父路由（例如只有单个子页面的 `/settings`）
 *      会渲染一个空的 Layout —— 内容区一片空白。
 *
 * 多子节点时走 `el-sub-menu`，index 只承担「唯一标识」的作用，用父路径即可。
 */
export function menuIndex(basePath: string, itemPath: string, visibleChildPaths: string[]): string {
  if (visibleChildPaths.length === 1) {
    return resolvePath(basePath, visibleChildPaths[0])
  }
  return basePath || itemPath || ''
}
