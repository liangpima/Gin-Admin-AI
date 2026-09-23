<template>
  <div class="app-container">
    <div class="search-form">
      <el-form :model="queryParams">
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="手机号">
              <el-input v-model="queryParams.phone" placeholder="请输入手机号" clearable @keyup.enter="handleSearch" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="昵称">
              <el-input v-model="queryParams.nickname" placeholder="请输入昵称" clearable @keyup.enter="handleSearch" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="等级">
              <el-select v-model="queryParams.levelId" placeholder="全部" clearable style="width: 100%">
                <el-option v-for="level in levelList" :key="level.id" :label="level.name" :value="level.id" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="状态">
              <el-select v-model="queryParams.status" placeholder="全部" clearable style="width: 100%">
                <el-option label="正常" :value="1" />
                <el-option label="停用" :value="0" />
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
          <span>会员列表</span>
          <el-button type="primary" @click="handleAdd">新增会员</el-button>
        </div>
      </template>

      <el-table :data="tableData" v-loading="loading" border>
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="memberNo" label="会员编号" width="110" />
        <el-table-column label="头像" width="60">
          <template #default="{ row }">
            <el-avatar :size="32" :src="row.avatar || undefined">{{ row.nickname?.charAt(0)?.toUpperCase() || row.username?.charAt(0)?.toUpperCase() }}</el-avatar>
          </template>
        </el-table-column>
        <el-table-column prop="nickname" label="昵称" width="120" />
        <el-table-column prop="phone" label="手机号" width="120" />
        <el-table-column label="性别" width="70">
          <template #default="{ row }">
            {{ { 0: '未知', 1: '男', 2: '女' }[row.gender as number] || '未知' }}
          </template>
        </el-table-column>
        <el-table-column label="等级" width="120">
          <template #default="{ row }">
            <el-select
              v-model="row.levelId"
              placeholder="无等级"
              style="width: 100%"
              @change="(val: number) => handleLevelChange(row as MemberRow, val)"
            >
              <el-option :label="'无等级'" :value="0" />
              <el-option v-for="level in levelList" :key="level.id" :label="level.name" :value="level.id" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="标签" min-width="200">
          <template #default="{ row }">
            <el-select
              v-model="row.tagIds"
              multiple
              collapse-tags
              collapse-tags-tooltip
              placeholder="请选择标签"
              style="width: 100%"
              @change="(val: number[]) => handleTagChange(row as MemberRow, val)"
            >
              <el-option v-for="tag in tagList" :key="tag.id" :label="tag.name" :value="tag.id" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column prop="points" label="积分" width="80" align="right" />
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-switch v-model="row.status" :active-value="1" :inactive-value="0" @change="handleStatusChange(row as MemberRow)" />
          </template>
        </el-table-column>
        <el-table-column label="注册时间" width="170">
          <template #default="{ row }">{{ formatDateTime(row.registerTime) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click="handleEdit(row as MemberRow)">编辑</el-button>
            <el-button type="danger" link size="small" @click="handleDelete(row as MemberRow)">删除</el-button>
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
        <el-form-item v-if="form.id && form.wechatOpenid" label="OpenID">
          <el-text type="info">{{ form.wechatOpenid }}</el-text>
        </el-form-item>
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
        <el-form-item label="用户名">
          <el-input v-model="form.username" placeholder="请输入用户名" />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="form.nickname" placeholder="请输入昵称" />
        </el-form-item>
        <el-form-item label="手机号" prop="phone">
          <el-input v-model="form.phone" placeholder="请输入手机号" />
        </el-form-item>
        <el-form-item label="性别">
          <el-radio-group v-model="form.gender">
            <el-radio :value="0">未知</el-radio>
            <el-radio :value="1">男</el-radio>
            <el-radio :value="2">女</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="出生日期">
          <el-date-picker v-model="form.birthday" type="date" value-format="YYYY-MM-DD" placeholder="请选择出生日期" style="width: 100%" />
        </el-form-item>
        <el-form-item label="等级">
          <el-select v-model="form.levelId" placeholder="请选择等级" style="width: 100%">
            <el-option :label="'无等级'" :value="0" />
            <el-option v-for="level in levelList" :key="level.id" :label="level.name" :value="level.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="标签">
          <el-select v-model="form.tagIds" multiple collapse-tags collapse-tags-tooltip placeholder="请选择标签" style="width: 100%">
            <el-option v-for="tag in tagList" :key="tag.id" :label="tag.name" :value="tag.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.status" :active-value="1" :inactive-value="0" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" placeholder="请输入备注" />
        </el-form-item>
        <el-form-item v-if="form.id && form.lastVisitTime" label="最近登录">
          <el-text type="info">{{ formatDateTime(form.lastVisitTime) }}</el-text>
        </el-form-item>
      </el-form>
    </FormDialog>

    <ImagePicker v-model:visible="avatarPickerVisible" @confirm="handleAvatarPick" />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { getMemberList, createMember, updateMember, deleteMember, updateMemberStatus, updateMemberTags, getAllMemberLevels, getAllMemberTags, type MemberItem, type MemberLevelItem, type MemberTagItem } from '@/api/member'
