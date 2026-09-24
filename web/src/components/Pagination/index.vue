<template>
  <div class="pagination-container" :class="{ hidden: hidden }">
    <el-pagination
      v-model:current-page="currentPage"
      v-model:page-size="pageSize"
      :background="background"
      :layout="layout"
      :page-sizes="pageSizes"
      :pager-count="pagerCount"
      :total="total"
      @size-change="handleSizeChange"
      @current-change="handleCurrentChange"
    />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

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
    pagerCount: window.innerWidth < 992 ? 5 : 7,
    layout: 'total, sizes, prev, pager, next, jumper',
    background: true,
    hidden: false,
  },
)

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

function handleSizeChange(val: number) {
  emit('pagination', { page: currentPage.value, limit: val })
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
