<template>
  <div class="app-container">
    <div class="search-form">
      <CollapsibleFilter>
        <el-form :model="queryParams">
          <el-row :gutter="16">
            <el-col :xs="24" :sm="12" :md="8" :lg="6">
              <el-form-item label="标题">
                <el-input
                  v-model="queryParams.name"
                  placeholder="请输入标题"
                  clearable
                  @keyup.enter="handleSearch"
                />
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :md="8" :lg="6">
              <el-form-item label="类型">
                <el-select
                  v-model="queryParams.type"
                  placeholder="全部"
                  clearable
                  style="width: 100%"
                >
                  <el-option
                    v-for="item in typeOptions"
                    :key="item.value"
                    :label="item.label"
                    :value="item.value"
                  />
                </el-select>
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :md="8" :lg="6">
              <el-form-item label="状态">
                <el-select
                  v-model="queryParams.status"
                  placeholder="全部"
                  clearable
                  style="width: 100%"
                >
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
      </CollapsibleFilter>
    </div>

    <el-card class="table-card">
      <template #header>
        <div class="card-header">
          <span>协议管理</span>
          <el-button type="primary" @click="handleAdd()">新增协议</el-button>
        </div>
      </template>

      <ResponsiveTable :data="tableData" :columns="columns" :loading="loading">
        <template #type="{ row }">{{ typeMap[row.type] || row.type }}</template>
        <template #status="{ row }">
          <el-switch
            v-model="row.status"
            :active-value="1"
            :inactive-value="0"
            @change="handleStatusChange(row as AgreementItem)"
          />
        </template>
        <template #createdAt="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        <template #actions="{ row }">
          <el-button type="primary" link size="small" @click="handleEdit(row as AgreementItem)"
            >编辑</el-button
          >
          <el-button type="danger" link size="small" @click="handleDelete(row as AgreementItem)"
            >删除</el-button
          >
        </template>
      </ResponsiveTable>

      <Pagination
        v-model:page="page"
        v-model:limit="pageSize"
        :page-sizes="[10, 20, 50]"
        :total="total"
        layout="total, sizes, prev, pager, next"
        :background="false"
        @pagination="loadData"
      />
    </el-card>

    <FormDialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="800px"
      top="5vh"
      :loading="submitLoading"
      @submit="handleSubmit"
    >
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="80px">
        <el-form-item label="标题" prop="title">
          <el-input v-model="form.title" placeholder="请输入标题" />
        </el-form-item>
        <el-form-item label="类型" prop="type">
          <el-select v-model="form.type" placeholder="请选择类型" style="width: 100%">
            <el-option
              v-for="item in typeOptions"
              :key="item.value"
              :label="item.label"
              :value="item.value"
            />
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
import { defineAsyncComponent, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import {
  getAgreementList,
  createAgreement,
  updateAgreement,
  deleteAgreement,
  type AgreementItem,
  type AgreementQuery,
} from '@/api/agreement'
// wangEditor 整个库约 800 kB。同步 import 会把它并进协议页自己的 chunk，
// 页面壳与表单要等它下载完才开始渲染。改异步后壳先出来，编辑器随后到。
//
// 注意它**本来就是懒加载的**（协议页是懒加载路由，不点进来不会下载）——
// 这一步只解决「同一页内重资源阻塞渲染」，不是省流量。
const WangEditor = defineAsyncComponent(() => import('@/components/WangEditor/index.vue'))
import CollapsibleFilter from '@/components/CollapsibleFilter/index.vue'
import FormDialog from '@/components/FormDialog/index.vue'
import ResponsiveTable from '@/components/ResponsiveTable/index.vue'
import type { ResponsiveColumn } from '@/components/ResponsiveTable/types'
import { formatDateTime } from '@/utils/format'
import { useCrud } from '@/hooks/useCrud'

// 列定义是唯一来源：桌面端表格列与手机端卡片字段都从这里派生
const columns: ResponsiveColumn<AgreementItem>[] = [
  { label: 'ID', prop: 'id', width: 60 },
  { label: '标题', prop: 'title', minWidth: 150, showOverflowTooltip: true },
  { label: '类型', slot: 'type', width: 120 },
  { label: '排序', prop: 'sort', width: 70 },
  { label: '状态', slot: 'status', width: 80 },
  { label: '创建时间', slot: 'createdAt', width: 170 },
  { label: '操作', slot: 'actions', width: 160, hideInCard: true },
]

interface AgreementForm {
  id: number
  title: string
  content: string
  type: string
  sort: number
  status: number
}

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

// 搜索条件只放本页自己的字段；page/pageSize 由 useCrud 管理
const queryParams = reactive({
  name: '',
  type: '',
  status: undefined as number | undefined,
})

const {
  loading,
  submitLoading,
  tableData,
  total,
  page,
  pageSize,
  dialogVisible,
  dialogTitle,
  form,
  formRef,
  loadData,
  handleSearch,
  handleAdd,
  handleEdit,
  handleSubmit,
  handleDelete,
} = useCrud<AgreementItem, AgreementForm, AgreementQuery>({
  list: (params) => getAgreementList(params),
  create: (payload) => createAgreement(payload),
  update: (payload) => updateAgreement(payload),
  remove: (id) => deleteAgreement(id),
  createForm: () => ({ id: 0, title: '', content: '', type: '', sort: 0, status: 1 }),
  // content 兜底成空串：接口可能给 null，而 WangEditor 的 v-model 拿到 null
  // 会让编辑器初始化异常（原实现就是 `row.content || ''`，这里保留该防御）
  rowToForm: (row) => ({
    id: row.id,
    title: row.title,
    content: row.content || '',
    type: row.type,
    sort: row.sort,
    status: row.status,
  }),
  query: () => ({ name: queryParams.name, type: queryParams.type, status: queryParams.status }),
  titles: { add: '新增协议', edit: '编辑协议' },
  deleteConfirm: '确认删除该协议？',
})

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

const formRules = {
  title: [{ required: true, message: '请输入标题', trigger: 'blur' }],
  type: [{ required: true, message: '请选择类型', trigger: 'change' }],
}
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
  .w-e-text [data-w-e-type='video'],
  .w-e-text .w-e-video-container,
  .w-e-text .w-e-video-mask,
  .w-e-text .w-e-video-wrapper {
    max-width: 100% !important;
    width: 100% !important;
    height: auto !important;
  }
}
</style>
