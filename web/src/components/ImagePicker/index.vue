<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    width="780px"
    :close-on-click-modal="false"
    @close="handleClose"
  >
    <div class="image-picker">
      <div class="image-picker__toolbar">
        <el-input
          v-model="searchName"
          placeholder="搜索文件名"
          clearable
          style="width: 200px"
          @keyup.enter="handleSearch"
          @clear="handleSearch"
        />
        <el-select v-model="sortOrder" style="width: 160px" @change="handleSearch">
          <el-option label="上传时间倒序" value="desc" />
          <el-option label="上传时间正序" value="asc" />
        </el-select>
        <div style="flex: 1" />
        <input
          ref="fileInputRef"
          type="file"
          :accept="acceptFilter"
          multiple
          style="display: none"
          @change="onFileInputChange"
        />
        <el-button type="primary" :loading="uploading" @click="fileInputRef?.click()">
          {{ uploading ? '上传中...' : uploadBtnText }}
        </el-button>
      </div>

      <div v-loading="loading" class="image-picker__grid">
        <div
          v-for="item in tableData"
          :key="item.id"
          class="image-picker__item"
          :class="{ 'is-selected': isSelected(item) }"
          @click="toggleSelect(item)"
        >
          <div class="image-picker__img">
            <img
              v-if="item.mimeType?.startsWith('image/')"
              :src="item.url"
              loading="lazy"
              @error="(e: Event) => ((e.target as HTMLElement).style.display = 'none')"
            />
            <img
              v-else-if="item.mimeType?.startsWith('video/')"
              src="/images/media.png"
              class="image-picker__video-cover"
            />
            <div v-else class="image-picker__video-icon">
              <el-icon :size="32"><VideoCamera /></el-icon>
            </div>
            <div v-if="isSelected(item)" class="image-picker__check">
              <el-icon><Check /></el-icon>
            </div>
          </div>
          <div class="image-picker__name" :title="item.name">{{ item.name }}</div>
        </div>

        <el-empty v-if="!loading && tableData.length === 0" :description="emptyText" />
      </div>

      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        style="margin-top: 12px; justify-content: flex-end"
        @current-change="loadData"
      />
    </div>

    <template #footer>
      <el-button @click="handleClose">取消</el-button>
      <el-button type="primary" @click="handleConfirm">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Check, VideoCamera } from '@element-plus/icons-vue'
import { getFileList, uploadFile, type FileItem } from '@/api/file'

const props = withDefaults(
  defineProps<{
    visible: boolean
    multiple?: boolean
    type?: 'image' | 'video'
  }>(),
  {
    multiple: false,
    type: 'image',
  },
)

const emit = defineEmits<{
  'update:visible': [val: boolean]
  confirm: [url: string | string[]]
}>()

const dialogTitle = computed(() => (props.type === 'video' ? '选择视频' : '选择图片'))
const uploadBtnText = computed(() => (props.type === 'video' ? '上传视频' : '上传图片'))
const emptyText = computed(() => (props.type === 'video' ? '暂无视频' : '暂无图片'))
const acceptFilter = computed(() => (props.type === 'video' ? 'video/*' : 'image/*'))

const visible = ref(props.visible)
watch(
  () => props.visible,
  (val) => {
    visible.value = val
  },
)
watch(visible, (val) => {
  emit('update:visible', val)
})

const loading = ref(false)
const uploading = ref(false)
const tableData = ref<FileItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(24)
const sortOrder = ref('desc')
const searchName = ref('')
const selectedIds = ref<Set<number>>(new Set())
const selectedMap = ref<Map<number, FileItem>>(new Map())

const fileInputRef = ref<HTMLInputElement>()

function isSelected(item: FileItem) {
  return selectedIds.value.has(item.id)
}

