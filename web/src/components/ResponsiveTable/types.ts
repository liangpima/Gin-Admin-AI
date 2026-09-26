/**
 * ResponsiveTable 的列定义。
 *
 * 单独成文件而不是放在 SFC 里：`<script setup>` 不允许 `export`，
 * 而页面需要这个类型来声明自己的 `columns`（这样列定义才有类型检查，
 * 写错 `prop` 或 `slot` 名能在 `npm run typecheck` 阶段发现，
 * 而不是等到手机上发现「卡片少了一个字段」）。
 *
 * 对行类型做成泛型（`ResponsiveColumn<PostItem>`）而不是
 * `Record<string, unknown>`：后者会让 `PostItem[]` 传不进 `data`
 * （接口没有字符串索引签名，TS 直接报不兼容），
 * 也会让页面里的 `row.createdAt` 失去类型。
 */
export interface ResponsiveColumn<T> {
  label: string
  /** 直接取值用的字段名；用了 slot 时可以不填 */
  prop?: keyof T & string
  width?: number | string
  minWidth?: number | string
  /** 单元格对齐方式，透传给 el-table-column（卡片里恒为「标签左、值右」） */
  align?: 'left' | 'center' | 'right'
  /** 内容超宽时以 tooltip 展示，透传给 el-table-column（卡片里不需要，文字自然换行） */
  showOverflowTooltip?: boolean
  /** 固定列，透传给 el-table-column（仅桌面端有意义；卡片里不存在横向滚动） */
  fixed?: boolean | 'left' | 'right'
  /** 具名插槽名：表格与卡片共用同一份渲染实现 */
  slot?: string
  /** 卡片里不展示（例如「操作」列 —— 它由 #actions 插槽统一渲染在卡片底部） */
  hideInCard?: boolean
  /** 卡片里的取值格式化；不配则直接取 row[prop] */
  formatter?: (row: T) => string
}
