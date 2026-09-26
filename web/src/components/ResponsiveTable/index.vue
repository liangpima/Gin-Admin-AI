<template>
  <!--
    桌面/平板：照旧用 el-table（一行代码都不改行为）
    手机（<768px）：同一份数据折叠成卡片列表

    为什么要做成组件而不是「每个页面各写一套卡片模板」：
    14 个页面 × 约 6 个字段 = 上百处重复，且**字段名与标签会在两处漂移** ——
    表格改了列、卡片忘了改，手机上就会少一个字段，而桌面端一切正常，
    这种问题只在手机上可见，最容易漏测。这里让列定义成为唯一来源：
    表格列与卡片字段都从同一个 `columns` 派生，标签不可能对不上。
  -->
  <div class="responsive-table">
    <el-table
      v-if="!isMobile"
      :data="tableData"
      v-loading="loading"
      :border="border"
      :stripe="stripe"
      :highlight-current-row="highlightCurrentRow"
      @current-change="onTableCurrentChange"
    >
      <el-table-column
        v-for="col in columns"
        :key="columnKey(col)"
        :prop="col.prop"
        :label="col.label"
        :width="col.width"
        :min-width="col.minWidth"
        :align="col.align"
        :show-overflow-tooltip="col.showOverflowTooltip"
        :fixed="col.fixed"
      >
        <template v-if="col.slot" #default="scope">
          <!--
            ⚠️ 这里**必须**用一层普通元素包住 slot 出口，不能直接写 `<slot/>`。

            el-table-column 会用一个 dummy row（`{ row: {}, column: {}, $index: -1 }`）
            调一次默认插槽，用来收集「子列」；它把结果里**是 ElTableColumn、
            或者是有状态组件（shapeFlag & 2）、或者是 Fragment** 的 vnode
            渲染进 `.hidden-columns`（见 element-plus 的 TableColumnRenderer）。

            直接写 `<slot/>` 时，插槽函数返回的是数组/Fragment，于是页面插槽里的
            `<el-switch>` 会被**真的挂载**一次，拿到 `row = {}` 执行 setup ——
            报 `[ElSwitch] model-value must be active-value or inactive-value`，
            并在 DOM 里多出一个隐藏开关（页面上看不见，只有控制台有告警）。

            包一层普通元素后，插槽函数返回的是单个元素 vnode，不满足上面的收集条件，
            隐藏区就空了。`display: contents` 让它不参与布局，对单元格无影响。
          -->
          <span class="cell-slot">
            <slot :name="col.slot" :row="scope.row" :index="scope.$index" />
          </span>
        </template>
      </el-table-column>
    </el-table>

    <div v-else v-loading="loading" class="responsive-cards">
      <el-empty v-if="!cardRows.length" description="暂无数据" :image-size="72" />
      <div
        v-for="(item, index) in cardRows"
        :key="item.key"
        class="record-card"
        :class="{ 'record-card--current': isCurrent(item.row, index) }"
        :data-testid="`record-card-${index}`"
        :style="item.level ? { marginLeft: `${item.level * 14}px` } : undefined"
        @click="highlightCurrentRow && emit('currentChange', item.row)"
      >
        <div
          v-for="col in cardColumns"
          :key="columnKey(col)"
          class="record-field"
          :data-label="col.label"
        >
          <span class="record-field__label">{{ col.label }}</span>
          <span class="record-field__value">
            <slot v-if="col.slot" :name="col.slot" :row="item.row" :index="index" />
            <template v-else>{{ display(item.row, col) }}</template>
          </span>
        </div>
        <div v-if="$slots.actions" class="record-card__actions">
          <slot name="actions" :row="item.row" :index="index" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts" generic="T">
import { computed } from 'vue'
import { useResponsive } from '@/hooks/useResponsive'
import type { ResponsiveColumn } from './types'

const props = withDefaults(
  defineProps<{
    data: T[]
    columns: ResponsiveColumn<T>[]
    loading?: boolean
    /** 行主键字段名，用于 v-for 的 key（默认 id） */
    rowKey?: string
    /**
     * 透传给 el-table 的边框/斑马纹。
     * 默认值与项目里多数表格一致（border 开、stripe 关），
     * 个别页面（支付订单列表）用的是 stripe，由该页显式覆盖。
     */
    border?: boolean
    stripe?: boolean
    /**
     * 主从联动用：开启后表格高亮当前行、卡片可点选，
     * 两者都通过 `currentChange` 抛出（字典页左侧「类型」列表就是这么用的）。
     * 卡片**只在开启时**绑定点击 —— 否则卡片里的下拉/开关会连带触发选中。
     */
    highlightCurrentRow?: boolean
    /** 当前选中行的主键值，用于给对应卡片加高亮边框 */
    currentRowKey?: string | number
    /**
     * 树形数据（部门、菜单）。开启后卡片会把树**按深度优先摊平**再渲染，
     * 并用左侧缩进表示层级。
     *
     * 为什么必须摊平：表格里子节点靠展开箭头呈现，而卡片的 `v-for` 只遍历
     * 顶层数组 —— 不摊平的话**子节点会整个消失**，桌面端却一切正常。
     */
    tree?: boolean
    /** 树形结构的子节点字段名（默认 children） */
    treeChildren?: string
  }>(),
  {
    loading: false,
    rowKey: 'id',
    border: true,
    stripe: false,
    highlightCurrentRow: false,
    tree: false,
    treeChildren: 'children',
  },
)

