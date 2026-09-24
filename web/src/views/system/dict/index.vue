<template>
  <div class="app-container">
    <el-row :gutter="16">
      <el-col :xs="24" :sm="24" :md="10" class="dict-type-col">
        <el-card class="table-card">
          <template #header>
            <div class="card-header">
              <span>字典类型</span>
              <el-button type="primary" @click="handleAddType">新增</el-button>
            </div>
          </template>
          <el-table
            :data="typeList"
            v-loading="typeLoading"
            border
            stripe
            highlight-current-row
            @current-change="handleTypeChange"
          >
            <el-table-column prop="id" label="ID" width="60" />
            <el-table-column prop="name" label="字典名称" min-width="100" />
            <el-table-column prop="type" label="字典类型" min-width="120" show-overflow-tooltip />
            <el-table-column prop="status" label="状态" width="70">
              <template #default="{ row }">
                <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">{{
                  row.status === 1 ? '正常' : '停用'
                }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="120">
              <template #default="{ row }">
                <el-button
                  type="primary"
                  link
                  size="small"
                  @click="handleEditType(row as DictTypeItem)"
                  >编辑</el-button
                >
                <el-button
                  type="danger"
                  link
                  size="small"
                  @click="handleDeleteType(row as DictTypeItem)"
                  >删除</el-button
                >
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
      <el-col :xs="24" :sm="24" :md="14">
        <el-card class="table-card">
          <template #header>
            <div class="card-header">
              <span>字典数据 {{ currentType ? `- ${currentType.name}` : '' }}</span>
              <el-button type="primary" :disabled="!currentType" @click="handleAddData"
                >新增</el-button
              >
            </div>
          </template>
          <el-table :data="dataList" v-loading="dataLoading" border stripe>
            <el-table-column prop="id" label="ID" width="60" />
            <el-table-column prop="label" label="字典标签" min-width="100" />
            <el-table-column prop="value" label="字典键值" min-width="80" />
            <el-table-column prop="sort" label="排序" width="60" />
            <el-table-column label="回显样式" width="100">
              <template #default="{ row }">
                <el-tag :type="tagTypeOf(row.listClass)" size="small">{{
                  row.listClass || '默认'
                }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="status" label="状态" width="70">
              <template #default="{ row }">
                <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">{{
                  row.status === 1 ? '正常' : '停用'
                }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="120">
              <template #default="{ row }">
                <el-button
                  type="primary"
                  link
                  size="small"
                  @click="handleEditData(row as DictDataItem)"
                  >编辑</el-button
                >
                <el-button
                  type="danger"
                  link
                  size="small"
                  @click="handleDeleteData(row as DictDataItem)"
                  >删除</el-button
                >
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    <FormDialog
      v-model="typeDialogVisible"
      :title="typeDialogTitle"
      :loading="submitting"
      @submit="handleTypeSubmit"
    >
      <el-form ref="typeFormRef" :model="typeForm" :rules="typeFormRules" label-width="80px">
        <el-form-item label="字典名称" prop="name"
          ><el-input v-model="typeForm.name" placeholder="请输入字典名称"
        /></el-form-item>
        <el-form-item label="字典类型" prop="type">
          <el-input
            v-model="typeForm.type"
            :disabled="isEditType"
            placeholder="小写字母、数字、下划线，如 sys_user_status"
          />
          <!-- 编码是业务代码里的字面量（useDict('...')），改掉会让已接入的页面取不到数据，
               因此编辑时不允许修改，需要换编码就新建一个类型 -->
          <div v-if="isEditType" class="form-hint">编码创建后不可修改</div>
        </el-form-item>
        <el-form-item label="状态"
          ><el-switch v-model="typeForm.status" :active-value="1" :inactive-value="0"
        /></el-form-item>
        <el-form-item label="备注"
          ><el-input v-model="typeForm.remark" type="textarea" placeholder="选填"
        /></el-form-item>
      </el-form>
    </FormDialog>

    <FormDialog
      v-model="dataDialogVisible"
      :title="dataDialogTitle"
      :loading="submitting"
      @submit="handleDataSubmit"
    >
      <el-form ref="dataFormRef" :model="dataForm" :rules="dataFormRules" label-width="80px">
        <el-form-item label="字典标签" prop="label"
          ><el-input v-model="dataForm.label" placeholder="展示给用户的文本，如「正常」"
        /></el-form-item>
        <el-form-item label="字典键值" prop="value"
          ><el-input v-model="dataForm.value" placeholder="实际存储的值，如 1"
        /></el-form-item>
        <el-form-item label="排序"
          ><el-input-number v-model="dataForm.sort" :min="0"
        /></el-form-item>
        <el-form-item label="回显样式">
          <el-select v-model="dataForm.listClass" placeholder="默认" clearable style="width: 100%">
            <el-option
              v-for="opt in tagTypeOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="状态"
          ><el-switch v-model="dataForm.status" :active-value="1" :inactive-value="0"
        /></el-form-item>
        <el-form-item label="备注"
          ><el-input v-model="dataForm.remark" type="textarea" placeholder="选填"
        /></el-form-item>
      </el-form>
    </FormDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'
