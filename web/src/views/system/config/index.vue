<template>
  <div class="app-container">
    <el-card class="table-card">
      <template #header>
        <div class="card-header">
          <span>参数管理</span>
          <el-button type="primary" @click="handleAdd()">新增配置</el-button>
        </div>
      </template>
      <el-table :data="tableData" v-loading="loading" border>
        <el-table-column prop="name" label="参数名称" min-width="100" />
        <el-table-column prop="key" label="参数键名" min-width="120" />
        <el-table-column prop="value" label="参数键值" min-width="100" show-overflow-tooltip />
        <el-table-column prop="type" label="系统内置" width="90">
          <template #default="{ row }">
            <el-tag :type="row.type === 0 ? 'danger' : 'info'" size="small">{{
              row.type === 0 ? '是' : '否'
            }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="170">
          <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="handleEdit(row as ConfigItem)"
              >编辑</el-button
            >
            <el-button type="danger" link size="small" @click="handleDelete(row as ConfigItem)"
              >删除</el-button
            >
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
import { formatDateTime } from '@/utils/format'
import { useCrud } from '@/hooks/useCrud'

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
