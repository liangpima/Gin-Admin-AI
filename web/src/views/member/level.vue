<template>
  <div class="app-container">
    <div class="search-form">
      <el-form :model="queryParams">
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="等级名称">
              <el-input v-model="queryParams.name" placeholder="请输入等级名称" clearable @keyup.enter="handleSearch" />
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
          <span>会员等级</span>
          <el-button type="primary" @click="handleAdd()">新增等级</el-button>
        </div>
      </template>

      <el-table :data="tableData" v-loading="loading" border>
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="name" label="等级名称" min-width="120" />
        <el-table-column prop="minPoints" label="最低积分" width="100" align="right" />
        <el-table-column label="折扣" width="80" align="center">
          <template #default="{ row }">{{ row.discount }}折</template>
        </el-table-column>
        <el-table-column label="图标" width="80" align="center">
          <template #default="{ row }">
            <el-avatar v-if="row.icon" :size="32" :src="row.icon" shape="square" />
            <span v-else style="color: var(--color-text-placeholder)">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="sort" label="排序" width="70" align="center" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">{{ row.status === 1 ? '正常' : '停用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="handleEdit(row as MemberLevelItem)">编辑</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row as MemberLevelItem)">删除</el-button>
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

    <FormDialog v-model="dialogVisible" :title="dialogTitle" :loading="submitLoading" @submit="handleSubmit">
      <el-form ref="formRef" :model="form" :rules="formRules" label-width="100px">
        <el-form-item label="等级名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入等级名称" />
        </el-form-item>
        <el-form-item label="最低积分">
          <el-input-number v-model="form.minPoints" :min="0" :step="100" />
        </el-form-item>
        <el-form-item label="折扣">
          <el-input-number v-model="form.discount" :min="1" :max="10" :step="0.5" :precision="1" />
          <span class="form-hint">如 9.5 表示九五折，10 表示不打折</span>
        </el-form-item>
        <el-form-item label="图标">
          <div class="logo-upload">
            <div v-if="form.icon" class="logo-preview" @click="iconPickerVisible = true">
              <img :src="form.icon" />
              <div class="logo-preview__mask">更换</div>
            </div>
            <div v-else class="logo-placeholder" @click="iconPickerVisible = true">
              <el-icon :size="24"><Plus /></el-icon>
              <span>上传图标</span>
            </div>
          </div>
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort" :min="0" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.status" :active-value="1" :inactive-value="0" />
        </el-form-item>
      </el-form>
    </FormDialog>

    <ImagePicker v-model:visible="iconPickerVisible" @confirm="handleIconPick" />
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import {
  getMemberLevelList,
  createMemberLevel,
  updateMemberLevel,
  deleteMemberLevel,
  type MemberLevelItem,
} from '@/api/member'
import ImagePicker from '@/components/ImagePicker/index.vue'
import FormDialog from '@/components/FormDialog/index.vue'
import { useCrud } from '@/hooks/useCrud'

interface LevelForm {
  id: number
  name: string
  minPoints: number
  discount: number
  icon: string
  sort: number
  status: number
}

// 搜索条件只放本页自己的字段。page/pageSize 由 useCrud 管理 ——
// 原先它们混在同一个 queryParams 里，重置时要记得一并复位，很容易漏。
const queryParams = reactive({ name: '' })
const iconPickerVisible = ref(false)

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
} = useCrud<MemberLevelItem, LevelForm, { name?: string; page: number; pageSize: number }>({
  list: (params) => getMemberLevelList(params),
  create: (payload) => createMemberLevel(payload),
  update: (payload) => updateMemberLevel(payload),
  remove: (id) => deleteMemberLevel(id),
  createForm: () => ({ id: 0, name: '', minPoints: 0, discount: 10, icon: '', sort: 0, status: 1 }),
  query: () => ({ name: queryParams.name }),
  titles: { add: '新增等级', edit: '编辑等级' },
  deleteConfirm: '确认删除该等级？',
})

function handleReset() {
  queryParams.name = ''
  handleSearch()
}

function handleIconPick(url: string | string[]) {
  form.icon = url as string
}

const formRules = {
  name: [{ required: true, message: '请输入等级名称', trigger: 'blur' }],
}
</script>

<style lang="scss" scoped>
.form-hint {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

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
</style>
