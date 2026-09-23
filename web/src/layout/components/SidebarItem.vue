<template>
  <div v-if="!item.meta?.hidden">
    <el-menu-item v-if="!hasMultiChildren" :index="menuPath">
      <el-icon v-if="menuIcon">
        <component :is="menuIcon" />
      </el-icon>
      <template #title>{{ menuTitle }}</template>
    </el-menu-item>

    <el-sub-menu v-else :index="item.path">
      <template #title>
        <el-icon v-if="item.meta?.icon">
          <component :is="item.meta.icon" />
        </el-icon>
        <span>{{ item.meta?.title || item.name }}</span>
      </template>
      <SidebarItem
        v-for="child in item.children"
        :key="child.path"
        :item="child"
        :base-path="resolvePath(child.path)"
      />
    </el-sub-menu>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { RouteRecordRaw } from 'vue-router'

const props = defineProps<{
  item: RouteRecordRaw
  basePath: string
}>()

const visibleChildren = computed(() => {
  if (!props.item.children) return []
  return props.item.children.filter((c) => !c.meta?.hidden)
})

const hasMultiChildren = computed(() => visibleChildren.value.length > 1)

const menuPath = computed(() => {
  if (visibleChildren.value.length === 1) {
    return props.basePath || props.item.path || ''
  }
  return props.basePath || props.item.path || ''
})

const menuIcon = computed(() => {
  if (visibleChildren.value.length === 1) {
    return visibleChildren.value[0].meta?.icon || props.item.meta?.icon || ''
  }
  return props.item.meta?.icon || ''
})

const menuTitle = computed(() => {
  if (visibleChildren.value.length === 1) {
    return visibleChildren.value[0].meta?.title || visibleChildren.value[0].name || ''
  }
  return props.item.meta?.title || props.item.name || ''
})

function resolvePath(childPath: string): string {
  if (!childPath) return props.basePath || ''
  if (childPath.startsWith('/')) return childPath
  return props.basePath ? `${props.basePath}/${childPath}` : childPath
}
</script>
