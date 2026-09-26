<template>
  <div class="app-container">
    <el-card class="table-card">
      <template #header>
        <div class="card-header">
          <span>参数管理</span>
          <el-button type="primary" @click="handleAdd()">新增配置</el-button>
        </div>
      </template>
      <ResponsiveTable :data="tableData" :columns="columns" :loading="loading">
        <template #type="{ row }">
          <el-tag :type="row.type === 0 ? 'danger' : 'info'" size="small">{{
            row.type === 0 ? '是' : '否'
          }}</el-tag>
        </template>
        <template #createdAt="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        <template #actions="{ row }">
          <el-button type="primary" link size="small" @click="handleEdit(row as ConfigItem)"
            >编辑</el-button
          >
          <el-button type="danger" link size="small" @click="handleDelete(row as ConfigItem)"
            >删除</el-button
          >
        </template>
      </ResponsiveTable>
      <Pagination
        v-model:page="page"
        v-model:limit="pageSize"
        :total="total"
        layout="total, prev, pager, next"
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
        <el-form-item label="参数名称" prop="name"
          ><el-input v-model="form.name" placeholder="请输入参数名称"
        /></el-form-item>
        <el-form-item label="参数键名" prop="key"
          ><el-input v-model="form.key" :disabled="!!form.id" placeholder="请输入参数键名"
        /></el-form-item>
        <el-form-item label="参数键值" prop="value"
          ><el-input v-model="form.value" type="textarea" placeholder="请输入参数键值"
        /></el-form-item>
        <el-form-item label="系统内置">
          <el-radio-group v-model="form.type"
            ><el-radio :value="0">是</el-radio><el-radio :value="1">否</el-radio></el-radio-group
          >
        </el-form-item>
      </el-form>
    </FormDialog>
  </div>
</template>

<script setup lang="ts">
import {
  getConfigList,
  createConfig,
  updateConfig,
  deleteConfig,
  type ConfigItem,
  type ConfigQuery,
} from '@/api/config'
import FormDialog from '@/components/FormDialog/index.vue'
import ResponsiveTable from '@/components/ResponsiveTable/index.vue'
import type { ResponsiveColumn } from '@/components/ResponsiveTable/types'
import { formatDateTime } from '@/utils/format'
import { useCrud } from '@/hooks/useCrud'

// 列定义是唯一来源：桌面端表格列与手机端卡片字段都从这里派生
const columns: ResponsiveColumn<ConfigItem>[] = [
  { label: '参数名称', prop: 'name', minWidth: 100 },
  { label: '参数键名', prop: 'key', minWidth: 120 },
  { label: '参数键值', prop: 'value', minWidth: 100, showOverflowTooltip: true },
  { label: '系统内置', slot: 'type', width: 90 },
  { label: '创建时间', slot: 'createdAt', width: 170 },
  { label: '操作', slot: 'actions', width: 160, hideInCard: true },
]

interface ConfigForm {
  id: number
  name: string
  key: string
  value: string
  type: number
}

// 分页、loading、弹窗开关与增删改查的编排都交给 useCrud；
// 这里只保留本页特有的东西：表单结构、校验规则。
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
} = useCrud<ConfigItem, ConfigForm, ConfigQuery>({
  list: (params) => getConfigList(params),
  create: (payload) => createConfig(payload),
  update: (payload) => updateConfig(payload),
  remove: (id) => deleteConfig(id),
  createForm: () => ({ id: 0, name: '', key: '', value: '', type: 1 }),
  titles: { add: '新增配置', edit: '编辑配置' },
  deleteConfirm: '确认删除？',
})

const formRules = {
  name: [{ required: true, message: '请输入参数名称', trigger: 'blur' }],
  key: [{ required: true, message: '请输入参数键名', trigger: 'blur' }],
}
</script>

<style lang="scss" scoped>
.table-card {
  :deep(.el-card__header) {
    border-bottom-color: var(--color-border-lighter);
  }
}
</style>
