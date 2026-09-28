<template>
  <div class="pagination-container" :class="{ hidden: hidden }">
    <el-pagination
      v-model:current-page="currentPage"
      v-model:page-size="pageSize"
      :background="background"
      :layout="layout"
      :page-sizes="pageSizes"
      :pager-count="resolvedPagerCount"
      :total="total"
      @size-change="handleSizeChange"
      @current-change="handleCurrentChange"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useResponsive } from '@/hooks/useResponsive'

const props = withDefaults(
  defineProps<{
    total: number
    page?: number
    limit?: number
    pageSizes?: number[]
    pagerCount?: number
    layout?: string
    background?: boolean
    hidden?: boolean
  }>(),
  {
    page: 1,
    limit: 10,
    pageSizes: () => [10, 20, 50, 100],
    // pagerCount 刻意**不给默认值**：一旦给了默认值，`props.pagerCount`
    // 就永远是「已提供」，下面那个按屏幕宽度自适应的计算属性永远不会生效。
    layout: 'total, sizes, prev, pager, next, jumper',
    background: true,
    hidden: false,
  },
)

const { isMobile } = useResponsive()

/**
 * 页码按钮数量。
 *
 * 此前写的是 `pagerCount: window.innerWidth < 992 ? 5 : 7`，作为 withDefaults
 * 的默认值，有两个问题：
 *   1. `withDefaults` 的默认值只在**模块求值时计算一次** —— 之后窗口怎么缩放
 *      都不会重算，所以手机端实际仍渲染 7 个页码，横向挤不下；
 *   2. 992 不属于本项目的断点体系（768 / 1024，见 assets/styles/responsive.scss），
 *      与 `@include mobile` 的口径不一致：768–992 这段平板宽度会拿到桌面端密度。
 *
 * 改为用 useResponsive 的 isMobile（768 断点）做响应式计算；显式传入
 * pagerCount 时以传入值为准。
 */
const resolvedPagerCount = computed(() => props.pagerCount ?? (isMobile.value ? 5 : 7))

const emit = defineEmits<{
  'update:page': [val: number]
  'update:limit': [val: number]
  pagination: [data: { page: number; limit: number }]
}>()

const currentPage = computed({
  get: () => props.page,
  set: (val) => emit('update:page', val),
})

const pageSize = computed({
  get: () => props.limit,
  set: (val) => emit('update:limit', val),
})

/**
 * 切换每页条数时**必须回到第一页**。
 *
 * 否则从第 5 页（每页 10 条）切到每页 100 条时，第 5 页早已超出总页数，
 * 接口返回空列表 —— 用户看到的是「改了每页条数之后数据全没了」，
 * 而实际上数据都在第一页。
 *
 * 这里显式 emit `update:page` 而不是靠 `currentPage` 计算属性的 setter：
 * 意图更直白，也不依赖父组件 v-model 的更新时序。
 */
function handleSizeChange(val: number) {
  emit('update:page', 1)
  emit('pagination', { page: 1, limit: val })
}

function handleCurrentChange(val: number) {
  emit('pagination', { page: val, limit: pageSize.value })
}
</script>

<style lang="scss" scoped>
.pagination-container {
  display: flex;
  justify-content: flex-end;
  padding: var(--spacing-base) 0 0;

  &.hidden {
    display: none;
  }
}
</style>
