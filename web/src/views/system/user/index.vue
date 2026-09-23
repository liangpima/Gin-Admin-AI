<template>
  <div class="app-container">
    <div class="search-form">
      <el-form :model="queryParams">
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="用户名">
              <el-input v-model="queryParams.username" placeholder="请输入用户名" clearable @keyup.enter="handleSearch" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="手机号">
              <el-input v-model="queryParams.phone" placeholder="请输入手机号" clearable @keyup.enter="handleSearch" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="状态">
              <el-select v-model="queryParams.status" placeholder="全部" clearable style="width: 100%">
                <el-option v-for="opt in statusOptions" :key="String(opt.value)" :label="opt.label" :value="opt.value" />
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
          <span>管理员列表</span>
          <div class="card-header__actions">
            <el-button :loading="exporting" @click="handleExport">导出</el-button>
            <el-button type="primary" @click="handleAdd">新增管理员</el-button>
          </div>
        </div>
      </template>

      <el-table :data="tableData" v-loading="loading" border>
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column label="头像" width="60">
          <template #default="{ row }">
            <el-avatar :size="32" :src="row.avatar || undefined">{{ row.username?.charAt(0)?.toUpperCase() }}</el-avatar>
          </template>
        </el-table-column>
        <el-table-column prop="username" label="用户名" width="120" />
        <el-table-column prop="nickname" label="昵称" min-width="120" />
        <el-table-column prop="phone" label="手机号" width="120" />
        <el-table-column prop="email" label="邮箱" min-width="200" show-overflow-tooltip />
        <el-table-column label="用户角色" min-width="200">
          <template #default="{ row }">
            <el-select
              v-model="row.roleIds"
              multiple
              collapse-tags
              collapse-tags-tooltip
              placeholder="请选择角色"
              style="width: 100%"
              @change="(val: number[]) => handleRoleChange(row as UserItem, val)"
            >
              <el-option v-for="role in roleList" :key="role.id" :label="role.name" :value="role.id" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="部门" min-width="180">
          <template #default="{ row }">
            <el-tree-select
              v-model="row.deptId"
              :data="deptTree"
              :props="{ label: 'name', value: 'id' } as any"
              placeholder="选择部门"
              check-strictly
              style="width: 100%"
              @change="(val: number) => handleDeptChange(row as UserItem, val)"
            />
          </template>
        </el-table-column>
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-switch v-model="row.status" :active-value="1" :inactive-value="0" @change="handleStatusChange(row as UserItem)" />
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="200">
          <template #default="{ row }">
            <MobileAction
              :actions="[
                { label: '编辑', icon: 'Edit', color: 'var(--el-color-primary)' },
                { label: '重置密码', icon: 'Key', color: 'var(--el-color-warning)' },
                { label: '删除', icon: 'Delete', color: 'var(--el-color-danger)' }
              ]"
              @command="(cmd: string) => handleAction(cmd, row as UserItem)"
            />
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
        <el-form-item label="头像">
          <div class="logo-upload">
            <div v-if="form.avatar" class="logo-preview" @click="avatarPickerVisible = true">
              <img :src="form.avatar" />
              <div class="logo-preview__mask">更换</div>
            </div>
            <div v-else class="logo-placeholder" @click="avatarPickerVisible = true">
              <el-icon :size="24"><Plus /></el-icon>
              <span>上传头像</span>
            </div>
          </div>
        </el-form-item>
        <el-form-item label="用户名" prop="username">
          <el-input v-model="form.username" :disabled="!!form.id" placeholder="请输入用户名" />
        </el-form-item>
        <el-form-item v-if="!form.id" label="密码" prop="password">
          <el-input v-model="form.password" type="password" placeholder="请输入密码" show-password />
        </el-form-item>
        <el-form-item label="昵称" prop="nickname">
          <el-input v-model="form.nickname" placeholder="请输入昵称" />
        </el-form-item>
        <el-form-item label="手机号" prop="phone">
          <el-input v-model="form.phone" placeholder="请输入手机号" />
        </el-form-item>
        <el-form-item label="邮箱" prop="email">
          <el-input v-model="form.email" placeholder="请输入邮箱" />
        </el-form-item>
        <el-form-item label="部门" prop="deptId">
          <el-tree-select v-model="form.deptId" :data="deptTree" :props="{ label: 'name', value: 'id' } as any" placeholder="请选择部门" check-strictly />
        </el-form-item>
        <el-form-item label="用户角色">
          <el-select v-model="form.roleIds" multiple collapse-tags collapse-tags-tooltip placeholder="请选择角色" style="width: 100%">
            <el-option v-for="role in roleList" :key="role.id" :label="role.name" :value="role.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.status" :active-value="1" :inactive-value="0" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" placeholder="请输入备注" />
        </el-form-item>
      </el-form>
    </FormDialog>

    <ImagePicker v-model:visible="avatarPickerVisible" @confirm="handleAvatarPick" />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { getUserList, createUser, updateUser, deleteUser, resetPassword, updateUserStatus, updateUserRoles, updateUserDept, exportUsers, type UserItem } from '@/api/user'
