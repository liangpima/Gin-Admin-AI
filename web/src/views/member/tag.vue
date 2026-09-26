<template>
  <div class="app-container">
    <div class="search-form">
      <CollapsibleFilter>
        <el-form :model="queryParams">
          <el-row :gutter="16">
            <el-col :xs="24" :sm="12" :md="8" :lg="6">
              <el-form-item label="标签名称">
                <el-input
                  v-model="queryParams.name"
                  placeholder="请输入标签名称"
                  clearable
                  @keyup.enter="handleSearch"
                />
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
          <span>会员标签</span>
          <el-button type="primary" @click="handleAdd()">新增标签</el-button>
        </div>
      </template>

      <ResponsiveTable :data="tableData" :columns="columns" :loading="loading">
        <template #color="{ row }">
          <el-tag :color="row.color" style="color: #fff; border: none">{{ row.name }}</el-tag>
        </template>
        <template #status="{ row }">
          <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">{{
            row.status === 1 ? '正常' : '停用'
          }}</el-tag>
        </template>
        <template #actions="{ row }">
          <el-button type="primary" link size="small" @click="handleEdit(row as MemberTagItem)"
            >编辑</el-button
          >
          <el-button type="danger" link size="small" @click="handleDelete(row as MemberTagItem)"
            >删除</el-button
          >
        </template>
      </ResponsiveTable>

      <Pagination
        v-model:page="page"
        v-model:limit="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        :background="false"
        @pagination="loadData"
      />
    </el-card>

    <FormDialog
      v-model="dialogVisible"
      :title="dialogTitle"
      :loading="submitLoading"
      @submit="handleSubmit"
    >
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="80px">
        <el-form-item label="标签名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入标签名称" />
        </el-form-item>
        <el-form-item label="颜色">
          <el-color-picker v-model="form.color" />
          <span class="form-hint">{{ form.color }}</span>
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort" :min="0" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.status" :active-value="1" :inactive-value="0" />
        </el-form-item>
      </el-form>
    </FormDialog>
  </div>
</template>

<script setup lang="ts">
import { reactive } from 'vue'
import {
  getMemberTagList,
  createMemberTag,
  updateMemberTag,
  deleteMemberTag,
  type MemberTagItem,
} from '@/api/member'
import CollapsibleFilter from '@/components/CollapsibleFilter/index.vue'
import FormDialog from '@/components/FormDialog/index.vue'
import ResponsiveTable from '@/components/ResponsiveTable/index.vue'
import type { ResponsiveColumn } from '@/components/ResponsiveTable/types'
import { useCrud } from '@/hooks/useCrud'

interface TagForm {
  id: number
  name: string
  color: string
  sort: number
  status: number
}

// 列定义是唯一来源：桌面端表格列与手机端卡片字段都从这里派生
const columns: ResponsiveColumn<MemberTagItem>[] = [
  { label: 'ID', prop: 'id', width: 60 },
  { label: '标签名称', prop: 'name', minWidth: 120 },
  { label: '颜色', slot: 'color', width: 100 },
  { label: '排序', prop: 'sort', width: 70, align: 'center' },
  { label: '状态', slot: 'status', width: 80 },
  { label: '操作', slot: 'actions', width: 160, hideInCard: true },
]

// 搜索条件只放本页自己的字段。page/pageSize 由 useCrud 管理 ——
// 原先它们混在同一个 queryParams 里，重置时要记得一并复位，很容易漏。
const queryParams = reactive({ name: '' })

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
} = useCrud<MemberTagItem, TagForm, { name?: string; page: number; pageSize: number }>({
  list: (params) => getMemberTagList(params),
  create: (payload) => createMemberTag(payload),
  update: (payload) => updateMemberTag(payload),
  remove: (id) => deleteMemberTag(id),
  createForm: () => ({ id: 0, name: '', color: '#409eff', sort: 0, status: 1 }),
  query: () => ({ name: queryParams.name }),
  titles: { add: '新增标签', edit: '编辑标签' },
  deleteConfirm: '确认删除该标签？',
})

function handleReset() {
  queryParams.name = ''
  handleSearch()
}

const formRules = {
  name: [{ required: true, message: '请输入标签名称', trigger: 'blur' }],
}
</script>

<style lang="scss" scoped>
.form-hint {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.table-card {
  :deep(.el-card__header) {
    border-bottom-color: var(--color-border-lighter);
  }
}
</style>
