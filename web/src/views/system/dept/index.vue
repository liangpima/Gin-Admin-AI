<template>
  <div class="app-container">
    <el-card class="table-card">
      <template #header>
        <div class="card-header">
          <span>部门管理</span>
          <el-button type="primary" @click="handleAdd()">新增部门</el-button>
        </div>
      </template>

      <ResponsiveTable :data="tableData" :columns="columns" :loading="loading" tree>
        <template #status="{ row }">
          <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">
            {{ row.status === 1 ? '正常' : '停用' }}
          </el-tag>
        </template>
        <template #actions="{ row }">
          <!-- 3 个按钮在窄列里会挤：MobileAction 在 <1024px 收成「更多」下拉，
               ≥1024px 按同一份 actions 生成按钮 -->
          <MobileAction
            :actions="[
              { label: '新增', icon: 'Plus', type: 'primary' },
              { label: '编辑', icon: 'Edit', type: 'primary' },
              { label: '删除', icon: 'Delete', type: 'danger' },
            ]"
            @command="(cmd: string) => handleAction(cmd, row as DeptItem)"
          />
        </template>
      </ResponsiveTable>
    </el-card>

    <FormDialog
      v-model="dialogVisible"
      :title="dialogTitle"
      :loading="submitLoading"
      @submit="handleSubmit"
    >
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="80px">
        <el-form-item label="上级部门">
          <el-tree-select
            v-model="form.parentId"
            :data="deptOptions"
            :props="{ label: 'name', value: 'id' } as any"
            placeholder="请选择上级部门"
            check-strictly
            clearable
          />
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
import { ref } from 'vue'
import { getDeptTree, createDept, updateDept, deleteDept, type DeptItem } from '@/api/dept'
import FormDialog from '@/components/FormDialog/index.vue'
import MobileAction from '@/components/MobileAction/index.vue'
import ResponsiveTable from '@/components/ResponsiveTable/index.vue'
import type { ResponsiveColumn } from '@/components/ResponsiveTable/types'
import { useCrud } from '@/hooks/useCrud'

// 列定义是唯一来源：桌面端表格列与手机端卡片字段都从这里派生。
// 本页是树形数据，`tree` 让卡片按深度优先摊平（否则子部门在手机上会整个消失）。
const columns: ResponsiveColumn<DeptItem>[] = [
  { label: '部门名称', prop: 'name', minWidth: 120 },
  { label: '负责人', prop: 'leader', minWidth: 80 },
  { label: '联系电话', prop: 'phone', minWidth: 100 },
  { label: '排序', prop: 'sort', width: 60 },
  { label: '状态', slot: 'status', width: 70 },
  { label: '操作', slot: 'actions', width: 180, hideInCard: true },
]

interface DeptForm {
  id: number
  parentId: number
  name: string
  leader: string
  phone: string
  email: string
  sort: number
  status: number
}

// 树选择器的选项只需要 id + 显示名 + children。
// 单独定义而不是复用 DeptItem：合成出来的「根部门」节点没有
// parentId/sort/leader 等字段，为了凑类型给它编造无意义的值是自欺欺人。
type DeptOption = Pick<DeptItem, 'id' | 'name' | 'children'>

const deptOptions = ref<DeptOption[]>([])

const {
  loading,
  submitLoading,
  tableData,
  dialogVisible,
  dialogTitle,
  form,
  formRef,
  handleAdd,
  handleEdit,
  handleSubmit,
  handleDelete,
} = useCrud<DeptItem, DeptForm, Record<string, unknown>>({
  // 部门是整棵树，不分页
  list: () => getDeptTree(),
  create: (payload) => createDept(payload),
  update: (payload) => updateDept(payload),
  remove: (id) => deleteDept(id),
  createForm: () => ({
    id: 0,
    parentId: 0,
    name: '',
    leader: '',
    phone: '',
    email: '',
    sort: 0,
    status: 1,
  }),
  pagination: false,
  // 树选择器的选项由同一棵树派生，放在 afterLoad 里才能保证
  // 新增/删除部门后下拉同步刷新
  afterLoad: (rows) => {
    deptOptions.value = [{ id: 0, name: '根部门', children: rows }]
  },
  titles: { add: '新增部门', edit: '编辑部门' },
  deleteConfirm: '确认删除该部门？',
})

const formRules = {
  name: [{ required: true, message: '请输入部门名称', trigger: 'blur' }],
}

/** MobileAction 的下拉分发：窄屏时操作收进「更多」，点击后按 label 回到原处理函数 */
function handleAction(cmd: string, row: DeptItem) {
  switch (cmd) {
    case '新增':
      handleAdd({ parentId: row.id })
      break
    case '编辑':
      handleEdit(row)
      break
    case '删除':
      handleDelete(row)
      break
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
