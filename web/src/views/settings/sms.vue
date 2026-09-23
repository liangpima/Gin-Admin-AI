<template>
  <div class="app-container">
    <el-card v-loading="loading" class="table-card">
      <el-tabs v-model="activeTab" style="margin-bottom: 16px">
        <el-tab-pane label="短信设置" name="config" />
        <el-tab-pane label="发送记录" name="log" />
      </el-tabs>

      <template v-if="activeTab === 'config'">
        <el-form ref="formRef" :model="form" label-width="120px" style="max-width: 600px; padding-bottom: 10px">
          <el-form-item label="短信状态">
            <el-radio-group v-model="form.status">
              <el-radio value="1">开启</el-radio>
              <el-radio value="0">关闭</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="短信接口">
            <el-select v-model="form.provider" placeholder="请选择短信接口" style="width: 100%">
              <el-option label="阿里云" value="aliyun" />
              <el-option label="腾讯云" value="tencent" />
              <el-option label="华为云" value="huawei" />
            </el-select>
          </el-form-item>
          <el-form-item label="AccessKey ID">
            <el-input v-model="form.access_key" placeholder="请输入AccessKey ID" show-password />
          </el-form-item>
          <el-form-item label="AccessKey Secret">
            <el-input v-model="form.secret_key" placeholder="请输入AccessKey Secret" show-password />
          </el-form-item>
          <el-form-item label="短信签名">
            <el-input v-model="form.sign_name" placeholder="请输入短信签名" />
          </el-form-item>
        </el-form>

        <el-divider content-position="left">短信模板设置</el-divider>

        <div class="template-list">
            <div v-for="tpl in templates.filter(t => !t.hidden)" :key="tpl.key" class="template-item">
            <span class="template-label">{{ tpl.label }}</span>
            <el-input
              v-model="tpl.value"
              :placeholder="tpl.placeholder"
              class="template-input"
            />
            <el-switch
              v-model="tpl.enabled"
              active-text="开启"
              inactive-text="关闭"
            />
            <span class="template-hint">{{ tpl.hint }}</span>
          </div>
        </div>

        <div class="save-bar">
          <el-button type="primary" :loading="saving" @click="handleSave">保存</el-button>
        </div>
      </template>

      <el-empty v-if="activeTab === 'log'" description="暂无发送记录" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, type FormInstance } from 'element-plus'
import { getConfigByPrefix, batchSaveConfig , type ConfigItem } from '@/api/config'

const PREFIX = 'sms.'
const activeTab = ref('config')
const loading = ref(false)
const saving = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  status: '1',
  provider: 'aliyun',
  access_key: '',
  secret_key: '',
  sign_name: '',
})

// 配置项是动态键：字段名由后端配置项名推导，无法用字面量联合类型约束。
// 用 Record 收口而不是 any —— 至少能保证读写的是字符串，
// 也避免 (form as any) 这种把整个表单类型抹掉的做法。
const formValues = form as unknown as Record<string, string>

interface TemplateItem {
  key: string
  label: string
  value: string
  enabled: boolean
  placeholder: string
  hint: string
  hidden?: boolean
}