import ImagePicker from '@/components/ImagePicker/index.vue'
import FormDialog from '@/components/FormDialog/index.vue'
import MobileAction from '@/components/MobileAction/index.vue'
import { formatDateTime } from '@/utils/format'
import { getAllRoles, type RoleItem } from '@/api/role'
import { getDeptTree, type DeptItem } from '@/api/dept'
import { useDict, type DictOption } from '@/hooks/useDict'

const loading = ref(false)
const submitLoading = ref(false)
const tableData = ref<UserItem[]>([])
const total = ref(0)
const dialogVisible = ref(false)
const dialogTitle = ref('')
const formRef = ref<FormInstance>()
const roleList = ref<RoleItem[]>([])
const deptTree = ref<DeptItem[]>([])
const avatarPickerVisible = ref(false)

const queryParams = reactive({
  username: '',
  phone: '',
  status: undefined as number | undefined,
  page: 1,
  pageSize: 10,
})

// 状态选项由数据字典 sys_user_status 驱动：新增状态（如「锁定」）时
// 只需在「系统管理 → 数据字典」里加一条，不用改这个页面。
const { getList: getDictList } = useDict('sys_user_status')

// 字典为空时的兜底。字典是可被运营删除的数据，一旦被清空，
// 筛选框会变成空下拉 —— 用户连「正常/停用」都选不了，属于功能级故障。
const DEFAULT_STATUS_OPTIONS: DictOption[] = [
  { label: '正常', value: '1', listClass: '', cssClass: '' },
  { label: '停用', value: '0', listClass: '', cssClass: '' },
]

const statusOptions = computed(() =>
  // 字典键值在库里是字符串，而 status 查询参数是数字，这里转回数字；
  // 非纯数字的键值原样保留，避免转出 NaN 让查询条件失效
  getDictList('sys_user_status', DEFAULT_STATUS_OPTIONS).map((opt) => ({
    label: opt.label,
    value: /^\d+$/.test(opt.value) ? Number(opt.value) : opt.value,
  })),
)

const form = reactive({
  id: 0,
  username: '',
  password: '',
  nickname: '',
  avatar: '',
  phone: '',
  email: '',
  deptId: 0,
  roleIds: [] as number[],
  status: 1,
  remark: '',
})

const formRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
  deptId: [{ required: true, message: '请选择部门', trigger: 'change' }],
}

async function loadData() {
  loading.value = true
  try {
    const res = await getUserList(queryParams)
    tableData.value = res.data.list.map((item) => ({
      ...item,
      roleIds: item.roles?.map((r) => r.id) || [],
    }))
    total.value = res.data.total
  } finally {
    loading.value = false
  }
}

async function loadRoles() {
  try {
    const res = await getAllRoles()
    roleList.value = res.data
  } catch {}
}

async function loadDepts() {
  try {
    const res = await getDeptTree()
    deptTree.value = res.data
  } catch {}
}

function handleSearch() {
  queryParams.page = 1
  loadData()
}

function handleReset() {
  queryParams.username = ''
  queryParams.phone = ''
  queryParams.status = undefined
  handleSearch()
}

async function handleStatusChange(row: UserItem) {
  try {
    await updateUserStatus({ id: row.id, status: row.status })
    ElMessage.success('状态修改成功')
  } catch {
    row.status = row.status === 1 ? 0 : 1
  }
}

async function handleRoleChange(row: UserItem, roleIds: number[]) {
  try {
    await updateUserRoles({ id: row.id, roleIds })
    ElMessage.success('角色修改成功')
  } catch {
    loadData()
  }
}

