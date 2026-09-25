<template>
  <div class="app-container">
    <el-card class="table-card">
      <template #header>
        <div class="card-header">
          <span>岗位管理</span>
          <el-button type="primary" @click="handleAdd()">新增岗位</el-button>
        </div>
      </template>
      <ResponsiveTable :data="tableData" :columns="columns" :loading="loading">
        <template #status="{ row }">
          <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">{{
            row.status === 1 ? '正常' : '停用'
          }}</el-tag>
        </template>
        <template #actions="{ row }">
          <el-button type="primary" link size="small" @click="handleEdit(row as PostItem)"
            >编辑</el-button
          >
          <el-button type="danger" link size="small" @click="handleDelete(row as PostItem)"
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
        <el-form-item label="岗位编码" prop="code"
          ><el-input v-model="form.code" placeholder="请输入岗位编码"
        /></el-form-item>
        <el-form-item label="岗位名称" prop="name"
          ><el-input v-model="form.name" placeholder="请输入岗位名称"
        /></el-form-item>
        <el-form-item label="排序"><el-input-number v-model="form.sort" :min="0" /></el-form-item>
        <el-form-item label="状态">
          <el-radio-group v-model="form.status"
            ><el-radio :value="1">正常</el-radio
            ><el-radio :value="0">停用</el-radio></el-radio-group
          >
        </el-form-item>
      </el-form>
    </FormDialog>
  </div>
</template>

<script setup lang="ts">
import {
  getPostList,
  createPost,
  updatePost,
  deletePost,
  type PostItem,
  type PostQuery,
} from '@/api/post'
import { formatDateTime } from '@/utils/format'
import FormDialog from '@/components/FormDialog/index.vue'
import ResponsiveTable from '@/components/ResponsiveTable/index.vue'
import type { ResponsiveColumn } from '@/components/ResponsiveTable/types'
import { useCrud } from '@/hooks/useCrud'

interface PostForm {
  id: number
  code: string
  name: string
  sort: number
  status: number
}

// 列定义是**唯一来源**：桌面端的表格列与手机端的卡片字段都从这里派生。
// 在表格里加一列却忘了在卡片里加，手机上就会少一个字段 —— 而桌面端一切正常，
// 这种问题最容易漏测，所以不让两处各写一份。
const columns: ResponsiveColumn<PostItem>[] = [
  { label: '岗位编码', prop: 'code', minWidth: 100 },
  { label: '岗位名称', prop: 'name', minWidth: 100 },
  { label: '排序', prop: 'sort', width: 60 },
  { label: '状态', slot: 'status', width: 70 },
  {
    label: '创建时间',
    prop: 'createdAt',
    width: 170,
    formatter: (row) => formatDateTime(row.createdAt as string),
  },
  // 操作列在卡片里由 #actions 插槽渲染在底部，不作为字段重复一遍
  { label: '操作', slot: 'actions', width: 160, hideInCard: true },
]

// 分页、loading、弹窗开关与增删改查的编排都交给 useCrud；
// 这里只保留本页特有的东西：表单结构、校验规则、提交载荷。
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
  handleAdd,
  handleEdit,
  handleSubmit,
  handleDelete,
} = useCrud<PostItem, PostForm, PostQuery>({
  list: (params) => getPostList(params),
  create: (payload) => createPost(payload),
  update: (payload) => updatePost(payload),
  remove: (id) => deletePost(id),
  createForm: () => ({ id: 0, code: '', name: '', sort: 0, status: 1 }),
  titles: { add: '新增岗位', edit: '编辑岗位' },
  deleteConfirm: '确定删除该岗位？',
})

const formRules = {
  code: [{ required: true, message: '请输入岗位编码', trigger: 'blur' }],
  name: [{ required: true, message: '请输入岗位名称', trigger: 'blur' }],
}
</script>
