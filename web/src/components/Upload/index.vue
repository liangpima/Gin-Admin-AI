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
import { computed, ref } from 'vue'
import { ElMessage, type UploadFile, type UploadUserFile } from 'element-plus'
import { csrfHeaders } from '@/utils/auth'

const props = withDefaults(
  defineProps<{
    action: string
    multiple?: boolean
    limit?: number
    accept?: string
    tip?: string
    maxSize?: number
    fileList?: UploadUserFile[]
  }>(),
  {
    multiple: false,
    limit: 1,
    accept: 'image/*',
    tip: '',
    maxSize: 5,
    fileList: () => [],
  },
)

// 直接用 Element Plus 自带的类型，不要自己写 any：
// 这些回调的签名由 el-upload 决定，抄错一个字段就会在运行期才暴露。
const emit = defineEmits<{
  success: [response: unknown, file: UploadFile]
  remove: [file: UploadFile]
  error: [error: Error]
}>()

const uploadRef = ref()

// 这里原本会手动塞一个 `Authorization: Bearer <token>` 头（P3-B2 删除）。
// 现在凭据是 HttpOnly cookie，el-upload 内部的 XHR 在同源下会自动携带它 ——
// 手动传反而传不出东西（JS 读不到 HttpOnly cookie，只会拼出 "Bearer undefined"，
// 那会被后端当成「头格式错误」而 401，比不传更糟）。
//
// 但 CSRF 令牌必须**手动传**（P3-B3）：el-upload 用的是自己的 XHR，
// 不经过 axios 的请求拦截器，拿不到那里附加的 X-CSRF-Token。
// 而上传是 POST —— 服务端要求 cookie 认证的写请求带上它，否则 403。
// 漏了这一行的现象是「其他写操作都正常，只有上传报 403」。
//
// 用 computed 而不是初始化时取一次：令牌由服务端在登录/续期时下发，
// 组件挂载时可能还没有（例如页面先渲染、用户稍后才登录），
// 取值必须延后到真正发请求的那一刻。
const uploadHeaders = computed(() => csrfHeaders())

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
