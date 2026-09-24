<template>
  <el-dialog
    :model-value="modelValue"
    :title="title"
    :width="width"
    :top="top"
    destroy-on-close
    append-to-body
    :close-on-click-modal="false"
    @update:model-value="$emit('update:modelValue', $event)"
    @close="$emit('update:modelValue', false)"
    class="form-dialog"
  >
    <slot></slot>
    <template #footer>
      <div class="form-dialog__footer">
        <el-button @click="$emit('update:modelValue', false)" :disabled="loading">{{
          cancelText
        }}</el-button>
        <el-button type="primary" @click="$emit('submit')" :loading="loading">
          <el-icon v-if="!loading"><Check /></el-icon>
          {{ confirmText }}
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { Check } from '@element-plus/icons-vue'

withDefaults(
  defineProps<{
    modelValue: boolean
    title?: string
    width?: string
    top?: string
    loading?: boolean
    cancelText?: string
    confirmText?: string
  }>(),
  {
    title: '操作',
    width: '600px',
    loading: false,
    cancelText: '取消',
    confirmText: '确定',
  },
)

defineEmits<{
  'update:modelValue': [value: boolean]
  submit: []
}>()
</script>

<style lang="scss">
.form-dialog {
  .el-dialog {
    border-radius: 12px;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.15);
  }

  .el-dialog__header {
    padding: 20px 24px 16px;
    margin: 0;
    border-bottom: 1px solid var(--el-border-color-lighter);
    background: var(--el-fill-color-lighter);

    .el-dialog__title {
      font-size: 16px;
      font-weight: 600;
      color: var(--el-text-color-primary);
    }

    .el-dialog__headerbtn {
      top: 20px;
      right: 20px;
    }
  }

  .el-dialog__body {
    padding: 24px;
    max-height: calc(90vh - 160px);
    overflow-y: auto;
  }

  .el-dialog__footer {
    padding: 16px 24px;
    border-top: 1px solid var(--el-border-color-lighter);
    background: var(--el-bg-color-page);
  }
}

.form-dialog .el-form-item {
  margin-bottom: 20px;

  &:last-child {
    margin-bottom: 0;
  }
}

.form-dialog .el-form-item__label {
  font-weight: 500;
}

.form-dialog .el-input__wrapper,
.form-dialog .el-textarea__inner {
  border-radius: 8px;
  transition: all 0.2s ease;

  &:hover {
    box-shadow: 0 0 0 1px var(--el-color-primary-light-5) inset;
  }

  &.is-focus {
    box-shadow:
      0 0 0 1px var(--el-color-primary) inset,
      0 0 0 3px var(--el-color-primary-light-9);
  }
}

.form-dialog__footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;

  .el-button--primary {
    border-radius: 8px;
    padding: 8px 20px;

    .el-icon {
      margin-right: 4px;
    }
  }

  .el-button:not(.el-button--primary) {
    border-radius: 8px;
    padding: 8px 20px;
  }
}
</style>
