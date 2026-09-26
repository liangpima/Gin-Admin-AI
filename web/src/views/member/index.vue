<template>
  <div class="app-container">
    <div class="search-form">
      <CollapsibleFilter>
        <el-form :model="queryParams">
          <el-row :gutter="16">
            <el-col :xs="24" :sm="12" :md="8" :lg="6">
              <el-form-item label="手机号">
                <el-input
                  v-model="queryParams.phone"
                  placeholder="请输入手机号"
                  clearable
                  @keyup.enter="handleSearch"
                />
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :md="8" :lg="6">
              <el-form-item label="昵称">
                <el-input
                  v-model="queryParams.nickname"
                  placeholder="请输入昵称"
                  clearable
                  @keyup.enter="handleSearch"
                />
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :md="8" :lg="6">
              <el-form-item label="等级">
                <el-select
                  v-model="queryParams.levelId"
                  placeholder="全部"
                  clearable
                  style="width: 100%"
                >
                  <el-option
                    v-for="level in levelList"
                    :key="level.id"
                    :label="level.name"
                    :value="level.id"
                  />
                </el-select>
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :md="8" :lg="6">
              <el-form-item label="状态">
                <el-select
                  v-model="queryParams.status"
                  placeholder="全部"
                  clearable
                  style="width: 100%"
                >
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
      </CollapsibleFilter>
    </div>

    <el-card class="table-card">
      <template #header>
        <div class="card-header">
          <span>会员列表</span>
          <el-button type="primary" @click="handleAdd()">新增会员</el-button>
        </div>
      </template>

      <ResponsiveTable :data="tableData" :columns="columns" :loading="loading">
        <template #avatar="{ row }">
          <el-avatar :size="32" :src="row.avatar || undefined">{{
            row.nickname?.charAt(0)?.toUpperCase() || row.username?.charAt(0)?.toUpperCase()
          }}</el-avatar>
        </template>
        <template #gender="{ row }">
          {{ { 0: '未知', 1: '男', 2: '女' }[row.gender as number] || '未知' }}
        </template>
        <template #level="{ row }">
          <el-select
            v-model="row.levelId"
            placeholder="无等级"
            style="width: 100%"
            @change="(val: number) => handleLevelChange(row as MemberRow, val)"
          >
            <el-option :label="'无等级'" :value="0" />
            <el-option
              v-for="level in levelList"
              :key="level.id"
              :label="level.name"
              :value="level.id"
            />
          </el-select>
        </template>
        <template #tags="{ row }">
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
        <template #status="{ row }">
          <el-switch
            v-model="row.status"
            :active-value="1"
            :inactive-value="0"
            @change="handleStatusChange(row as MemberRow)"
          />
        </template>
        <template #registerTime="{ row }">{{ formatDateTime(row.registerTime) }}</template>
        <template #actions="{ row }">
          <el-button type="primary" link size="small" @click="handleEdit(row as MemberRow)"
            >编辑</el-button
          >
          <el-button type="danger" link size="small" @click="handleDelete(row as MemberRow)"
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
          <el-date-picker
            v-model="form.birthday"
            type="date"
            value-format="YYYY-MM-DD"
            placeholder="请选择出生日期"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="等级">
          <el-select v-model="form.levelId" placeholder="请选择等级" style="width: 100%">
            <el-option :label="'无等级'" :value="0" />
            <el-option
              v-for="level in levelList"
              :key="level.id"
              :label="level.name"
              :value="level.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="标签">
          <el-select
            v-model="form.tagIds"
            multiple
            collapse-tags
            collapse-tags-tooltip
            placeholder="请选择标签"
            style="width: 100%"
          >
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
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import {
  getMemberList,
  createMember,
  updateMember,
  deleteMember,
  updateMemberStatus,
  updateMemberTags,
  getAllMemberLevels,
  getAllMemberTags,
  type MemberItem,
  type MemberLevelItem,
  type MemberTagItem,
} from '@/api/member'
import CollapsibleFilter from '@/components/CollapsibleFilter/index.vue'
import ImagePicker from '@/components/ImagePicker/index.vue'
import FormDialog from '@/components/FormDialog/index.vue'
import ResponsiveTable from '@/components/ResponsiveTable/index.vue'
import type { ResponsiveColumn } from '@/components/ResponsiveTable/types'
import { formatDateTime } from '@/utils/format'
import { useCrud } from '@/hooks/useCrud'

