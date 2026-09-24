<template>
  <div class="app-container">
    <el-card class="table-card">
      <template #header>
        <div class="card-header">
          <span>部门管理</span>
          <el-button type="primary" @click="handleAdd()">新增部门</el-button>
        </div>
      </template>

      <el-table
        :data="tableData"
        v-loading="loading"
        border
        row-key="id"
        :tree-props="{ children: 'children' }"
        default-expand-all
      >
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
            <el-button type="primary" link size="small" @click="handleAdd({ parentId: row.id })"
              >新增</el-button
            >
            <el-button type="primary" link size="small" @click="handleEdit(row as DeptItem)"
              >编辑</el-button
            >
            <el-button type="danger" link size="small" @click="handleDelete(row as DeptItem)"
              >删除</el-button
            >
          </template>
        </el-table-column>
      </el-table>
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
import { useCrud } from '@/hooks/useCrud'

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
</script>

<style lang="scss" scoped>
.table-card {
  :deep(.el-card__header) {
    border-bottom-color: var(--color-border-lighter);
  }
}
</style>
