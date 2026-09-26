<template>
  <div class="mobile-action">
    <!--
      桌面端（≥1024px）：按 `actions` 生成普通按钮。
      **不要改回「渲染默认插槽」** —— 那样每个页面都得自己再写一遍按钮，
      而 `actions` 与按钮是两份数据，标签必然漂移；更糟的是页面一旦忘记写插槽，
      桌面上操作列会**整列空白**（手机上反而正常，最难发现）。
      system/user 就带着这个缺陷存在了很久。
    -->
    <template v-if="isDesktop">
      <el-button
        v-for="action in actions"
        :key="action.label"
        :type="action.type ?? 'primary'"
        link
        size="small"
        @click="emit('command', action.label)"
        >{{ action.label }}</el-button
      >
    </template>
    <template v-else>
      <el-dropdown @command="handleCommand">
        <el-icon class="action-trigger"><MoreFilled /></el-icon>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item v-for="action in actions" :key="action.label" :command="action.label">
              <!-- 图标色由 type 派生，页面不用再各写一遍 var(--el-color-xxx) -->
              <el-icon :style="{ color: `var(--el-color-${action.type ?? 'primary'})` }">
                <component :is="action.icon" />
              </el-icon>
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
  /** 全局注册的图标组件名（须在 utils/icons.ts 的 appIcons 白名单里） */
  icon: string
  /** 语义色：桌面按钮的 type 与下拉图标的颜色都用它 */
  type?: 'primary' | 'success' | 'warning' | 'danger' | 'info'
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
