<template>
  <div class="app-container">
    <el-card v-loading="loading">
      <template #header>
        <div class="card-header">
          <span>支付设置</span>
          <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
        </div>
      </template>
      <el-form ref="formRef" :model="form" label-width="140px" style="max-width: 600px">
        <el-divider content-position="left">微信支付</el-divider>
        <el-form-item label="微信AppID" prop="wechat_app_id">
          <el-input v-model="form.wechat_app_id" placeholder="请输入微信AppID" />
        </el-form-item>
        <el-form-item label="微信商户号" prop="wechat_mch_id">
          <el-input v-model="form.wechat_mch_id" placeholder="请输入微信商户号" />
        </el-form-item>
        <el-form-item label="API密钥" prop="wechat_key">
          <el-input v-model="form.wechat_key" placeholder="请输入微信API密钥" show-password />
          <div class="form-tip">V3接口使用商户API私钥，V2接口使用商户密钥</div>
        </el-form-item>
        <el-form-item label="证书序列号" prop="wechat_serial_no">
          <el-input v-model="form.wechat_serial_no" placeholder="请输入商户API证书序列号" />
          <div class="form-tip">在商户平台「API安全」中查看，用于V3接口签名</div>
        </el-form-item>
        <el-form-item label="PEM证书" prop="wechat_cert_pem">
          <el-upload
            :auto-upload="false"
            :limit="1"
            :on-change="(file: UploadFile) => handleCertUpload(file, 'wechat_cert_pem')"
            :on-remove="() => handleCertRemove('wechat_cert_pem')"
            accept=".pem,.crt,.cer"
          >
            <el-button type="primary" :icon="Upload">上传证书</el-button>
            <template #tip>
              <div class="el-upload__tip">支持 .pem / .crt / .cer 格式，大小不超过 2MB</div>
            </template>
          </el-upload>
          <el-input
            v-if="form.wechat_cert_pem"
            v-model="form.wechat_cert_pem"
            disabled
            style="margin-top: 8px"
          />
        </el-form-item>
        <el-form-item label="证书密钥" prop="wechat_key_pem">
          <el-upload
            :auto-upload="false"
            :limit="1"
            :on-change="(file: UploadFile) => handleCertUpload(file, 'wechat_key_pem')"
            :on-remove="() => handleCertRemove('wechat_key_pem')"
            accept=".pem,.key"
          >
            <el-button type="primary" :icon="Upload">上传密钥</el-button>
            <template #tip>
              <div class="el-upload__tip">支持 .pem / .key 格式，大小不超过 2MB</div>
            </template>
          </el-upload>
          <el-input
            v-if="form.wechat_key_pem"
            v-model="form.wechat_key_pem"
            disabled
            style="margin-top: 8px"
          />
        </el-form-item>
        <el-divider content-position="left">支付宝</el-divider>
        <el-form-item label="支付宝AppID" prop="alipay_app_id">
          <el-input v-model="form.alipay_app_id" placeholder="请输入支付宝AppID" />
        </el-form-item>
        <el-form-item label="应用私钥" prop="alipay_key">
          <el-input
            v-model="form.alipay_key"
            type="textarea"
            :rows="3"
            placeholder="请输入支付宝应用私钥"
            show-password
          />
          <div class="form-tip">在开放平台「应用详情」>「接口加签方式」中获取</div>
        </el-form-item>
        <el-form-item label="支付宝公钥" prop="alipay_public_key">
          <el-input
            v-model="form.alipay_public_key"
            type="textarea"
            :rows="3"
            placeholder="请输入支付宝公钥"
          />
          <div class="form-tip">用于验证支付宝回调签名，非应用公钥</div>
        </el-form-item>
        <el-divider content-position="left">通用设置</el-divider>
        <el-form-item label="支付回调地址" prop="notify_url">
          <el-input
            v-model="form.notify_url"
            placeholder="https://your-domain.com/api/v1/pay/notify/wechat"
          />
          <div class="form-tip">微信和支付宝回调统一地址，需公网可访问</div>
        </el-form-item>
        <el-form-item label="支付完成跳转" prop="return_url">
          <el-input v-model="form.return_url" placeholder="https://your-domain.com/pay/result" />
          <div class="form-tip">支付宝支付完成后跳转的页面地址（可选）</div>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, type UploadFile, type FormInstance } from 'element-plus'
import { Upload } from '@element-plus/icons-vue'
import { getConfigByPrefix, batchSaveConfig, uploadCert, type ConfigItem } from '@/api/config'

const PREFIX = 'pay.'
const loading = ref(false)
const saving = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  wechat_app_id: '',
  wechat_mch_id: '',
  wechat_key: '',
  wechat_serial_no: '',
  wechat_cert_pem: '',
  wechat_key_pem: '',
  alipay_app_id: '',
  alipay_key: '',
  alipay_public_key: '',
  notify_url: '',
  return_url: '',
})

// 配置项是动态键：字段名由后端配置项名推导，无法用字面量联合类型约束。
// 用 Record 收口而不是 any —— 至少能保证读写的是字符串，
// 也避免 (form as any) 这种把整个表单类型抹掉的做法。
const formValues = form as unknown as Record<string, string>

const fieldMap: Record<string, string> = {
  wechat_app_id: 'wechat_app_id',
  wechat_mch_id: 'wechat_mch_id',
  wechat_key: 'wechat_key',
  wechat_serial_no: 'wechat_serial_no',
  wechat_cert_pem: 'wechat_cert_pem',
  wechat_key_pem: 'wechat_key_pem',
  alipay_app_id: 'alipay_app_id',
  alipay_key: 'alipay_key',
  alipay_public_key: 'alipay_public_key',
  notify_url: 'notify_url',
  return_url: 'return_url',
}

async function loadData() {
  loading.value = true
  try {
    const res = await getConfigByPrefix(PREFIX)
    const list: ConfigItem[] = res.data || []
    list.forEach((item) => {
      const field = fieldMap[item.key?.replace(PREFIX, '')]
      if (field) {
        formValues[field] = item.value || ''
      }
    })
  } finally {
    loading.value = false
  }
}

async function handleCertUpload(file: UploadFile, field: string) {
  // 用 file.raw（浏览器原生 File），而不是退回 file 本身 ——
  // UploadFile 只是 el-upload 的包装对象，直接当 File 用会传错东西
  const rawFile = file.raw
  if (!rawFile) {
    ElMessage.error('无法读取所选文件')
    return
  }
  try {
    const res = await uploadCert(rawFile)
    formValues[field] = res.data.path
    ElMessage.success('证书上传成功')
  } catch {
    ElMessage.error('证书上传失败')
  }
}

function handleCertRemove(field: string) {
  formValues[field] = ''
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