async function handleDeptChange(row: UserItem, deptId: number) {
  try {
    await updateUserDept({ id: row.id, deptId })
    ElMessage.success('部门修改成功')
  } catch {
    loadData()
  }
}

function resetForm() {
  form.id = 0
  form.username = ''
  form.password = ''
  form.nickname = ''
  form.avatar = ''
  form.phone = ''
  form.email = ''
  form.deptId = 0
  form.roleIds = []
  form.status = 1
  form.remark = ''
}

const exporting = ref(false)

/**
 * 导出当前筛选条件下的管理员列表。
 *
 * 导出走的是不分页查询，因此只带筛选条件、不带分页参数；
 * 后端有行数上限保护（service.ExportMaxRows）。
 */
async function handleExport() {
  exporting.value = true
  try {
    const res = await exportUsers({
      username: queryParams.username,
      phone: queryParams.phone,
      status: queryParams.status,
    })

    // http 门面已把 axios 的 response 解包，这里拿到的就是 Blob 本体
    const blob = new Blob([res], {
      type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `users_${formatDateTime(new Date()).replace(/[-: ]/g, '')}.xlsx`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(url)

    ElMessage.success('导出成功')
  } catch {
    // 拦截器已提示具体错误（如无权限、导出失败），此处不重复弹窗
  } finally {
    exporting.value = false
  }
}

function handleAdd() {
  resetForm()
  dialogTitle.value = '新增管理员'
  dialogVisible.value = true
}

function handleEdit(row: UserItem) {
  resetForm()
  Object.assign(form, {
    id: row.id,
    username: row.username,
    nickname: row.nickname,
    avatar: row.avatar,
    phone: row.phone,
    email: row.email,
    deptId: row.deptId,
    roleIds: row.roles?.map((r) => r.id) || [],
    status: row.status,
    remark: row.remark,
  })
  dialogTitle.value = '编辑管理员'
  dialogVisible.value = true
}

async function handleSubmit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return

  submitLoading.value = true
  try {
    if (form.id) {
      await updateUser(form)
    } else {
      await createUser(form)
    }
    ElMessage.success('操作成功')
    dialogVisible.value = false
    loadData()
  } finally {
    submitLoading.value = false
  }
}

async function handleDelete(row: UserItem) {
  await ElMessageBox.confirm('确认删除该管理员？', '提示', { type: 'warning' })
  await deleteUser(row.id)
  ElMessage.success('删除成功')
  loadData()
}

async function handleResetPwd(row: UserItem) {
  const { value } = await ElMessageBox.prompt('请输入新密码', '重置密码', {
    inputPattern: /.{6,}/,
    inputErrorMessage: '密码长度不能少于6位',
  })
  await resetPassword({ id: row.id, password: value })
  ElMessage.success('密码已重置')
}

function handleAvatarPick(url: string | string[]) {
  form.avatar = url as string
}

function handleAction(cmd: string, row: UserItem) {
  switch (cmd) {
    case '编辑':
      handleEdit(row)
      break
    case '重置密码':
      handleResetPwd(row)
      break
    case '删除':
      handleDelete(row)
      break
  }
}

onMounted(() => {
  loadData()
  loadRoles()
  loadDepts()
})
</script>

<style lang="scss" scoped>
.logo-upload {
  .logo-preview {
    width: 80px;
    height: 80px;
    border-radius: 8px;
    overflow: hidden;
    cursor: pointer;
    position: relative;
    border: 1px solid var(--el-border-color);

    img {
      width: 100%;
      height: 100%;
      object-fit: contain;
    }

    &__mask {
      position: absolute;
      inset: 0;
      background: rgba(0, 0, 0, 0.5);
      color: #fff;
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 13px;
      opacity: 0;
      transition: opacity 0.2s;
    }

    &:hover .logo-preview__mask {
      opacity: 1;
    }
  }

  .logo-placeholder {
    width: 80px;
    height: 80px;
    border: 1px dashed var(--el-border-color);
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
    cursor: pointer;
    color: var(--el-text-color-secondary);
    font-size: 12px;
    transition: border-color 0.2s;

    &:hover {
      border-color: var(--el-color-primary);
    }
  }
}
.table-card {
  :deep(.el-card__header) {
    border-bottom-color: var(--color-border-lighter);
  }
}
.text-secondary {
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
}
</style>
