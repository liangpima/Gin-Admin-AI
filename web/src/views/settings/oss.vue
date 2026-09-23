<template>
  <div class="app-container">
    <el-card v-loading="loading">
      <template #header>
        <div class="card-header">
          <span>OSS存储设置</span>
          <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
        </div>
      </template>
      <el-form ref="formRef" :model="form" label-width="120px" style="max-width: 600px">
        <el-form-item label="存储类型" prop="type">
          <el-select v-model="form.type" placeholder="请选择存储类型" style="width: 100%">
            <el-option label="阿里云OSS" value="aliyun" />
            <el-option label="腾讯云COS" value="tencent" />
            <el-option label="MinIO" value="minio" />
            <el-option label="本地存储" value="local" />
          </el-select>
        </el-form-item>
        <el-form-item label="Endpoint" prop="endpoint">
          <el-input v-model="form.endpoint" placeholder="请输入Endpoint" />
          <div class="form-tip" v-if="form.type === 'aliyun'">阿里云：如 oss-cn-hangzhou.aliyuncs.com</div>
          <div class="form-tip" v-else-if="form.type === 'tencent'">腾讯云：填写 Region，如 ap-guangzhou</div>
          <div class="form-tip" v-else-if="form.type === 'minio'">MinIO：如 localhost:9000 或 minio.example.com:9000</div>
          <div class="form-tip" v-else>请输入 Endpoint</div>
        </el-form-item>
        <el-form-item label="Bucket" prop="bucket">
          <el-input v-model="form.bucket" placeholder="请输入Bucket名称" />
          <div class="form-tip">存储空间名称，需提前在控制台创建</div>
        </el-form-item>
        <el-form-item label="AccessKey" prop="access_key">
          <el-input v-model="form.access_key" placeholder="请输入AccessKey" />
          <div class="form-tip">云平台 AccessKey ID，用于身份验证</div>
        </el-form-item>
        <el-form-item label="SecretKey" prop="secret_key">
          <el-input v-model="form.secret_key" placeholder="请输入SecretKey" show-password />
          <div class="form-tip">云平台 AccessKey Secret，务必妥善保管</div>
        </el-form-item>
        <el-form-item label="自定义域名" prop="domain">
          <el-input v-model="form.domain" placeholder="请输入自定义域名（可选）" />
          <div class="form-tip">绑定自定义域名后文件访问将使用该域名，留空则使用默认域名</div>
        </el-form-item>
        <el-divider content-position="left">备份设置</el-divider>
        <el-form-item label="本地备份">
          <el-switch
            v-model="form.local_backup"
            active-value="1"
            inactive-value="0"
            active-text="开启"
            inactive-text="关闭"
          />
          <div class="form-tip">开启后所有上传文件均在本地备份一份；关闭后上传完成自动删除本地文件</div>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, type FormInstance } from 'element-plus'
import { getConfigByPrefix, batchSaveConfig , type ConfigItem } from '@/api/config'

const PREFIX = 'oss.'
const loading = ref(false)
const saving = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  type: 'local',
  endpoint: '',
  bucket: '',
  access_key: '',
  secret_key: '',
  domain: '',
  local_backup: '0',
})

// 配置项是动态键：字段名由后端配置项名推导，无法用字面量联合类型约束。
// 用 Record 收口而不是 any —— 至少能保证读写的是字符串，
// 也避免 (form as any) 这种把整个表单类型抹掉的做法。
const formValues = form as unknown as Record<string, string>

const fieldMap: Record<string, string> = {
  type: 'type',
  endpoint: 'endpoint',
  bucket: 'bucket',
  access_key: 'access_key',
  secret_key: 'secret_key',
  domain: 'domain',
  local_backup: 'local_backup',
}

async function loadData() {
  loading.value = true
  try {
    const res = await getConfigByPrefix(PREFIX)
    const list: ConfigItem[] = res.data || []
    list.forEach((item) => {
      const field = fieldMap[item.key?.replace(PREFIX, '')]
      if (field) {
        ;formValues[field] = item.value || ''
      }
    })
  } finally {
    loading.value = false
  }
}

async function handleSave() {
  saving.value = true
  try {
    const items = Object.entries(fieldMap).map(([field, key]) => ({
      key,
      value: formValues[field] || '',
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
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.form-tip {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}
</style>
