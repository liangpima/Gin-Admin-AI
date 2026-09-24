<template>
  <div class="app-container">
    <el-card class="table-card">
      <template #header>
        <div class="card-header">
          <span>岗位管理</span>
          <el-button type="primary" @click="handleAdd()">新增岗位</el-button>
        </div>
      </template>
      <el-table :data="tableData" v-loading="loading" border>
        <el-table-column prop="code" label="岗位编码" min-width="100" />
        <el-table-column prop="name" label="岗位名称" min-width="100" />
        <el-table-column prop="sort" label="排序" width="60" />
        <el-table-column prop="status" label="状态" width="70">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">{{ row.status === 1 ? '正常' : '停用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="handleEdit(row as PostItem)">编辑</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row as PostItem)">删除</el-button>
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

    <FormDialog v-model="dialogVisible" :title="dialogTitle" :loading="submitLoading" @submit="handleSubmit">
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="80px">
        <el-form-item label="岗位编码" prop="code"><el-input v-model="form.code" placeholder="请输入岗位编码" /></el-form-item>
        <el-form-item label="岗位名称" prop="name"><el-input v-model="form.name" placeholder="请输入岗位名称" /></el-form-item>
        <el-form-item label="排序"><el-input-number v-model="form.sort" :min="0" /></el-form-item>
        <el-form-item label="状态">
          <el-radio-group v-model="form.status"><el-radio :value="1">正常</el-radio><el-radio :value="0">停用</el-radio></el-radio-group>
        </el-form-item>
      </el-form>
    </FormDialog>
  </div>
</template>

<script setup lang="ts">
import { getPostList, createPost, updatePost, deletePost, type PostItem, type PostQuery } from '@/api/post'
import { formatDateTime } from '@/utils/format'
import FormDialog from '@/components/FormDialog/index.vue'
import { useCrud } from '@/hooks/useCrud'

interface PostForm {
  id: number
  code: string
  name: string
  sort: number
  status: number
}

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
