<template>
  <div ref="rightPanelRef" class="rightPanel-container" :class="{ show: visible }">
    <div class="rightPanel-background" @click="close" />
    <div class="rightPanel" :style="{ width: width + 'px' }">
      <div class="rightPanel__header">
        <h4>{{ title }}</h4>
        <el-icon class="rightPanel__close" @click="close"><Close /></el-icon>
      </div>
      <div class="rightPanel__body">
        <slot />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { watch } from 'vue'

const props = withDefaults(
  defineProps<{
    visible: boolean
    title?: string
    width?: number
  }>(),
  {
    title: '',
    width: 300,
  },
)

const emit = defineEmits<{
  'update:visible': [val: boolean]
}>()

function close() {
  emit('update:visible', false)
}

watch(
  () => props.visible,
  (val) => {
    document.body.style.overflow = val ? 'hidden' : ''
  },
)
</script>

<style lang="scss" scoped>
.rightPanel-container {
  position: fixed;
  top: 0;
  left: 0;
  width: 100%;
  height: 100%;
  z-index: 2000;
  pointer-events: none;

  &.show {
    pointer-events: auto;
  }
}

.rightPanel-background {
  position: absolute;
  top: 0;
  left: 0;
  width: 100%;
  height: 100%;
  background: var(--color-bg-mask);
  opacity: 0;
  transition: opacity var(--transition-normal);
  pointer-events: auto;

  .show & {
    opacity: 1;
  }
}

.rightPanel {
  width: 100%;
  height: 100%;
  position: absolute;
  top: 0;
  right: 0;
  background: var(--color-bg-card);
  box-shadow: var(--shadow-xl);
  transform: translateX(100%);
  transition: transform var(--transition-normal);
  overflow: hidden;

  .show & {
    transform: translateX(0);
  }
}

.rightPanel__header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: var(--spacing-lg);
  border-bottom: 1px solid var(--color-border-lighter);

  h4 {
    margin: 0;
    font-size: var(--font-size-md);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
  }
}

.rightPanel__close {
  cursor: pointer;
  font-size: 18px;
  color: var(--color-text-secondary);
  padding: 4px;
  border-radius: var(--radius-sm);
  transition: all var(--transition-fast);

  &:hover {
    color: var(--color-danger);
    background: var(--color-bg-hover);
  }
}

.rightPanel__body {
  height: calc(100% - 57px);
  overflow-y: auto;
  padding: var(--spacing-lg);
}
</style>
