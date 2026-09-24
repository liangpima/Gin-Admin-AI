<template>
  <div class="app-container">
    <div class="search-form">
      <el-form :model="queryParams">
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="角色名称">
              <el-input v-model="queryParams.name" placeholder="请输入角色名称" clearable @keyup.enter="handleSearch" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="角色编码">
              <el-input v-model="queryParams.code" placeholder="请输入角色编码" clearable @keyup.enter="handleSearch" />
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
          <span>角色列表</span>
          <el-button type="primary" @click="handleAdd">新增角色</el-button>
        </div>
      </template>

      <el-table :data="tableData" v-loading="loading" border>
        <el-table-column prop="name" label="角色名称" min-width="100" />
        <el-table-column prop="code" label="角色编码" min-width="100" />
        <el-table-column prop="sort" label="排序" width="60" />
        <el-table-column prop="status" label="状态" width="70">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">
              {{ row.status === 1 ? '正常' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="200">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="handleEdit(row as RoleItem)">编辑</el-button>
            <el-button type="primary" link size="small" @click="handlePermission(row as RoleItem)">权限</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row as RoleItem)">删除</el-button>
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

    <FormDialog v-model="dialogVisible" :title="dialogTitle" :loading="submitLoading" @submit="handleSubmit">
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="80px">
        <el-form-item label="角色名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入角色名称" />
        </el-form-item>
        <el-form-item label="角色编码" prop="code">
          <el-input v-model="form.code" :disabled="!!form.id" placeholder="请输入角色编码" />
        </el-form-item>
        <el-form-item label="排序" prop="sort">
          <el-input-number v-model="form.sort" :min="0" />
        </el-form-item>
        <el-form-item label="状态">
          <el-radio-group v-model="form.status">
            <el-radio :value="1">正常</el-radio>
            <el-radio :value="0">停用</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" placeholder="请输入备注" />
        </el-form-item>
      </el-form>
    </FormDialog>

    <FormDialog v-model="permDialogVisible" title="分配权限" :loading="permLoading" @submit="handlePermSubmit">
      <el-tree
        ref="menuTreeRef"
        :data="menuTree"
        :props="{ label: 'title', children: 'children' }"
        show-checkbox
        node-key="id"
        :default-checked-keys="checkedMenuIds"
      />
    </FormDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox, ElTree, type FormInstance } from 'element-plus'
import { getRoleList, createRole, updateRole, deleteRole, type RoleItem } from '@/api/role'
import FormDialog from '@/components/FormDialog/index.vue'
import { formatDateTime } from '@/utils/format'
import { getMenuTree, type MenuItem } from '@/api/menu'

const loading = ref(false)
const submitLoading = ref(false)
const tableData = ref<RoleItem[]>([])
const total = ref(0)
const dialogVisible = ref(false)
const dialogTitle = ref('')
const formRef = ref<FormInstance>()

const permDialogVisible = ref(false)
const permLoading = ref(false)
const menuTree = ref<MenuItem[]>([])
const checkedMenuIds = ref<number[]>([])
const currentRoleId = ref(0)
// el-tree 实例类型：直接用 InstanceType 取，避免手写一份不完整的接口
const menuTreeRef = ref<InstanceType<typeof ElTree>>()

const queryParams = reactive({
  name: '',
  code: '',
  page: 1,
  pageSize: 10,
})

const form = reactive({
  id: 0,
  name: '',
  code: '',
  sort: 0,
  status: 1,
  remark: '',
})

const formRules = {
  name: [{ required: true, message: '请输入角色名称', trigger: 'blur' }],
  code: [{ required: true, message: '请输入角色编码', trigger: 'blur' }],
}

async function loadData() {
  loading.value = true
  try {
    const res = await getRoleList(queryParams)
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
  queryParams.code = ''
  handleSearch()
}

function resetForm() {
  form.id = 0
  form.name = ''
  form.code = ''
  form.sort = 0
  form.status = 1
  form.remark = ''
}

function handleAdd() {
  resetForm()
  dialogTitle.value = '新增角色'
  dialogVisible.value = true
}

function handleEdit(row: RoleItem) {
  resetForm()
  Object.assign(form, row)
  dialogTitle.value = '编辑角色'
  dialogVisible.value = true
}

async function handleSubmit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  submitLoading.value = true
  try {
    if (form.id) {
      await updateRole(form)
    } else {
      await createRole(form)
    }
    ElMessage.success('操作成功')
    dialogVisible.value = false
    loadData()
  } finally {
    submitLoading.value = false
  }
}

async function handleDelete(row: RoleItem) {
  await ElMessageBox.confirm('确认删除该角色？', '提示', { type: 'warning' })
  await deleteRole(row.id)
  ElMessage.success('删除成功')
  loadData()
}

async function handlePermission(row: RoleItem) {
  currentRoleId.value = row.id
  checkedMenuIds.value = row.menuIds || []
  try {
    const res = await getMenuTree()
    menuTree.value = res.data
  } catch (err) {
    // 菜单树取不到就没法分配权限，但仍要打开弹窗并给出空态，
    // 否则用户点了「分配权限」没有任何反应，比看到空列表更困惑
    console.warn('[role] 菜单树加载失败，权限分配将无菜单可选', err)
  }
  permDialogVisible.value = true
}

async function handlePermSubmit() {
  permLoading.value = true
  try {
    const checkedKeys = menuTreeRef.value?.getCheckedKeys() || []
    const halfCheckedKeys = menuTreeRef.value?.getHalfCheckedKeys() || []
    // el-tree 的 key 类型是 string | number（取决于 data 里的 node-key 实际类型）。
    // 本项目的菜单 id 是数字，这里显式转回 number，避免把字符串 ID 传给后端。
    const menuIds = [...checkedKeys, ...halfCheckedKeys].map((k) => Number(k))
    await updateRole({ id: currentRoleId.value, menuIds })
    ElMessage.success('权限分配成功')
    permDialogVisible.value = false
    loadData()
  } finally {
    permLoading.value = false
  }
}

onMounted(() => { loadData() })
</script>

<style lang="scss" scoped>
.table-card {
  :deep(.el-card__header) {
    border-bottom-color: var(--color-border-lighter);
  }
}
</style>
