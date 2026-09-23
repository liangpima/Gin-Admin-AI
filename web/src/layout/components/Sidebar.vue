<template>
  <div class="sidebar" :class="sidebarClass">
    <div class="logo">
      <div class="logo__icon">
        <img v-if="siteLogo" :src="siteLogo" class="logo__img" />
        <svg v-else viewBox="0 0 32 32" width="24" height="24" fill="none" xmlns="http://www.w3.org/2000/svg">
          <rect width="32" height="32" rx="8" fill="var(--el-color-primary)"/>
          <path d="M8 16L16 8L24 16L16 24L8 16Z" fill="white" opacity="0.9"/>
        </svg>
      </div>
      <transition name="fade">
        <h1 v-show="showFull" class="logo__text">{{ siteName }}</h1>
      </transition>
    </div>
    <el-scrollbar class="sidebar__menu-wrap">
      <el-menu
        :default-active="activeMenu"
        :collapse="menuCollapse"
        :unique-opened="true"
        router
      >
        <template v-if="menuRoutes.length > 0">
          <SidebarItem
            v-for="route in menuRoutes"
            :key="route.path"
            :item="route"
            :base-path="route.path"
          />
        </template>
      </el-menu>
    </el-scrollbar>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/store/modules/app'
import { usePermissionStore } from '@/store/modules/permission'
import { useResponsive } from '@/hooks/useResponsive'
import { getConfigByPrefix, type ConfigItem } from '@/api/config'
import SidebarItem from './SidebarItem.vue'

const route = useRoute()
const appStore = useAppStore()
const permissionStore = usePermissionStore()
const { isMobile } = useResponsive()

const siteName = ref('Gin-Admin')
const siteLogo = ref('')

const activeMenu = computed(() => route.path)

const menuCollapse = computed(() => {
  if (isMobile.value) return false
  return !appStore.sidebar.opened
})

const showFull = computed(() => appStore.sidebar.opened)

const sidebarClass = computed(() => ({
  'is-collapse': !isMobile.value && !appStore.sidebar.opened,
  'is-open': isMobile.value && appStore.sidebar.opened,
}))

const menuRoutes = computed(() => {
  if (!permissionStore.routesLoaded) return []
  return permissionStore.routes.filter((r) => !r.meta?.hidden)
})

watch(isMobile, (val) => {
  if (val) {
    appStore.closeSidebar(true)
  } else {
    if (!appStore.sidebar.opened) {
      appStore.toggleSidebar()
    }
  }
})

onMounted(async () => {
  if (isMobile.value) {
    appStore.closeSidebar(true)
  }
  try {
    const res = await getConfigByPrefix('site.')
    const list: ConfigItem[] = res.data || []
    const map: Record<string, string> = {}
    list.forEach((item) => {
      const key = item.key?.replace('site.', '')
      if (key) map[key] = item.value || ''
    })
    if (map.name) siteName.value = map.name
    if (map.logo) siteLogo.value = map.logo
  } catch {}
})
</script>

<style lang="scss" scoped>
@use '@/assets/styles/responsive.scss' as *;

.sidebar {
  height: 100vh;
  background: var(--el-bg-color);
  transition: width 0.28s;
  width: 210px;
  flex-shrink: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--el-border-color-lighter);

  &.is-collapse {
    width: 64px;
  }

  @include mobile {
    position: fixed;
    z-index: 1000;
    left: 0;
    top: 0;
    width: 0;

    &.is-open {
      width: 210px;
    }
  }

  :deep(.el-menu) {
    border-right: none !important;
    width: 100%;
  }

  :deep(.el-scrollbar) {
    height: calc(100vh - 50px);
  }

  :deep(.el-scrollbar__view) {
    height: 100%;
  }
}

.logo {
  height: 50px;
  min-height: 50px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  padding: 0 12px;
  overflow: hidden;
}

.logo__icon {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}

.logo__img {
  width: 24px;
  height: 24px;
  object-fit: contain;
}

.logo__text {
  color: var(--el-text-color-primary);
  font-size: 16px;
  font-weight: 600;
  margin: 0;
  white-space: nowrap;
  overflow: hidden;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.15s;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