import ImagePicker from '@/components/ImagePicker/index.vue'
import FormDialog from '@/components/FormDialog/index.vue'
import { formatDateTime } from '@/utils/format'

const loading = ref(false)
const submitLoading = ref(false)
// 列表行 = 接口返回的 MemberItem + 前端映射出来的 tagIds。
// 接口给的是 tags（对象数组），表格里要按 id 做多选回显，所以映射时补 tagIds。
type MemberRow = MemberItem & { tagIds: number[] }

const tableData = ref<MemberRow[]>([])
const total = ref(0)
const dialogVisible = ref(false)
const dialogTitle = ref('')
const formRef = ref<FormInstance>()
const levelList = ref<MemberLevelItem[]>([])
const tagList = ref<MemberTagItem[]>([])
const avatarPickerVisible = ref(false)

const queryParams = reactive({
  phone: '',
  nickname: '',
  levelId: undefined as number | undefined,
  status: undefined as number | undefined,
  page: 1,
  pageSize: 10,
})

const form = reactive({
  id: 0,
  username: '',
  nickname: '',
  avatar: '',
  phone: '',
  gender: 0,
  birthday: '',
  levelId: 0,
  tagIds: [] as number[],
  status: 1,
  remark: '',
  wechatOpenid: '',
  lastVisitTime: '',
})

const formRules = {
  phone: [{ required: true, message: '请输入手机号', trigger: 'blur' }],
}

async function loadData() {
  loading.value = true
  try {
    const res = await getMemberList(queryParams)
    tableData.value = res.data.list.map((item) => ({
      ...item,
      tagIds: item.tags?.map((t) => t.id) || [],
    }))
    total.value = res.data.total
  } finally {
    loading.value = false
  }
}

async function loadLevels() {
  try {
    const res = await getAllMemberLevels()
    levelList.value = res.data
  } catch {}
}

async function loadTags() {
  try {
    const res = await getAllMemberTags()
    tagList.value = res.data
  } catch {}
}

function handleSearch() {
  queryParams.page = 1
  loadData()
}

function handleReset() {
  queryParams.phone = ''
  queryParams.nickname = ''
  queryParams.levelId = undefined
  queryParams.status = undefined
  handleSearch()
}

async function handleStatusChange(row: MemberRow) {
  try {
    await updateMemberStatus({ id: row.id, status: row.status })
    ElMessage.success('状态修改成功')
  } catch {
    row.status = row.status === 1 ? 0 : 1
  }
}

async function handleTagChange(row: MemberRow, tagIds: number[]) {
  try {
    await updateMemberTags({ id: row.id, tagIds })
    ElMessage.success('标签修改成功')
  } catch {
    loadData()
  }
}

async function handleLevelChange(row: MemberRow, levelId: number) {
  try {
    await updateMember({ id: row.id, levelId })
    ElMessage.success('等级修改成功')
  } catch {
    loadData()
  }
}

function resetForm() {
  form.id = 0
  form.username = ''
  form.nickname = ''
  form.avatar = ''
  form.phone = ''
  form.gender = 0
  form.birthday = ''
  form.levelId = 0
  form.tagIds = []
  form.status = 1
  form.remark = ''
  form.wechatOpenid = ''
  form.lastVisitTime = ''
}

function handleAdd() {
  resetForm()
  dialogTitle.value = '新增会员'
  dialogVisible.value = true
}

function handleEdit(row: MemberRow) {
  resetForm()
  Object.assign(form, {
    id: row.id,
    username: row.username,
    nickname: row.nickname,
    avatar: row.avatar,
    phone: row.phone,
    gender: row.gender,
    birthday: row.birthday?.split('T')[0] || '',
    levelId: row.levelId,
    tagIds: row.tagIds || [],
    status: row.status,
    remark: row.remark,
    wechatOpenid: row.wechatOpenid || '',
    lastVisitTime: row.lastVisitTime || '',
  })
  dialogTitle.value = '编辑会员'
  dialogVisible.value = true
}

async function handleSubmit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return

  submitLoading.value = true
  try {
    if (form.id) {
      await updateMember(form)
    } else {
      await createMember(form)
    }
    ElMessage.success('操作成功')
    dialogVisible.value = false
    loadData()
  } finally {
    submitLoading.value = false
  }
}

async function handleDelete(row: MemberRow) {
  await ElMessageBox.confirm('确认删除该会员？', '提示', { type: 'warning' })
  await deleteMember(row.id)
  ElMessage.success('删除成功')
  loadData()
}

function handleAvatarPick(url: string | string[]) {
  form.avatar = url as string
}

onMounted(() => {
  loadData()
  loadLevels()
  loadTags()
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
</style>