// 列定义是唯一来源：桌面端表格列与手机端卡片字段都从这里派生。
// 注意「等级 / 标签 / 状态」三列在表格里是行内编辑控件，卡片里用的是
// **同一个插槽**，所以手机上照样能直接改 —— 这正是把渲染收在一处的收益。
const columns: ResponsiveColumn<MemberRow>[] = [
  { label: 'ID', prop: 'id', width: 60 },
  { label: '会员编号', prop: 'memberNo', width: 110 },
  { label: '头像', slot: 'avatar', width: 60 },
  { label: '昵称', prop: 'nickname', width: 120 },
  { label: '手机号', prop: 'phone', width: 120 },
  { label: '性别', slot: 'gender', width: 70 },
  { label: '等级', slot: 'level', width: 120 },
  { label: '标签', slot: 'tags', minWidth: 200 },
  { label: '积分', prop: 'points', width: 80, align: 'right' },
  { label: '状态', slot: 'status', width: 80 },
  { label: '注册时间', slot: 'registerTime', width: 170 },
  { label: '操作', slot: 'actions', width: 160, hideInCard: true },
]

// 列表行 = 接口返回的 MemberItem + 前端映射出来的 tagIds。
// 接口给的是 tags（对象数组），表格里要按 id 做多选回显，所以映射时补 tagIds。
type MemberRow = MemberItem & { tagIds: number[] }

interface MemberForm {
  id: number
  username: string
  nickname: string
  avatar: string
  phone: string
  gender: number
  birthday: string
  levelId: number
  tagIds: number[]
  status: number
  remark: string
  wechatOpenid: string
  lastVisitTime: string
}

type MemberQuery = {
  phone?: string
  nickname?: string
  levelId?: number
  status?: number
  page: number
  pageSize: number
}

// 搜索条件只放本页自己的字段；page/pageSize 由 useCrud 管理
const queryParams = reactive({
  phone: '',
  nickname: '',
  levelId: undefined as number | undefined,
  status: undefined as number | undefined,
})

const levelList = ref<MemberLevelItem[]>([])
const tagList = ref<MemberTagItem[]>([])
const avatarPickerVisible = ref(false)

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
} = useCrud<MemberRow, MemberForm, MemberQuery, MemberItem>({
  list: (params) => getMemberList(params),
  create: (payload) => createMember(payload),
  update: (payload) => updateMember(payload),
  remove: (id) => deleteMember(id),
  createForm: () => ({
    id: 0,
    username: '',
    nickname: '',
    avatar: '',
    phone: '',
    gender: 0,
    birthday: '',
    levelId: 0,
    tagIds: [],
    status: 1,
    remark: '',
    wechatOpenid: '',
    lastVisitTime: '',
  }),
  // 列表行要补 tagIds 供表格多选回显
  mapRow: (item) => ({ ...item, tagIds: item.tags?.map((t) => t.id) || [] }),
  // 生日在接口里是 ISO 时间串，而日期选择器只认 yyyy-MM-dd，必须截断
  rowToForm: (row) => ({
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
    remark: row.remark || '',
    wechatOpenid: row.wechatOpenid || '',
    lastVisitTime: row.lastVisitTime || '',
  }),
  query: () => ({
    phone: queryParams.phone,
    nickname: queryParams.nickname,
    levelId: queryParams.levelId,
    status: queryParams.status,
  }),
  titles: { add: '新增会员', edit: '编辑会员' },
  deleteConfirm: '确认删除该会员？',
})

function handleReset() {
  queryParams.phone = ''
  queryParams.nickname = ''
  queryParams.levelId = undefined
  queryParams.status = undefined
  handleSearch()
}

async function loadLevels() {
  try {
    const res = await getAllMemberLevels()
    levelList.value = res.data
  } catch (err) {
    // 等级只用于筛选与下拉选择，加载失败不阻断列表本身
    console.warn('[member] 会员等级加载失败，等级筛选与选择将为空', err)
  }
}

async function loadTags() {
  try {
    const res = await getAllMemberTags()
    tagList.value = res.data
  } catch (err) {
    // 标签只用于筛选与下拉选择，加载失败不阻断列表本身
    console.warn('[member] 会员标签加载失败，标签筛选与选择将为空', err)
  }
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

function handleAvatarPick(url: string | string[]) {
  form.avatar = url as string
}

const formRules = {
  phone: [{ required: true, message: '请输入手机号', trigger: 'blur' }],
}

// 列表由 useCrud 的 immediate 自动加载，这里只补本页的下拉数据
onMounted(() => {
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
