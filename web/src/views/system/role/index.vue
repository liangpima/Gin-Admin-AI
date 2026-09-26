<template>
  <div class="app-container">
    <div class="search-form">
      <CollapsibleFilter>
        <el-form :model="queryParams">
          <el-row :gutter="16">
            <el-col :xs="24" :sm="12" :md="8" :lg="6">
              <el-form-item label="角色名称">
                <el-input
                  v-model="queryParams.name"
                  placeholder="请输入角色名称"
                  clearable
                  @keyup.enter="handleSearch"
                />
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :md="8" :lg="6">
              <el-form-item label="角色编码">
                <el-input
                  v-model="queryParams.code"
                  placeholder="请输入角色编码"
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
          <span>角色列表</span>
          <el-button type="primary" @click="handleAdd()">新增角色</el-button>
        </div>
      </template>

      <ResponsiveTable :data="tableData" :columns="columns" :loading="loading">
        <template #status="{ row }">
          <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">
            {{ row.status === 1 ? '正常' : '停用' }}
          </el-tag>
        </template>
        <template #createdAt="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        <template #actions="{ row }">
          <el-button type="primary" link size="small" @click="handleEdit(row as RoleItem)"
            >编辑</el-button
          >
          <el-button type="primary" link size="small" @click="handlePermission(row as RoleItem)"
            >权限</el-button
          >
          <el-button type="danger" link size="small" @click="handleDelete(row as RoleItem)"
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
      :loading="submitLoading"
      @submit="handleSubmit"
    >
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

    <FormDialog
      v-model="permDialogVisible"
      title="分配权限"
      :loading="permLoading"
      @submit="handlePermSubmit"
    >
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
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import type { TreeInstance } from 'element-plus'
import {
  getRoleList,
  createRole,
  updateRole,
  deleteRole,
  type RoleItem,
  type RoleQuery,
} from '@/api/role'
import CollapsibleFilter from '@/components/CollapsibleFilter/index.vue'
import FormDialog from '@/components/FormDialog/index.vue'
import ResponsiveTable from '@/components/ResponsiveTable/index.vue'
import type { ResponsiveColumn } from '@/components/ResponsiveTable/types'
import { formatDateTime } from '@/utils/format'
import { getMenuTree, type MenuItem } from '@/api/menu'
import { useCrud } from '@/hooks/useCrud'

// 列定义是唯一来源：桌面端表格列与手机端卡片字段都从这里派生
const columns: ResponsiveColumn<RoleItem>[] = [
  { label: '角色名称', prop: 'name', minWidth: 100 },
  { label: '角色编码', prop: 'code', minWidth: 100 },
  { label: '排序', prop: 'sort', width: 60 },
  { label: '状态', slot: 'status', width: 70 },
  { label: '创建时间', slot: 'createdAt', width: 170 },
  { label: '操作', slot: 'actions', width: 200, hideInCard: true },
]

interface RoleForm {
  id: number
  name: string
  code: string
  sort: number
  status: number
  remark: string
}

// 搜索条件只放本页自己的字段；page/pageSize 由 useCrud 管理
const queryParams = reactive({ name: '', code: '' })

const permDialogVisible = ref(false)
const permLoading = ref(false)
const menuTree = ref<MenuItem[]>([])
const checkedMenuIds = ref<number[]>([])
const currentRoleId = ref(0)
// el-tree 实例类型：用 Element Plus 导出的 TreeInstance，避免手写一份不完整的接口。
//
// ⚠️ 这里必须是 **type-only** import。写成 `import { ElTree } from 'element-plus'`
// 会让 unplugin-vue-components 认为该组件已在本文件引入，从而跳过对模板里
// <el-tree> 的解析 —— 连带丢掉 tree 的样式（症状是权限树没有连接线、勾选框错位，
// 而且**不报错**）。P3-A2 去掉全量 CSS 后才暴露出来。
const menuTreeRef = ref<TreeInstance>()

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
} = useCrud<RoleItem, RoleForm, RoleQuery>({
  list: (params) => getRoleList(params),
  create: (payload) => createRole(payload),
  update: (payload) => updateRole(payload),
  remove: (id) => deleteRole(id),
  createForm: () => ({ id: 0, name: '', code: '', sort: 0, status: 1, remark: '' }),
  query: () => ({ name: queryParams.name, code: queryParams.code }),
  titles: { add: '新增角色', edit: '编辑角色' },
  deleteConfirm: '确认删除该角色？',
})

function handleReset() {
  queryParams.name = ''
  queryParams.code = ''
  handleSearch()
}

const formRules = {
  name: [{ required: true, message: '请输入角色名称', trigger: 'blur' }],
  code: [{ required: true, message: '请输入角色编码', trigger: 'blur' }],
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
</script>

<style lang="scss" scoped>
.table-card {
  :deep(.el-card__header) {
    border-bottom-color: var(--color-border-lighter);
  }
}
</style>
