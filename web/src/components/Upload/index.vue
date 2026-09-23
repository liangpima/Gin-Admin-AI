<template>
  <el-upload
    ref="uploadRef"
    :action="action"
    :headers="uploadHeaders"
    :multiple="multiple"
    :limit="limit"
    :accept="accept"
    :before-upload="handleBeforeUpload"
    :on-success="handleSuccess"
    :on-error="handleError"
    :on-exceed="handleExceed"
    :on-remove="handleRemove"
    :file-list="fileList"
    list-type="picture-card"
  >
    <el-icon class="upload-icon"><Plus /></el-icon>
    <template #tip>
      <div v-if="tip" class="upload-tip">{{ tip }}</div>
    </template>
  </el-upload>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { ElMessage, type UploadFile, type UploadUserFile } from 'element-plus'
import { getToken } from '@/utils/auth'

const props = withDefaults(defineProps<{
  action: string
  multiple?: boolean
  limit?: number
  accept?: string
  tip?: string
  maxSize?: number
  fileList?: UploadUserFile[]
}>(), {
  multiple: false,
  limit: 1,
  accept: 'image/*',
  tip: '',
  maxSize: 5,
  fileList: () => [],
})

// 直接用 Element Plus 自带的类型，不要自己写 any：
// 这些回调的签名由 el-upload 决定，抄错一个字段就会在运行期才暴露。
const emit = defineEmits<{
  success: [response: unknown, file: UploadFile]
  remove: [file: UploadFile]
  error: [error: Error]
}>()

const uploadRef = ref()

const uploadHeaders = computed(() => ({
  Authorization: `Bearer ${getToken()}`,
}))

function handleBeforeUpload(file: File) {
  const isLt = file.size / 1024 / 1024 < props.maxSize
  if (!isLt) {
    ElMessage.error(`文件大小不能超过 ${props.maxSize}MB!`)
    return false
  }
  return true
}

function handleSuccess(response: unknown, file: UploadFile) {
  ElMessage.success('上传成功')
  emit('success', response, file)
}

function handleError(error: Error) {
  ElMessage.error('上传失败')
  emit('error', error)
}

function handleExceed() {
  ElMessage.warning(`最多只能上传 ${props.limit} 个文件`)
}

function handleRemove(file: UploadFile) {
  emit('remove', file)
}
</script>

<style lang="scss" scoped>
.upload-icon {
  font-size: 28px;
  color: var(--color-text-placeholder);
}

.upload-tip {
  font-size: var(--font-size-xs);
  color: var(--color-text-secondary);
  margin-top: var(--spacing-xs);
}
</style>
