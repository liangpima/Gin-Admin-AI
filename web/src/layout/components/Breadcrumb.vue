<template>
  <el-breadcrumb class="breadcrumb" separator="/">
    <transition-group name="breadcrumb">
      <el-breadcrumb-item v-for="(item, index) in breadcrumbs" :key="item.path">
        <span
          v-if="item.redirect === 'noRedirect' || index === breadcrumbs.length - 1"
          class="breadcrumb__text no-redirect"
        >
          {{ item.meta?.title }}
        </span>
        <a v-else class="breadcrumb__text" @click.prevent="handleLink(item)">
          {{ item.meta?.title }}
        </a>
      </el-breadcrumb-item>
    </transition-group>
  </el-breadcrumb>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter, type RouteLocationMatched } from 'vue-router'

const route = useRoute()
const router = useRouter()
const breadcrumbs = ref<RouteLocationMatched[]>([])

function getBreadcrumbs() {
  const matched = route.matched.filter((item) => item.meta?.title)
  breadcrumbs.value = matched
}

function handleLink(item: RouteLocationMatched) {
  const { redirect, path } = item
  if (redirect) {
    router.push(redirect as string)
  } else {
    router.push(path)
  }
}

watch(
  () => route.path,
  () => getBreadcrumbs(),
  { immediate: true },
)
</script>

<style lang="scss" scoped>
.breadcrumb {
  margin-left: var(--spacing-sm);
}

.breadcrumb__text {
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  transition: color var(--transition-fast);

  &:hover {
    color: var(--color-primary);
  }
}

.no-redirect {
  color: var(--color-text-secondary);
  cursor: default;

  &:hover {
    color: var(--color-text-secondary);
  }
}

.breadcrumb-enter-active {
  transition: all 0.3s;
}

.breadcrumb-leave-active {
  transition: all 0.2s;
  position: absolute;
}

.breadcrumb-enter-from,
.breadcrumb-leave-to {
  opacity: 0;
  transform: translateX(10px);
}
</style>