const templates = reactive<TemplateItem[]>([
  {
    key: 'tpl_verify_code', label: '短信验证码', value: '',
    enabled: false, placeholder: '请填写模板编号',
    hint: '模板内容示例：您的验证码：${code}，该验证码5分钟内有效，请勿泄露于他人！',
  },
  {
    key: 'tpl_order_paid', label: '订单支付成功', value: '',
    enabled: false, placeholder: '请填写模板编号',
    hint: '模板内容示例：订单支付成功，订单号：${ordernum}，我们会尽快为您发货。',
  },
  {
    key: 'tpl_order_shipped', label: '订单发货通知', value: '',
    enabled: false, placeholder: '请填写模板编号',
    hint: '模板内容示例：您的订单${ordernum}已发货，快递公司：${express_com}，快递单号：${express_no}，请留意查收。',
  },
  {
    key: 'tpl_group_success', label: '拼团成功通知', value: '',
    enabled: false, placeholder: '请填写模板编号',
    hint: '模板内容示例：订单${ordernum}拼团成功，我们会尽快为您发货。',
  },
  {
    key: 'tpl_refund_success', label: '退款成功通知', value: '',
    enabled: false, placeholder: '请填写模板编号',
    hint: '模板内容示例：您的订单${ordernum}退款成功，退款金额：${money}元，请留意查收。',
  },
  {
    key: 'tpl_refund_reject', label: '退款驳回通知', value: '',
    enabled: false, placeholder: '请填写模板编号',
    hint: '模板内容示例：抱歉您的订单${ordernum}退款申请失败，原因：${reason}。',
  },
  {
    key: 'tpl_withdraw_success', label: '提现成功通知', value: '',
    enabled: false, placeholder: '请填写模板编号',
    hint: '模板内容示例：提现成功，打款金额：${money}，请留意查收。',
  },
  {
    key: 'tpl_withdraw_fail', label: '提现失败通知', value: '',
    enabled: false, placeholder: '请填写模板编号',
    hint: '模板内容示例：抱歉您的提现申请失败，原因：${reason}。',
  },
  {
    key: 'tpl_commission', label: '分销成功提醒', value: '',
    enabled: false, placeholder: '请填写模板编号',
    hint: '模板内容示例：成功获得佣金：${money}元，请留意查收。',
  },
  {
    key: 'tpl_dining_success', label: '餐饮预定成功提醒', value: '',
    enabled: false, placeholder: '请填写模板编号', hidden: true,
    hint: '模板内容示例：预定成功，餐厅名称：${restaurant_name}，订位信息：${table}，预定时间：${time_range}，请准时到达。',
  },
  {
    key: 'tpl_dining_fail', label: '餐饮预定失败提醒', value: '',
    enabled: false, placeholder: '请填写模板编号', hidden: true,
    hint: '模板内容示例：抱歉您的预定申请失败，餐厅名称：${restaurant_name}，请重新预定。',
  },
])

const configFields: Record<string, string> = {
  status: 'status',
  provider: 'provider',
  access_key: 'access_key',
  secret_key: 'secret_key',
  sign_name: 'sign_name',
}

async function loadData() {
  loading.value = true
  try {
    const res = await getConfigByPrefix(PREFIX)
    const list: ConfigItem[] = res.data || []
    list.forEach((item) => {
      const rawKey = item.key?.replace(PREFIX, '')
      if (configFields[rawKey]) {
        ;formValues[configFields[rawKey]] = item.value || ''
      }
      const tpl = templates.find((t) => t.key === rawKey)
      if (tpl) {
        tpl.value = item.value || ''
      }
    })

    templates.forEach((tpl) => {
      const enabledItem = list.find((item) => item.key === `${PREFIX}${tpl.key}_enabled`)
      if (enabledItem) {
        tpl.enabled = enabledItem.value === '1'
      }
    })
  } finally {
    loading.value = false
  }
}

async function handleSave() {
  saving.value = true
  try {
    const items: { key: string; value: string }[] = []

    Object.entries(configFields).forEach(([field, key]) => {
      items.push({ key, value: formValues[field] || '' })
    })

    templates.forEach((tpl) => {
      items.push({ key: tpl.key, value: tpl.value })
      items.push({ key: `${tpl.key}_enabled`, value: tpl.enabled ? '1' : '0' })
    })

    await batchSaveConfig(PREFIX, items)
    ElMessage.success('保存成功')
  } finally {
    saving.value = false
  }
}

onMounted(() => loadData())
</script>

<style lang="scss" scoped>
@use '@/assets/styles/responsive.scss' as *;

.table-card {
  :deep(.el-card__header) {
    border-bottom-color: var(--color-border-lighter);
  }

  :deep(.el-tabs__header) {
    margin-bottom: 16px;
  }
}

.save-bar {
  margin-top: 16px;
  padding-left: 132px;
}

.template-list {
  .template-item {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 0;
    border-bottom: 1px solid var(--el-border-color-lighter);

    &:last-child {
      border-bottom: none;
    }

    .template-label {
      width: 120px;
      text-align: right;
      flex-shrink: 0;
      font-size: var(--el-form-label-font-size, 14px);
      color: var(--el-text-color-regular);
      line-height: 32px;
      padding-right: 12px;
    }

    .template-input {
      width: 280px;
      flex-shrink: 0;
    }

    .template-hint {
      font-size: 12px;
      color: var(--el-text-color-secondary);
    }

    @include mobile {
      flex-wrap: wrap;
      gap: 8px;

      .template-label {
        width: 100%;
        text-align: left;
        padding-bottom: 4px;
      }

      .template-input {
        width: 0;
        flex: 1;
      }

      .template-hint {
        width: 100%;
      }
    }
  }
}
</style>
