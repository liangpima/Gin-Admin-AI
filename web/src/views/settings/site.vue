<template>
  <div class="app-container">
    <el-card v-loading="loading">
      <template #header>
        <div class="card-header">
          <span>网站设置</span>
          <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
        </div>
      </template>
      <el-form ref="formRef" :model="form" label-width="120px" style="max-width: 600px">
        <el-form-item label="网站名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入网站名称" />
        </el-form-item>
        <el-form-item label="网站标题" prop="title">
          <el-input v-model="form.title" placeholder="请输入网站标题" />
        </el-form-item>
        <el-form-item label="Logo" prop="logo">
          <div class="logo-upload">
            <div v-if="form.logo" class="logo-preview" @click="logoPickerVisible = true">
              <img :src="form.logo" />
              <div class="logo-preview__mask">更换</div>
            </div>
            <div v-else class="logo-placeholder" @click="logoPickerVisible = true">
              <el-icon :size="24"><Plus /></el-icon>
              <span>上传Logo</span>
            </div>
          </div>
        </el-form-item>
        <el-form-item label="网站描述" prop="description">
          <el-input v-model="form.description" type="textarea" :rows="3" placeholder="请输入网站描述" />
        </el-form-item>
        <el-form-item label="版权信息" prop="copyright">
          <el-input v-model="form.copyright" placeholder="请输入版权信息" />
        </el-form-item>
        <el-form-item label="ICP备案号" prop="icp">
          <el-input v-model="form.icp" placeholder="请输入ICP备案号" />
        </el-form-item>
        <el-form-item label="ICP备案链接" prop="icpLink">
          <el-input v-model="form.icpLink" placeholder="请输入ICP备案链接" />
        </el-form-item>
        <el-form-item label="公安备案号" prop="policeRecord">
          <el-input v-model="form.policeRecord" placeholder="请输入公安备案号" />
        </el-form-item>
        <el-form-item label="公安备案链接" prop="policeRecordLink">
          <el-input v-model="form.policeRecordLink" placeholder="请输入公安备案链接" />
        </el-form-item>
        <el-form-item label="联系电话" prop="phone">
          <el-input v-model="form.phone" placeholder="请输入联系电话" />
        </el-form-item>
        <el-form-item label="会员ID位数" prop="memberIdDigits">
          <el-input-number v-model="form.memberIdDigits" :min="4" :max="12" :step="1" />
          <span class="form-hint">如 6 表示从 000001 开始</span>
        </el-form-item>
      </el-form>
    </el-card>

    <ImagePicker v-model:visible="logoPickerVisible" @confirm="handleLogoPick" />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, type FormInstance } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { getConfigByPrefix, batchSaveConfig , type ConfigItem } from '@/api/config'
import ImagePicker from '@/components/ImagePicker/index.vue'

const PREFIX = 'site.'
const loading = ref(false)
const saving = ref(false)
const formRef = ref<FormInstance>()
const logoPickerVisible = ref(false)

const form = reactive({
  name: '',
  title: '',
  logo: '',
  description: '',
  copyright: '',
  icp: '',
  icpLink: '',
  policeRecord: '',
  policeRecordLink: '',
  phone: '',
  memberIdDigits: 6,
})

// 配置项是动态键：字段名由后端配置项名推导，无法用字面量联合类型约束。
// 用 Record 收口而不是 any —— 至少能保证读写的是字符串，
// 也避免 (form as any) 这种把整个表单类型抹掉的做法。
const formValues = form as unknown as Record<string, string | number>

const fieldMap: Record<string, string> = {
  name: 'name',
  title: 'title',
  logo: 'logo',
  description: 'description',
  copyright: 'copyright',
  icp: 'icp',
  icpLink: 'icpLink',
  policeRecord: 'policeRecord',
  policeRecordLink: 'policeRecordLink',
  phone: 'phone',
  memberIdDigits: 'memberIdDigits',
}

async function loadData() {
  loading.value = true
  try {
    const res = await getConfigByPrefix(PREFIX)
    const list: ConfigItem[] = res.data || []
    list.forEach((item) => {
      const field = fieldMap[item.key?.replace(PREFIX, '')]
      if (field) {
        let val: string | number = item.value || ''
        if (field === 'memberIdDigits') {
          val = parseInt(val, 10) || 6
        }
        ;formValues[field] = val
      }
    })
  } finally {
    loading.value = false
  }
}

function handleLogoPick(url: string | string[]) {
  form.logo = url as string
}

async function handleSave() {
  saving.value = true
  try {
    const items = Object.entries(fieldMap).map(([field, key]) => ({
      key,
      value: String(formValues[field] ?? ''),
    }))
    await batchSaveConfig(PREFIX, items)
    ElMessage.success('保存成功')
  } finally {
    saving.value = false
  }
}

onMounted(() => loadData())
</script>

<style lang="scss" scoped>
.form-hint {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
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
</style>
