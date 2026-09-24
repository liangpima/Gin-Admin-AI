<template>
  <div class="app-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>附件管理</span>
          <input
            ref="fileInputRef"
            type="file"
            multiple
            style="display: none"
            @change="onFileInputChange"
          />
          <el-button type="primary" :loading="uploading" @click="fileInputRef?.click()">
            {{ uploading ? '上传中...' : '上传附件' }}
          </el-button>
        </div>
      </template>

      <div class="gallery-toolbar">
        <el-input
          v-model="queryParams.name"
          placeholder="搜索文件名"
          clearable
          style="width: 200px"
          @keyup.enter="loadData"
          @clear="loadData"
        />
        <el-select v-model="sortOrder" style="width: 160px" @change="loadData">
          <el-option label="上传时间倒序" value="desc" />
          <el-option label="上传时间正序" value="asc" />
        </el-select>
      </div>

      <div v-if="loading" class="loading-text">加载中...</div>

      <div v-else class="gallery-grid">
        <div v-for="item in tableData" :key="item.id" class="gallery-item">
          <div class="gallery-item__img" @click="openPreview(item)">
            <img
              v-if="item.mimeType?.startsWith('image/')"
              :src="item.url"
              loading="lazy"
              @error="(e: Event) => ((e.target as HTMLElement).style.display = 'none')"
            />
            <img
              v-else-if="item.mimeType?.startsWith('video/')"
              src="/images/media.png"
              class="gallery-item__video-cover"
            />
            <div v-else class="gallery-item__icon">
              <el-icon :size="32"><Document /></el-icon>
            </div>
          </div>
          <div class="gallery-item__footer">
            <span class="gallery-item__name" :title="item.name">{{ item.name }}</span>
            <span class="gallery-item__size">{{ formatSize(item.size) }}</span>
            <el-button type="danger" link size="small" @click.stop="handleDelete(item)">
              <el-icon><Delete /></el-icon>
            </el-button>
          </div>
        </div>

        <el-empty v-if="tableData.length === 0" description="暂无文件" />
      </div>

      <Pagination
        v-model:page="page"
        v-model:limit="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        :background="false"
        @pagination="loadData"
      />
    </el-card>

    <Teleport to="body">
      <div v-if="showViewer" class="img-viewer-overlay" @click="closePreview">
        <div class="img-viewer-close" @click.stop="closePreview">&times;</div>
        <div
          v-if="viewerIndex > 0"
          class="img-viewer-arrow img-viewer-arrow--left"
          @click.stop="goPrev"
        >
          &#8249;
        </div>
        <img :src="viewerUrl" class="img-viewer-img" @click.stop />
        <div
          v-if="viewerIndex < imageList.length - 1"
          class="img-viewer-arrow img-viewer-arrow--right"
          @click.stop="goNext"
        >
          &#8250;
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Document } from '@element-plus/icons-vue'
import { getFileList, deleteFile, uploadFile, type FileItem } from '@/api/file'

const loading = ref(false)
const tableData = ref<FileItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(24)
const sortOrder = ref('desc')
const queryParams = reactive({ name: '' })

const fileInputRef = ref<HTMLInputElement>()
const uploading = ref(false)

const showViewer = ref(false)
const viewerUrl = ref('')
const viewerIndex = ref(0)

const imageList = computed(() => tableData.value.map((f) => f.url))

function openPreview(item: FileItem) {
  if (!item.mimeType?.startsWith('image/')) {
    ElMessage.warning('该文件格式不支持预览')
    return
  }
  viewerIndex.value = imageList.value.indexOf(item.url)
  viewerUrl.value = item.url
  showViewer.value = true
  document.body.style.overflow = 'hidden'
}

function closePreview() {
  showViewer.value = false
  document.body.style.overflow = ''
}

function goPrev() {
  if (viewerIndex.value > 0) {
    viewerIndex.value--
    viewerUrl.value = imageList.value[viewerIndex.value]
  }
}

