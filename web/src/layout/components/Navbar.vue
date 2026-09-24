<template>
  <div class="navbar">
    <div class="left-menu">
      <el-icon class="hamburger" @click="toggleSidebar">
        <Fold v-if="appStore.sidebar.opened && !isMobile" />
        <Expand v-else />
      </el-icon>
      <Breadcrumb v-if="!isMobile" />
    </div>

    <div class="right-menu">
      <el-tooltip content="刷新页面（清除缓存）" placement="bottom">
        <span class="right-menu__icon" @click="handleRefresh">
          <el-icon :size="18"><Refresh /></el-icon>
        </span>
      </el-tooltip>

      <el-tooltip :content="isDark ? '切换亮色模式' : '切换暗色模式'" placement="bottom">
        <span class="right-menu__icon" @click="toggleTheme">
          <el-icon :size="18">
            <Moon v-if="!isDark" />
            <Sunny v-else />
          </el-icon>
        </span>
      </el-tooltip>

      <el-dropdown class="avatar-container" trigger="click">
        <div class="avatar-wrapper">
          <el-avatar :size="32" :src="userStore.userInfo?.avatar || undefined" class="avatar">
            {{ userStore.userInfo?.nickname?.charAt(0) || 'A' }}
          </el-avatar>
          <span class="username" v-if="!isMobile">{{
            userStore.userInfo?.nickname || userStore.userInfo?.username
          }}</span>
          <el-icon class="avatar-arrow"><ArrowDown /></el-icon>
        </div>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item divided @click="handleLogout">
              <el-icon><SwitchButton /></el-icon>
              退出登录
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useAppStore } from '@/store/modules/app'
import { useUserStore } from '@/store/modules/user'
import { useResponsive } from '@/hooks/useResponsive'
import { useTheme } from '@/hooks/useTheme'
import Breadcrumb from './Breadcrumb.vue'

const router = useRouter()
const appStore = useAppStore()
const userStore = useUserStore()
const { isMobile } = useResponsive()
const { isDark, toggleTheme } = useTheme()

function handleRefresh() {
  location.reload()
}

function toggleSidebar() {
  appStore.toggleSidebar()
}

async function handleLogout() {
  await userStore.logout()
  router.push('/login')
}
</script>

<style lang="scss" scoped>
@use '@/assets/styles/responsive.scss' as *;

.navbar {
  height: 50px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px;
  background: var(--el-bg-color);
  box-shadow: 0 1px 4px rgba(0, 21, 41, 0.08);
  border-bottom: 1px solid var(--el-border-color-lighter);

  @include mobile {
    padding: 0 12px;
  }
}

.left-menu {
  display: flex;
  align-items: center;
}

.hamburger {
  font-size: 20px;
  cursor: pointer;
  margin-right: 12px;
  color: var(--el-text-color-regular);
  padding: 4px;
  border-radius: 4px;
  transition: all 0.15s;

  &:hover {
    background: var(--el-fill-color-light);
    color: var(--el-color-primary);
  }
}

.right-menu {
  display: flex;
  align-items: center;
  gap: 8px;
}

.right-menu__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  cursor: pointer;
  color: var(--el-text-color-regular);
  border-radius: 6px;
  transition: all 0.15s;

  &:hover {
    background: var(--el-fill-color-light);
    color: var(--el-color-primary);
  }
}

.avatar-wrapper {
  display: flex;
  align-items: center;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 6px;
  transition: all 0.15s;

  &:hover {
    background: var(--el-fill-color-light);
  }

  .avatar {
    background: var(--el-color-primary);
    color: white;
    font-weight: 600;
  }

  .username {
    margin-left: 8px;
    font-size: 14px;
    color: var(--el-text-color-regular);
    font-weight: 500;

    @include mobile {
      display: none;
    }
  }

  .avatar-arrow {
    margin-left: 4px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
}
</style>
