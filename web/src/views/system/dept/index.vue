<template>
  <div class="app-container">
    <el-card class="table-card">
      <template #header>
        <div class="card-header">
          <span>部门管理</span>
          <el-button type="primary" @click="handleAdd()">新增部门</el-button>
        </div>
      </template>

      <el-table :data="tableData" v-loading="loading" border row-key="id" :tree-props="{ children: 'children' }" default-expand-all>
        <el-table-column prop="name" label="部门名称" min-width="120" />
        <el-table-column prop="leader" label="负责人" min-width="80" />
        <el-table-column prop="phone" label="联系电话" min-width="100" />
        <el-table-column prop="sort" label="排序" width="60" />
        <el-table-column prop="status" label="状态" width="70">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">
              {{ row.status === 1 ? '正常' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="handleAdd(row.id)">新增</el-button>
            <el-button type="primary" link size="small" @click="handleEdit(row as DeptItem)">编辑</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row as DeptItem)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <FormDialog v-model="dialogVisible" :title="dialogTitle" :loading="submitLoading" @submit="handleSubmit">
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="80px">
        <el-form-item label="上级部门">
          <el-tree-select v-model="form.parentId" :data="deptOptions" :props="{ label: 'name', value: 'id' } as any" placeholder="请选择上级部门" check-strictly clearable />
        </el-form-item>
        <el-form-item label="部门名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入部门名称" />
        </el-form-item>
        <el-form-item label="负责人">
          <el-input v-model="form.leader" placeholder="请输入负责人" />
        </el-form-item>
        <el-form-item label="联系电话">
          <el-input v-model="form.phone" placeholder="请输入联系电话" />
        </el-form-item>
        <el-form-item label="邮箱">
          <el-input v-model="form.email" placeholder="请输入邮箱" />
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
      </el-form>
    </FormDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'
import { getDeptTree, createDept, updateDept, deleteDept , type DeptItem} from '@/api/dept'
import FormDialog from '@/components/FormDialog/index.vue'

const loading = ref(false)
const submitLoading = ref(false)
const tableData = ref<DeptItem[]>([])
const dialogVisible = ref(false)
const dialogTitle = ref('')
const formRef = ref<FormInstance>()
// 树选择器的选项只需要 id + 显示名 + children。
// 单独定义而不是复用 DeptItem：合成出来的「根部门」节点没有
// parentId/sort/leader 等字段，为了凑类型给它编造无意义的值是自欺欺人。
type DeptOption = Pick<DeptItem, 'id' | 'name' | 'children'>

const deptOptions = ref<DeptOption[]>([])

const form = reactive({
  id: 0,
  parentId: 0,
  name: '',
  leader: '',
  phone: '',
  email: '',
  sort: 0,
  status: 1,
})

const formRules = {
  name: [{ required: true, message: '请输入部门名称', trigger: 'blur' }],
}

async function loadData() {
  loading.value = true
  try {
    const res = await getDeptTree()
    tableData.value = res.data
    deptOptions.value = [{ id: 0, name: '根部门', children: res.data }]
  } finally {
    loading.value = false
  }
}

function resetForm(parentId = 0) {
  form.id = 0
  form.parentId = parentId
  form.name = ''
  form.leader = ''
  form.phone = ''
  form.email = ''
  form.sort = 0
  form.status = 1
}

function handleAdd(parentId = 0) {
  resetForm(parentId)
  dialogTitle.value = '新增部门'
  dialogVisible.value = true
}

function handleEdit(row: DeptItem) {
  resetForm()
  Object.assign(form, row)
  dialogTitle.value = '编辑部门'
  dialogVisible.value = true
}

async function handleSubmit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  submitLoading.value = true
  try {
    if (form.id) {
      await updateDept(form)
    } else {
      await createDept(form)
    }
    ElMessage.success('操作成功')
    dialogVisible.value = false
    loadData()
  } finally {
    submitLoading.value = false
  }
}

async function handleDelete(row: DeptItem) {
  await ElMessageBox.confirm('确认删除该部门？', '提示', { type: 'warning' })
  await deleteDept(row.id)
  ElMessage.success('删除成功')
  loadData()
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
