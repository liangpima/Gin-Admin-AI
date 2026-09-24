<template>
  <div class="app-container">
    <div class="search-form">
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
    </div>

    <el-card class="table-card">
      <template #header>
        <div class="card-header">
          <span>会员标签</span>
          <el-button type="primary" @click="handleAdd()">新增标签</el-button>
        </div>
      </template>

      <el-table :data="tableData" v-loading="loading" border>
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="name" label="标签名称" min-width="120" />
        <el-table-column label="颜色" width="100">
          <template #default="{ row }">
            <el-tag :color="row.color" style="color: #fff; border: none">{{ row.name }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="sort" label="排序" width="70" align="center" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">{{
              row.status === 1 ? '正常' : '停用'
            }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="handleEdit(row as MemberTagItem)"
              >编辑</el-button
            >
            <el-button type="danger" link size="small" @click="handleDelete(row as MemberTagItem)"
              >删除</el-button
            >
          </template>
        </el-table-column>
      </el-table>

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
import FormDialog from '@/components/FormDialog/index.vue'
import { useCrud } from '@/hooks/useCrud'

interface TagForm {
  id: number
  name: string
  color: string
  sort: number
  status: number
}

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