function goNext() {
  if (viewerIndex.value < imageList.value.length - 1) {
    viewerIndex.value++
    viewerUrl.value = imageList.value[viewerIndex.value]
  }
}

function onKeydown(e: KeyboardEvent) {
  if (!showViewer.value) return
  if (e.key === 'Escape') closePreview()
  else if (e.key === 'ArrowLeft') goPrev()
  else if (e.key === 'ArrowRight') goNext()
}

onMounted(() => {
  loadData()
  document.addEventListener('keydown', onKeydown)
})
onBeforeUnmount(() => document.removeEventListener('keydown', onKeydown))

async function onFileInputChange(e: Event) {
  const input = e.target as HTMLInputElement
  if (!input.files || input.files.length === 0) return
  const files = Array.from(input.files)
  input.value = ''
  const valid: File[] = []
  const maxSize = 50 * 1024 * 1024
  for (const file of files) {
    if (file.size > maxSize) {
      ElMessage.error(`${file.name} 超过 50MB`)
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

function formatSize(bytes: number) {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / 1024 / 1024).toFixed(1) + ' MB'
}

async function loadData() {
  loading.value = true
  try {
    const res = await getFileList({
      name: queryParams.name,
      page: page.value,
      pageSize: pageSize.value,
    })
    tableData.value = res.data.list
    total.value = res.data.total
  } finally {
    loading.value = false
  }
}

async function handleDelete(row: FileItem) {
  try {
    await ElMessageBox.confirm(`确定要删除文件「${row.name}」吗？`, '提示', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning',
    })
    await deleteFile(row.id)
    ElMessage.success('删除成功')
    loadData()
  } catch (err) {
    // 用户点「取消」时 ElMessageBox 会 reject 出 'cancel' / 'close' 字符串，
    // 那是正常操作，不该记成错误；只有接口真的失败才留排查痕迹
    // （接口失败的提示由响应拦截器负责）
    if (err !== 'cancel' && err !== 'close') {
      console.warn('[file] 删除文件失败', err)
    }
  }
}
</script>

<style lang="scss" scoped>
.gallery-toolbar {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
}

.loading-text {
  text-align: center;
  padding: 60px 0;
  color: var(--el-text-color-secondary);
}

.gallery-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 16px;
  min-height: 200px;

  .el-empty {
    grid-column: 1 / -1;
    padding: 60px 0;
  }
}

.gallery-item {
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  overflow: hidden;
  transition: box-shadow 0.2s;

  &:hover {
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  }

  &__img {
    width: 100%;
    height: 140px;
    background: var(--el-fill-color-lighter);
    cursor: pointer;
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

  &__video-cover {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  &__icon {
    color: var(--el-text-color-secondary);
  }

  &__footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 8px;
    background: var(--el-bg-color);
  }

  &__name {
    font-size: 12px;
    color: var(--el-text-color-regular);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 1;
    margin-right: 4px;
  }

  &__size {
    font-size: 11px;
    color: var(--el-text-color-placeholder);
    white-space: nowrap;
    margin-right: 4px;
  }
}
</style>

<style>
.img-viewer-overlay {
  position: fixed;
  top: 0;
  left: 0;
  width: 100%;
  height: 100%;
  background: rgba(0, 0, 0, 0.5);
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
}

.img-viewer-close {
  position: absolute;
  top: 16px;
  right: 20px;
  font-size: 32px;
  color: #fff;
  cursor: pointer;
  z-index: 10001;
  line-height: 1;
}

.img-viewer-img {
  max-width: 90vw;
  max-height: 90vh;
  object-fit: contain;
}

.img-viewer-arrow {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  font-size: 48px;
  color: rgba(255, 255, 255, 0.7);
  cursor: pointer;
  z-index: 10001;
  padding: 20px;
  user-select: none;

  &:hover {
    color: #fff;
  }
}

.img-viewer-arrow--left {
  left: 10px;
}

.img-viewer-arrow--right {
  right: 10px;
}
</style>