import {
  getDictTypeList,
  createDictType,
  updateDictType,
  deleteDictType,
  getDictDataList,
  createDictData,
  updateDictData,
  deleteDictData,
  type DictTypeItem,
  type DictDataItem,
} from '@/api/dict'
import FormDialog from '@/components/FormDialog/index.vue'
import { clearDictCache } from '@/hooks/useDict'

type TagType = 'primary' | 'success' | 'info' | 'warning' | 'danger'

const VALID_TAG_TYPES: TagType[] = ['primary', 'success', 'info', 'warning', 'danger']

const tagTypeOptions = [
  { label: '主要', value: 'primary' },
  { label: '成功', value: 'success' },
  { label: '信息', value: 'info' },
  { label: '警告', value: 'warning' },
  { label: '危险', value: 'danger' },
]

/** 字典里配了非法样式（如中文）时兜底成 info，避免标签渲染成无样式 */
function tagTypeOf(listClass?: string): TagType {
  return VALID_TAG_TYPES.includes(listClass as TagType) ? (listClass as TagType) : 'info'
}

const typeLoading = ref(false)
const dataLoading = ref(false)
const submitting = ref(false)
const typeList = ref<DictTypeItem[]>([])
const dataList = ref<DictDataItem[]>([])
const currentType = ref<DictTypeItem | null>(null)

const typeDialogVisible = ref(false)
const dataDialogVisible = ref(false)
const editingTypeId = ref(0)
const editingDataId = ref(0)
const typeFormRef = ref<FormInstance>()
const dataFormRef = ref<FormInstance>()

const typeForm = reactive({ name: '', type: '', status: 1 as number, remark: '' })
const dataForm = reactive({
  label: '',
  value: '',
  sort: 0,
  listClass: '',
  status: 1 as number,
  remark: '',
})

const typeFormRules = {
  name: [{ required: true, message: '必填' }],
  type: [{ required: true, message: '必填' }],
}
const dataFormRules = {
  label: [{ required: true, message: '必填' }],
  value: [{ required: true, message: '必填' }],
}

const isEditType = computed(() => editingTypeId.value > 0)
const typeDialogTitle = computed(() => (isEditType.value ? '编辑字典类型' : '新增字典类型'))
const dataDialogTitle = computed(() => (editingDataId.value > 0 ? '编辑字典数据' : '新增字典数据'))

async function loadTypes() {
  typeLoading.value = true
  try {
    const res = await getDictTypeList({ page: 1, pageSize: 100 })
    typeList.value = res.data.list
    // 保持选中项与列表同步：编辑后名称/状态会变，不刷新的话右栏标题还是旧值
    if (currentType.value) {
      currentType.value = typeList.value.find((t) => t.id === currentType.value?.id) ?? null
    }
  } finally {
    typeLoading.value = false
  }
}

