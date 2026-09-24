<template>
  <div class="app-container">
    <el-card class="table-card">
      <template #header>
        <div class="card-header">
          <span>菜单管理</span>
          <el-button type="primary" @click="handleAdd()">新增菜单</el-button>
        </div>
      </template>

      <el-table :data="tableData" v-loading="loading" border row-key="id" :tree-props="{ children: 'children' }" default-expand-all>
        <el-table-column prop="title" label="菜单名称" min-width="150" />
        <el-table-column prop="icon" label="图标" width="80">
          <template #default="{ row }">
            <el-icon v-if="row.icon"><component :is="row.icon" /></el-icon>
          </template>
        </el-table-column>
        <el-table-column prop="type" label="类型" width="80">
          <template #default="{ row }">
            <el-tag v-if="row.type === 0" type="warning">目录</el-tag>
            <el-tag v-else-if="row.type === 1" type="success">菜单</el-tag>
            <el-tag v-else type="danger">按钮</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="sort" label="排序" width="80" />
        <el-table-column prop="permission" label="权限标识" min-width="120" />
        <el-table-column prop="path" label="路由地址" min-width="120" />
        <el-table-column prop="component" label="组件路径" min-width="120" />
        <el-table-column prop="status" label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'danger'">
              {{ row.status === 1 ? '正常' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="handleAdd({ parentId: row.id })">新增</el-button>
            <el-button type="primary" link size="small" @click="handleEdit(row as MenuItem)">编辑</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row as MenuItem)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <FormDialog v-model="dialogVisible" :title="dialogTitle" :loading="submitLoading" @submit="handleSubmit">
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="80px">
        <el-form-item label="上级菜单">
          <el-tree-select v-model="form.parentId" :data="menuOptions" :props="{ label: 'title', value: 'id' } as any" placeholder="请选择上级菜单" check-strictly clearable />
        </el-form-item>
        <el-form-item label="菜单类型" prop="type">
          <el-radio-group v-model="form.type">
            <el-radio :value="0">目录</el-radio>
            <el-radio :value="1">菜单</el-radio>
            <el-radio :value="2">按钮</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="菜单名称" prop="title">
          <el-input v-model="form.title" placeholder="请输入菜单名称" />
        </el-form-item>
        <el-form-item v-if="form.type !== 2" label="路由地址" prop="path">
          <el-input v-model="form.path" placeholder="请输入路由地址" />
        </el-form-item>
        <el-form-item v-if="form.type === 1" label="组件路径" prop="component">
          <el-input v-model="form.component" placeholder="如: system/user/index" />
        </el-form-item>
        <el-form-item v-if="form.type === 2" label="权限标识" prop="permission">
          <el-input v-model="form.permission" placeholder="如: system:user:list" />
        </el-form-item>
        <el-form-item v-if="form.type !== 2" label="图标">
          <el-input v-model="form.icon" placeholder="请输入图标名称" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort" :min="0" />
        </el-form-item>
        <el-form-item v-if="form.type !== 2" label="是否可见">
          <el-radio-group v-model="form.visible">
            <el-radio :value="1">显示</el-radio>
            <el-radio :value="0">隐藏</el-radio>
          </el-radio-group>
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
import { getMenuTree, createMenu, updateMenu, deleteMenu, type MenuItem } from '@/api/menu'
import FormDialog from '@/components/FormDialog/index.vue'
import { useCrud } from '@/hooks/useCrud'

interface MenuForm {
  id: number
  parentId: number
  title: string
  name: string
  path: string
  component: string
  icon: string
  type: number
  permission: string
  sort: number
  visible: number
  status: number
  isCache: number
  isExternal: number
}

// 同 dept：树选择器的合成根节点只带 id/title/children
type MenuOption = Pick<MenuItem, 'id' | 'title' | 'children'>

const menuOptions = ref<MenuOption[]>([])

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
} = useCrud<MenuItem, MenuForm, Record<string, unknown>>({
  // 菜单是整棵树，不分页
  list: () => getMenuTree(),
  create: (payload) => createMenu(payload),
  update: (payload) => updateMenu(payload),
  remove: (id) => deleteMenu(id),
  createForm: () => ({
    id: 0,
    parentId: 0,
    title: '',
    name: '',
    path: '',
    component: '',
    icon: '',
    type: 1,
    permission: '',
    sort: 0,
    visible: 1,
    status: 1,
    isCache: 1,
    isExternal: 0,
  }),
  pagination: false,
  // 树选择器的选项由同一棵树派生，放在 afterLoad 里才能保证
  // 新增/删除菜单后下拉同步刷新
  afterLoad: (rows) => {
    menuOptions.value = [{ id: 0, title: '根目录', children: rows }]
  },
  titles: { add: '新增菜单', edit: '编辑菜单' },
  deleteConfirm: '确认删除该菜单？',
})

const formRules = {
  title: [{ required: true, message: '请输入菜单名称', trigger: 'blur' }],
}
</script>

<style lang="scss" scoped>
.table-card {
  :deep(.el-card__header) {
    border-bottom-color: var(--color-border-lighter);
  }
}
</style>
