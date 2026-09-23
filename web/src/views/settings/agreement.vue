<template>
  <div class="app-container">
    <div class="search-form">
      <el-form :model="queryParams">
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="标题">
              <el-input v-model="queryParams.name" placeholder="请输入标题" clearable @keyup.enter="handleSearch" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="类型">
              <el-select v-model="queryParams.type" placeholder="全部" clearable style="width: 100%">
                <el-option v-for="item in typeOptions" :key="item.value" :label="item.label" :value="item.value" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="状态">
              <el-select v-model="queryParams.status" placeholder="全部" clearable style="width: 100%">
                <el-option label="正常" :value="1" />
                <el-option label="停用" :value="0" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item>
              <el-button type="primary" @click="handleSearch">搜索</el-button>
              <el-button @click="handleReset">重置</el-button>
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
    </div>

    <el-card class="table-card">
      <template #header>
        <div class="card-header">
          <span>协议管理</span>
          <el-button type="primary" @click="handleAdd">新增协议</el-button>
        </div>
      </template>

      <el-table :data="tableData" v-loading="loading" border>
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="title" label="标题" min-width="150" show-overflow-tooltip />
        <el-table-column label="类型" width="120">
          <template #default="{ row }">
            {{ typeMap[row.type] || row.type }}
          </template>
        </el-table-column>
        <el-table-column prop="sort" label="排序" width="70" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-switch v-model="row.status" :active-value="1" :inactive-value="0" @change="handleStatusChange(row as AgreementItem)" />
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="handleEdit(row as AgreementItem)">编辑</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row as AgreementItem)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <Pagination
        v-model:page="queryParams.page"
        v-model:limit="queryParams.pageSize"
        :page-sizes="[10, 20, 50]"
        :total="total"
        layout="total, sizes, prev, pager, next"
        :background="false"
        @pagination="loadData"
      />
    </el-card>

    <FormDialog v-model="dialogVisible" :title="dialogTitle" width="800px" top="5vh" :loading="submitLoading" @submit="handleSubmit">
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="80px">
        <el-form-item label="标题" prop="title">
          <el-input v-model="form.title" placeholder="请输入标题" />
        </el-form-item>
        <el-form-item label="类型" prop="type">
          <el-select v-model="form.type" placeholder="请选择类型" style="width: 100%">
            <el-option v-for="item in typeOptions" :key="item.value" :label="item.label" :value="item.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort" :min="0" />
        </el-form-item>
        <el-form-item label="状态">
          <el-radio-group v-model="form.status">
            <el-radio :value="1">正常</el-radio>
            <el-radio :value="0">停用</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="内容" prop="content">
          <WangEditor v-model="form.content" :height="350" />
        </el-form-item>
      </el-form>
    </FormDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'
import { getAgreementList, createAgreement, updateAgreement, deleteAgreement , type AgreementItem } from '@/api/agreement'
import WangEditor from '@/components/WangEditor/index.vue'
import FormDialog from '@/components/FormDialog/index.vue'
import { formatDateTime } from '@/utils/format'

const typeOptions = [
  { label: '用户协议', value: 'terms' },
  { label: '隐私政策', value: 'privacy' },
  { label: '关于我们', value: 'about' },
  { label: '联系方式', value: 'contact' },
]

const typeMap: Record<string, string> = {
  terms: '用户协议',
  privacy: '隐私政策',
  about: '关于我们',
  contact: '联系方式',
}

const loading = ref(false)
const submitLoading = ref(false)
const tableData = ref<AgreementItem[]>([])
const total = ref(0)
const dialogVisible = ref(false)
const dialogTitle = ref('')
const formRef = ref<FormInstance>()

const queryParams = reactive({
  name: '',
  type: '',
  status: undefined as number | undefined,
  page: 1,
  pageSize: 10,
})

const form = reactive({
  id: 0,
  title: '',
  content: '',
  type: '',
  sort: 0,
  status: 1,
})

const formRules = {
  title: [{ required: true, message: '请输入标题', trigger: 'blur' }],
  type: [{ required: true, message: '请选择类型', trigger: 'change' }],
}

async function loadData() {
  loading.value = true
  try {
    const res = await getAgreementList(queryParams)
    tableData.value = res.data.list
    total.value = res.data.total
  } finally {
    loading.value = false
  }
}

function handleSearch() {
  queryParams.page = 1
  loadData()
}

function handleReset() {
  queryParams.name = ''
  queryParams.type = ''
  queryParams.status = undefined
  handleSearch()
}

async function handleStatusChange(row: AgreementItem) {
  try {
    await updateAgreement({ id: row.id, status: row.status })
    ElMessage.success('状态修改成功')
  } catch {
    row.status = row.status === 1 ? 0 : 1
  }
}

function handleAdd() {
  form.id = 0
  form.title = ''
  form.content = ''
  form.type = ''
  form.sort = 0
  form.status = 1
  dialogTitle.value = '新增协议'
  dialogVisible.value = true
}

function handleEdit(row: AgreementItem) {
  form.id = row.id
  form.title = row.title
  form.content = row.content || ''
  form.type = row.type
  form.sort = row.sort
  form.status = row.status
  dialogTitle.value = '编辑协议'
  dialogVisible.value = true
}

async function handleSubmit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return

  submitLoading.value = true
  try {
    if (form.id) {
      await updateAgreement(form)
    } else {
      await createAgreement(form)
    }
    ElMessage.success('操作成功')
    dialogVisible.value = false
    loadData()
  } finally {
    submitLoading.value = false
  }
}

async function handleDelete(row: AgreementItem) {
  await ElMessageBox.confirm('确认删除该协议？', '提示', { type: 'warning' })
  await deleteAgreement(row.id)
  ElMessage.success('删除成功')
  loadData()
}

onMounted(() => {
  loadData()
})
</script>

<style lang="scss" scoped>
.table-card {
  :deep(.el-card__header) {
    border-bottom-color: var(--color-border-lighter);
  }
}
</style>

<style lang="scss">
// 非 scoped - 强制约束编辑器内视频宽度
.form-dialog .w-e-text-container {
  overflow: hidden !important;

  .w-e-text {
    overflow: hidden !important;
    max-width: 100% !important;
    width: 100% !important;

    > * {
      max-width: 100% !important;
    }
  }

  .w-e-text video,
  .w-e-text iframe,
  .w-e-text [data-w-e-type="video"],
  .w-e-text .w-e-video-container,
  .w-e-text .w-e-video-mask,
  .w-e-text .w-e-video-wrapper {
    max-width: 100% !important;
    width: 100% !important;
    height: auto !important;
  }
}
</style>