async function loadData() {
  if (!currentType.value) {
    dataList.value = []
    return
  }
  dataLoading.value = true
  try {
    const res = await getDictDataList({ dictType: currentType.value.type, page: 1, pageSize: 100 })
    dataList.value = res.data.list
  } finally {
    dataLoading.value = false
  }
}

function handleTypeChange(row: DictTypeItem | null) {
  currentType.value = row
  loadData()
}

function handleAddType() {
  editingTypeId.value = 0
  Object.assign(typeForm, { name: '', type: '', status: 1, remark: '' })
  typeDialogVisible.value = true
}

function handleEditType(row: DictTypeItem) {
  editingTypeId.value = row.id
  Object.assign(typeForm, {
    name: row.name,
    type: row.type,
    status: row.status,
    remark: row.remark ?? '',
  })
  typeDialogVisible.value = true
}

async function handleTypeSubmit() {
  const valid = await typeFormRef.value?.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    if (isEditType.value) {
      await updateDictType(editingTypeId.value, {
        name: typeForm.name,
        status: typeForm.status,
        remark: typeForm.remark,
      })
    } else {
      await createDictType({ name: typeForm.name, type: typeForm.type })
    }
    ElMessage.success('操作成功')
    typeDialogVisible.value = false
    // 选项可能已变（停用/改名），清缓存让其他页面重新拉取
    clearDictCache()
    await loadTypes()
  } finally {
    submitting.value = false
  }
}

async function handleDeleteType(row: DictTypeItem) {
  // 取消确认框会 reject，不接住会变成未处理的 Promise 异常
  const ok = await ElMessageBox.confirm(`确认删除字典类型「${row.name}」？`, '提示', {
    type: 'warning',
  }).catch(() => false)
  if (!ok) return
  await deleteDictType(row.id)
  ElMessage.success('删除成功')
  clearDictCache()
  if (currentType.value?.id === row.id) {
    currentType.value = null
    dataList.value = []
  }
  await loadTypes()
}

function handleAddData() {
  editingDataId.value = 0
  Object.assign(dataForm, { label: '', value: '', sort: 0, listClass: '', status: 1, remark: '' })
  dataDialogVisible.value = true
}

function handleEditData(row: DictDataItem) {
  editingDataId.value = row.id
  Object.assign(dataForm, {
    label: row.label,
    value: row.value,
    sort: row.sort,
    listClass: row.listClass ?? '',
    status: row.status,
    remark: row.remark ?? '',
  })
  dataDialogVisible.value = true
}

async function handleDataSubmit() {
  const valid = await dataFormRef.value?.validate().catch(() => false)
  if (!valid) return
  if (!currentType.value) return

  submitting.value = true
  try {
    if (editingDataId.value > 0) {
      await updateDictData(editingDataId.value, { ...dataForm })
    } else {
      await createDictData({ ...dataForm, dictType: currentType.value.type })
    }
    ElMessage.success('操作成功')
    dataDialogVisible.value = false
    clearDictCache(currentType.value.type)
    await loadData()
  } finally {
    submitting.value = false
  }
}

async function handleDeleteData(row: DictDataItem) {
  const ok = await ElMessageBox.confirm(`确认删除字典数据「${row.label}」？`, '提示', {
    type: 'warning',
  }).catch(() => false)
  if (!ok) return
  await deleteDictData(row.id)
  ElMessage.success('删除成功')
  if (currentType.value) clearDictCache(currentType.value.type)
  await loadData()
}

onMounted(() => {
  loadTypes()
})
</script>

<style lang="scss" scoped>
@use '@/assets/styles/responsive.scss' as *;

.table-card {
  :deep(.el-card__header) {
    border-bottom-color: var(--color-border-lighter);
  }
}

.form-hint {
  margin-top: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

@include mobile {
  .dict-type-col {
    margin-bottom: var(--spacing-md);
  }
}
</style>