function toggleSelect(item: FileItem) {
  if (props.multiple) {
    if (selectedIds.value.has(item.id)) {
      selectedIds.value.delete(item.id)
      selectedMap.value.delete(item.id)
    } else {
      selectedIds.value.add(item.id)
      selectedMap.value.set(item.id, item)
    }
  } else {
    if (selectedIds.value.has(item.id)) {
      selectedIds.value.clear()
      selectedMap.value.clear()
    } else {
      selectedIds.value.clear()
      selectedMap.value.clear()
      selectedIds.value.add(item.id)
      selectedMap.value.set(item.id, item)
    }
  }
}

function handleSearch() {
  page.value = 1
  loadData()
}

async function loadData() {
  loading.value = true
  try {
    const mimeType = props.type === 'video' ? 'video' : props.type === 'image' ? 'image' : ''
    const res = await getFileList({
      name: searchName.value,
      mimeType,
      sortOrder: sortOrder.value,
      page: page.value,
      pageSize: pageSize.value,
    })
    tableData.value = res.data.list
    total.value = res.data.total
  } finally {
    loading.value = false
  }
}

async function onFileInputChange(e: Event) {
  const input = e.target as HTMLInputElement
  if (!input.files || input.files.length === 0) return
  const files = Array.from(input.files)
  input.value = ''

  const maxSize = props.type === 'video' ? 50 * 1024 * 1024 : 10 * 1024 * 1024
  const valid: File[] = []
  for (const file of files) {
    if (file.size > maxSize) {
      ElMessage.error(`${file.name} 超过${props.type === 'video' ? '50MB' : '10MB'}`)
      continue
    }
    valid.push(file)
  }
  if (valid.length === 0) return

  uploading.value = true
  try {
    for (const file of valid) {
      await uploadFile(file)
    }
    ElMessage.success(`成功上传 ${valid.length} 个文件`)
    loadData()
  } catch {
    ElMessage.error('上传失败')
  } finally {
    uploading.value = false
  }
}

function handleConfirm() {
  if (selectedIds.value.size === 0) {
    ElMessage.warning(props.type === 'video' ? '请先选择视频' : '请先选择图片')
    return
  }
  if (props.multiple) {
    const urls = Array.from(selectedMap.value.values()).map((item) => item.url)
    emit('confirm', urls)
  } else {
    const first = selectedMap.value.values().next().value
    emit('confirm', first!.url)
  }
  visible.value = false
}

function handleClose() {
  visible.value = false
}

watch(visible, (val) => {
  if (val) {
    selectedIds.value.clear()
    selectedMap.value.clear()
    page.value = 1
    loadData()
  }
})
</script>

<style lang="scss" scoped>
.image-picker {
  &__toolbar {
    display: flex;
    gap: 12px;
    margin-bottom: 16px;
    align-items: center;
  }

  &__grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
    grid-auto-rows: max-content;
    gap: 12px;
    max-height: 450px;
    overflow-y: auto;
    align-content: start;

    .el-empty {
      grid-column: 1 / -1;
      padding: 40px 0;
    }
  }

  &__item {
    border: 2px solid transparent;
    border-radius: 6px;
    overflow: hidden;
    cursor: pointer;
    transition:
      border-color 0.2s,
      box-shadow 0.2s;

    &:hover {
      box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12);
    }

    &.is-selected {
      border-color: var(--el-color-primary);
      box-shadow: 0 0 0 1px var(--el-color-primary);
    }
  }

  &__img {
    width: 100%;
    height: 110px;
    background: var(--el-fill-color-lighter);
    position: relative;
    overflow: hidden;
    display: flex;
    align-items: center;
    justify-content: center;

    img {
      width: 100%;
      height: 100%;
      object-fit: cover;
      display: block;
    }
  }

  &__video-icon {
    color: var(--el-text-color-secondary);
  }

  &__video-cover {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  &__check {
    position: absolute;
    top: 0;
    right: 0;
    width: 24px;
    height: 24px;
    background: var(--el-color-primary);
    color: #fff;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 14px;
    border-bottom-left-radius: 6px;
  }

  &__name {
    padding: 4px 6px;
    font-size: 12px;
    color: var(--el-text-color-regular);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    background: var(--el-bg-color);
  }
}
</style>
