<template>
  <div class="table-skeleton">
    <el-skeleton :rows="rows" animated />
  </div>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    rows?: number
  }>(),
  {
    rows: 5,
  },
)
</script>

<style lang="scss" scoped>
.table-skeleton {
  padding: var(--spacing-lg);
}
</style>