const emit = defineEmits<{
  currentChange: [row: T]
}>()

/**
 * el-table 的 `current-change` 形参是 `DefaultRow | null`（element-plus 自己的类型），
 * 与泛型 T 不兼容，所以在这里收口转换一次 —— 免得每个页面各自写断言。
 * 取消选中时 row 为 null，那种情况不往外抛。
 */
function onTableCurrentChange(row: Record<PropertyKey, unknown> | null) {
  if (row) emit('currentChange', row as unknown as T)
}

const { isMobile } = useResponsive()

// el-table 的 `data` 声明为 `DefaultRow[]`（即 `Record<PropertyKey, any>`），
// 与泛型 T 不兼容，这里显式桥接一次。
// 用 `unknown` 而不是 `any`：本项目的 ESLint 把 no-explicit-any 留在 warning 级，
// 而 lint 门槛是 `--max-warnings 0`，写 any 会让 CI 直接红。
const tableData = computed(() => props.data as unknown as Record<PropertyKey, unknown>[])

// 计算属性而不是模板里内联 filter：避免每次渲染都新建数组，
// 也让「卡片里该显示哪些列」这条规则只有一处。
const cardColumns = computed(() => props.columns.filter((c) => !c.hideInCard))

/** 卡片要渲染的行（树形时按深度优先摊平，并记录层级用于缩进） */
const cardRows = computed(() => {
  if (!props.tree) {
    return props.data.map((row, i) => ({ row, level: 0, key: rowKeyOf(row, i) }))
  }
  const out: { row: T; level: number; key: string }[] = []
  const walk = (rows: T[], level: number) => {
    rows.forEach((row, i) => {
      out.push({ row, level, key: `${level}-${rowKeyOf(row, i)}` })
      const children = (row as Record<string, unknown>)[props.treeChildren]
      if (Array.isArray(children) && children.length) {
        walk(children as T[], level + 1)
      }
    })
  }
  walk(props.data, 0)
  return out
})

// key 必须稳定且唯一：prop 可能缺失（纯 slot 列），label 也可能重复，
// 所以两者拼接后再退化到下标 —— 用下标当唯一 key 会在列表重排时错位。
function columnKey(col: ResponsiveColumn<T>, index?: number) {
  return col.prop ?? col.slot ?? `${col.label}-${index ?? 0}`
}

function rowKeyOf(row: T, index: number) {
  const v = (row as Record<string, unknown>)[props.rowKey]
  return v === undefined || v === null ? `idx-${index}` : String(v)
}

/** 卡片是否处于选中态（与表格的 highlight-current-row 对应） */
function isCurrent(row: T, index: number) {
  if (!props.highlightCurrentRow || props.currentRowKey === undefined) return false
  return rowKeyOf(row, index) === String(props.currentRowKey)
}

function display(row: T, col: ResponsiveColumn<T>): string {
  if (col.formatter) return col.formatter(row)
  const v = col.prop ? (row as Record<string, unknown>)[col.prop] : ''
  return v === undefined || v === null || v === '' ? '-' : String(v)
}
</script>

<style lang="scss" scoped>
.responsive-table {
  width: 100%;
}

// 见模板里的说明：这层包裹是为了让 el-table-column 的 dummy 渲染收集不到组件。
// display: contents 让它对单元格布局完全透明。
.cell-slot {
  display: contents;
}

.responsive-cards {
  min-height: 120px;
}

.record-card {
  padding: 12px 14px;
  margin-bottom: 12px;
  background: var(--color-bg-container, #fff);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--radius-md, 8px);

  &:last-child {
    margin-bottom: 0;
  }

  // 主从联动里的「当前项」：与 el-table 的 highlight-current-row 视觉对齐
  &--current {
    border-color: var(--el-color-primary);
    box-shadow: 0 0 0 1px var(--el-color-primary) inset;
  }
}

.record-field {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  justify-content: space-between;
  padding: 6px 0;

  // 相邻字段之间的细分隔线：卡片里没有表格线，靠它区分「这是两行」
  & + & {
    border-top: 1px dashed var(--el-border-color-extra-light);
  }
}

.record-field__label {
  flex: none;
  font-size: 13px;
  color: var(--color-text-secondary);
}

.record-field__value {
  flex: 1;
  font-size: 14px;
  color: var(--color-text-primary);
  text-align: right;
  word-break: break-all;
}

.record-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: flex-end;
  padding-top: 10px;
  margin-top: 6px;
  border-top: 1px solid var(--el-border-color-lighter);
}
</style>
