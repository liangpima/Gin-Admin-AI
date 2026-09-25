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
    <el-table v-if="!isMobile" :data="tableData" v-loading="loading" border>
      <el-table-column
        v-for="col in columns"
        :key="columnKey(col)"
        :prop="col.prop"
        :label="col.label"
        :width="col.width"
        :min-width="col.minWidth"
      >
        <template v-if="col.slot" #default="scope">
          <slot :name="col.slot" :row="scope.row" :index="scope.$index" />
        </template>
      </el-table-column>
    </el-table>

    <div v-else v-loading="loading" class="responsive-cards">
      <el-empty v-if="!data.length" description="暂无数据" :image-size="72" />
      <div
        v-for="(row, index) in data"
        :key="rowKeyOf(row, index)"
        class="record-card"
        :data-testid="`record-card-${index}`"
      >
        <div
          v-for="col in cardColumns"
          :key="columnKey(col)"
          class="record-field"
          :data-label="col.label"
        >
          <span class="record-field__label">{{ col.label }}</span>
          <span class="record-field__value">
            <slot v-if="col.slot" :name="col.slot" :row="row" :index="index" />
            <template v-else>{{ display(row, col) }}</template>
          </span>
        </div>
        <div v-if="$slots.actions" class="record-card__actions">
          <slot name="actions" :row="row" :index="index" />
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
  }>(),
  { loading: false, rowKey: 'id' },
)

const { isMobile } = useResponsive()

// el-table 的 `data` 声明为 `DefaultRow[]`（即 `Record<PropertyKey, any>`），
// 与泛型 T 不兼容，这里显式桥接一次。
// 用 `unknown` 而不是 `any`：本项目的 ESLint 把 no-explicit-any 留在 warning 级，
// 而 lint 门槛是 `--max-warnings 0`，写 any 会让 CI 直接红。
const tableData = computed(() => props.data as unknown as Record<PropertyKey, unknown>[])

// 计算属性而不是模板里内联 filter：避免每次渲染都新建数组，
// 也让「卡片里该显示哪些列」这条规则只有一处。
const cardColumns = computed(() => props.columns.filter((c) => !c.hideInCard))

// key 必须稳定且唯一：prop 可能缺失（纯 slot 列），label 也可能重复，
// 所以两者拼接后再退化到下标 —— 用下标当唯一 key 会在列表重排时错位。
function columnKey(col: ResponsiveColumn<T>, index?: number) {
  return col.prop ?? col.slot ?? `${col.label}-${index ?? 0}`
}

function rowKeyOf(row: T, index: number) {
  const v = (row as Record<string, unknown>)[props.rowKey]
  return v === undefined || v === null ? `idx-${index}` : String(v)
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
