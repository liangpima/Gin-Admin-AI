<template>
  <div class="mobile-action">
    <template v-if="isDesktop">
      <slot />
    </template>
    <template v-else>
      <el-dropdown @command="handleCommand">
        <el-icon class="action-trigger"><MoreFilled /></el-icon>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item v-for="action in actions" :key="action.label" :command="action.label">
              <el-icon :style="{ color: action.color }"><component :is="action.icon" /></el-icon>
              <span>{{ action.label }}</span>
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </template>
  </div>
</template>

<script setup lang="ts">
import { useResponsive } from '@/hooks/useResponsive'

interface Action {
  label: string
  icon: string
  color?: string
}

defineProps<{
  actions: Action[]
}>()

const emit = defineEmits<{
  command: [cmd: string]
}>()

const { isDesktop } = useResponsive()

function handleCommand(cmd: string) {
  emit('command', cmd)
}
</script>

<style lang="scss" scoped>
.mobile-action {
  display: inline-flex;
  align-items: center;
}

.action-trigger {
  cursor: pointer;
  font-size: 18px;
  color: var(--color-text-secondary);
  padding: 4px;
  border-radius: var(--radius-sm);
  transition: all var(--transition-fast);

  &:hover {
    background: var(--color-bg-hover);
    color: var(--color-primary);
  }
}
</style>
