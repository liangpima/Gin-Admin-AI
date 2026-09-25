import type { Component } from 'vue'
import {
  Aim,
  ArrowDown,
  Back,
  Briefcase,
  Check,
  CircleCheck,
  CircleClose,
  Close,
  Coin,
  Collection,
  Delete,
  Document,
  Edit,
  Expand,
  Fold,
  FolderOpened,
  HomeFilled,
  Key,
  Lock,
  Menu,
  Message,
  Moon,
  MoreFilled,
  Notebook,
  OfficeBuilding,
  Plus,
  Position,
  PriceTag,
  Refresh,
  Setting,
  Sunny,
  SwitchButton,
  Tools,
  TrendCharts,
  Upload,
  User,
  UserFilled,
  VideoCamera,
  Wallet,
} from '@element-plus/icons-vue'

/**
 * 全局注册的图标**白名单**。
 *
 * ## 为什么不是「全量注册」
 *
 * 改造前 `main.ts` 写的是：
 *
 * ```ts
 * import * as ElementPlusIconsVue from '@element-plus/icons-vue'
 * for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
 *   app.component(key, component)
 * }
 * ```
 *
 * 图标包导出 **293 个**图标（`dist/index.js` 320 kB），全部进了首屏主包，
 * 而全仓实际只用 39 个。改成显式清单后主包少了约 270 kB 的图标代码。
 *
 * ## 为什么必须是白名单而不是「删掉全局注册」
 *
 * 本项目有**多处按字符串名解析图标**的站点，它们不经过模板静态解析，
 * 只能靠全局注册才能渲染出来：
 *
 * - `layout/components/SidebarItem.vue` —— `<component :is="item.meta.icon" />`，
 *   图标名来自**数据库** `sys_menu.icon`
 * - `views/system/menu/index.vue` —— 菜单管理页渲染 `row.icon`（同样来自数据库）
 * - `views/dashboard/index.vue` —— 卡片列表里的 `icon: 'User'` 这类字符串
 * - `views/system/user/index.vue` —— 操作菜单里的 `icon: 'Key'` 这类字符串
 * - `components/MobileAction/index.vue` —— `<component :is="action.icon" />`
 * - `views/login/index.vue` —— `prefix-icon="User"` / `prefix-icon="Lock"`
 *
 * 所以清单必须覆盖「模板里当标签用的」**和**「当字符串传的」两类。
 * 漏掉一个的症状是**静默**的：`<component :is="不存在的名字">` 不报错，
 * 只是那个位置什么都没有 —— 侧边栏会少一个图标，而控制台一片安静。
 *
 * `icons.spec.ts` 就是防这件事的：它扫描全仓源码，把上面几类站点里的图标名
 * 全部找出来，逐个断言在清单里。
 */
export const appIcons: Record<string, Component> = {
  Aim,
  ArrowDown,
  Back,
  Briefcase,
  Check,
  CircleCheck,
  CircleClose,
  Close,
  Coin,
  Collection,
  Delete,
  Document,
  Edit,
  Expand,
  Fold,
  FolderOpened,
  HomeFilled,
  Key,
  Lock,
  Menu,
  Message,
  Moon,
  MoreFilled,
  Notebook,
  OfficeBuilding,
  Plus,
  Position,
  PriceTag,
  Refresh,
  Setting,
  Sunny,
  SwitchButton,
  Tools,
  TrendCharts,
  Upload,
  User,
  UserFilled,
  VideoCamera,
  Wallet,
}

/**
 * 数据库 `sys_menu.icon` 里在用的图标名（2026-09-25 实测）。
 *
 * 单独列出来是因为它们的**来源不在代码里** —— 侧边栏与菜单管理页会按字符串
 * 解析这些名字，但 grep 源码找不到它们，只有查库才知道。将来往菜单表里加新图标时，
 * 要一并加进 `appIcons`，否则那个菜单的图标会静默变空白。
 *
 * 查库命令（见 `.workbuddy-ai/memory/ENV-AND-TOOLING.md`）：
 * ```
 * mysql -uroot -p123456 -N -e "SELECT DISTINCT icon FROM gin.sys_menu WHERE icon <> '';"
 * ```
 *
 * 注意：**不要**从 `sql/init.sql` 里 grep 图标名 —— 那行 VALUES 同时含
 * `name`/`component`/`icon` 多列，会把菜单名（`Agreement`/`Config`/`Dept`…）
 * 一起捞出来，结果不可信。
 */
export const MENU_ICON_NAMES: string[] = [
  'Briefcase',
  'Coin',
  'Collection',
  'Document',
  'FolderOpened',
  'Lock',
  'Menu',
  'Message',
  'OfficeBuilding',
  'Position',
  'PriceTag',
  'Tools',
  'TrendCharts',
  'Upload',
  'User',
  'UserFilled',
  'Wallet',
]

/** 在 `main.ts` 里把白名单图标注册为全局组件 */
export function registerAppIcons(app: {
  component: (name: string, component: Component) => unknown
}): void {
  for (const [name, component] of Object.entries(appIcons)) {
    app.component(name, component)
  }
}
