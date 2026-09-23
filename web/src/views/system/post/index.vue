<template>
  <div class="app-container">
    <el-card class="table-card">
      <template #header>
        <div class="card-header">
          <span>岗位管理</span>
          <el-button type="primary" @click="handleAdd">新增岗位</el-button>
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
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'
import { getPostList, createPost, updatePost, deletePost , type PostItem} from '@/api/post'
import { formatDateTime } from '@/utils/format'
import FormDialog from '@/components/FormDialog/index.vue'

const loading = ref(false)
const submitLoading = ref(false)
const tableData = ref<PostItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const dialogVisible = ref(false)
const dialogTitle = ref('')
const formRef = ref<FormInstance>()

const form = reactive({ id: 0, code: '', name: '', sort: 0, status: 1 })
const formRules = {
  code: [{ required: true, message: '请输入岗位编码', trigger: 'blur' }],
  name: [{ required: true, message: '请输入岗位名称', trigger: 'blur' }],
}

async function loadData() {
  loading.value = true
  try {
    const res = await getPostList({ page: page.value, pageSize: pageSize.value })
    tableData.value = res.data.list
    total.value = res.data.total
  } finally { loading.value = false }
}

function handleAdd() {
  form.id = 0; form.code = ''; form.name = ''; form.sort = 0; form.status = 1
  dialogTitle.value = '新增岗位'; dialogVisible.value = true
}

function handleEdit(row: PostItem) {
  Object.assign(form, row)
  dialogTitle.value = '编辑岗位'; dialogVisible.value = true
}

async function handleSubmit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  submitLoading.value = true
  try {
    if (form.id) { await updatePost(form) }
    else { await createPost(form) }
    ElMessage.success('操作成功'); dialogVisible.value = false; loadData()
  } finally { submitLoading.value = false }
}

async function handleDelete(row: PostItem) {
  await ElMessageBox.confirm('确定删除该岗位？', '提示', { type: 'warning' })
  await deletePost(row.id)
  ElMessage.success('删除成功'); loadData()
}

onMounted(() => loadData())
</script>
