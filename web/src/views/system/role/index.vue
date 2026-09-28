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
          <!-- 3 个按钮在窄列里会挤：MobileAction 在 <1024px 收成「更多」下拉，
               ≥1024px 按同一份 actions 生成按钮（不用页面再写一遍） -->
          <MobileAction
            :actions="[
              { label: '编辑', icon: 'Edit', type: 'primary' },
              { label: '权限', icon: 'Key', type: 'warning' },
              { label: '删除', icon: 'Delete', type: 'danger' },
            ]"
            @command="(cmd: string) => handleAction(cmd, row as RoleItem)"
          />
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
  getRoleById,
  createRole,
  updateRole,
  deleteRole,
  type RoleItem,
  type RoleQuery,
} from '@/api/role'
import CollapsibleFilter from '@/components/CollapsibleFilter/index.vue'
import FormDialog from '@/components/FormDialog/index.vue'
import MobileAction from '@/components/MobileAction/index.vue'
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

/**
 * MobileAction 的下拉分发：窄屏时操作收进「更多」，
 * 点击后按 label 回到原来那个按钮的处理函数。
 * 桌面端不走这里 —— MobileAction 在 ≥1024px 直接渲染默认插槽，
 * 而本页默认插槽里已经没有按钮了，所以桌面端也走这条分支（见组件实现）。
 */
function handleAction(cmd: string, row: RoleItem) {
  switch (cmd) {
    case '编辑':
      handleEdit(row)
      break
    case '权限':
      handlePermission(row)
      break
    case '删除':
      handleDelete(row)
      break
  }
}

async function handlePermission(row: RoleItem) {
  currentRoleId.value = row.id
  permLoading.value = true
  try {
    // 必须同时取「菜单树」与「该角色已授权的 menuIds」。
    // 早前只取菜单树、并把 checkedMenuIds 设为 row.menuIds —— 而列表接口
    // 不返回该字段，于是勾选恒为空，用户点「确定」就会提交空数组，
    // 把该角色已有的授权整体清空（静默的权限丢失）。
    const [treeRes, roleRes] = await Promise.all([getMenuTree(), getRoleById(row.id)])
    menuTree.value = treeRes.data ?? []

    // 只回显**叶子**节点，父节点交给 el-tree 自行推导「全选/半选」。
    //
    // 原因：提交时写入的 menuIds 里同时含父节点（见 handlePermSubmit 合并了
    // getHalfCheckedKeys），而 el-tree 在 check-strictly=false 下会把父节点的
    // 选中状态**级联到它的全部子节点** —— 若把父 ID 一并回填，角色原本没有的
    // 权限会被静默勾上，等于扩权。只填叶子，则「回显 → 提交」正好互为逆运算。
    const leafIds = new Set(collectLeafIds(menuTree.value))
    checkedMenuIds.value = (roleRes.data.menuIds ?? []).filter((id) => leafIds.has(id))
  } catch (err) {
    // 取不到就**不打开弹窗**：空树 + 空勾选一旦被提交，结果就是清空授权，
    // 比「点了没反应」严重得多。给出明确提示让用户重试。
    menuTree.value = []
    checkedMenuIds.value = []
    ElMessage.error('加载角色权限失败，请稍后重试')
    console.warn('[role] 加载菜单树或角色授权失败', err)
    return
  } finally {
    permLoading.value = false
  }
  permDialogVisible.value = true
}

/** 递归收集菜单树的叶子节点 ID（父节点的勾选状态由 el-tree 推导）。 */
function collectLeafIds(nodes: MenuItem[], out: number[] = []): number[] {
  for (const node of nodes) {
    const children = node.children ?? []
    if (children.length > 0) {
      collectLeafIds(children, out)
    } else {
      out.push(node.id)
    }
  }
  return out
}

async function handlePermSubmit() {
  // 兜底：菜单树为空时提交出去等于清空授权，直接拦住
  if (menuTree.value.length === 0) {
    ElMessage.warning('菜单树未加载完成，请关闭弹窗后重试')
    return
  }
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
