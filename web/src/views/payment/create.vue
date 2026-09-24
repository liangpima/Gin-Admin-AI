<template>
  <div class="app-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>创建支付测试</span>
        </div>
      </template>

      <el-form :model="form" label-width="100px" style="max-width: 500px">
        <el-form-item label="订单标题">
          <el-input v-model="form.subject" placeholder="请输入订单标题" />
        </el-form-item>
        <el-form-item label="订单描述">
          <el-input v-model="form.body" placeholder="请输入订单描述（可选）" />
        </el-form-item>
        <el-form-item label="金额(元)">
          <el-input-number v-model="form.amountYuan" :min="0.01" :precision="2" :step="1" />
        </el-form-item>
        <el-form-item label="支付渠道">
          <el-radio-group v-model="form.channel">
            <el-radio value="wechat">微信支付</el-radio>
            <el-radio value="alipay">支付宝</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="submitting" @click="handleCreate">创建订单</el-button>
        </el-form-item>
      </el-form>

      <el-divider v-if="payResult" />

      <div v-if="payResult" class="pay-result">
        <el-alert
          :title="payResult.message"
          :type="payResult.success ? 'success' : 'error'"
          show-icon
          :closable="false"
        />
        <div v-if="payResult.formUrl" style="margin-top: 16px">
          <p>请在新窗口打开支付页面：</p>
          <el-button type="primary" @click="openPayWindow(payResult.formUrl!)"
            >打开支付页面</el-button
          >
        </div>
        <div v-if="payResult.codeUrl" style="margin-top: 16px">
          <p>微信Native支付码：</p>
          <el-input v-model="payResult.codeUrl" readonly />
        </div>
        <div v-if="payResult.orderString" style="margin-top: 16px">
          <p>支付宝App支付参数：</p>
          <el-input v-model="payResult.orderString" type="textarea" :rows="3" readonly />
        </div>
        <div v-if="payResult.orderNo" style="margin-top: 16px">
          <p>
            订单号：<el-tag>{{ payResult.orderNo }}</el-tag>
          </p>
          <el-button size="small" @click="checkStatus">查询支付状态</el-button>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import { createPayOrder, queryPayOrder } from '@/api/payment'

const submitting = ref(false)

const form = reactive({
  subject: '测试商品',
  body: '',
  amountYuan: 0.01,
  channel: 'wechat' as 'wechat' | 'alipay',
})

const payResult = ref<{
  success: boolean
  message: string
  formUrl?: string
  codeUrl?: string
  orderString?: string
  orderNo?: string
} | null>(null)

async function handleCreate() {
  if (!form.subject) {
    ElMessage.warning('请输入订单标题')
    return
  }

  submitting.value = true
  try {
    const res = await createPayOrder({
      subject: form.subject,
      body: form.body,
      amount: Math.round(form.amountYuan * 100),
      channel: form.channel,
    })

    const data = res.data
    payResult.value = {
      success: true,
      message: '订单创建成功',
      formUrl: data.formUrl,
      codeUrl: data.codeUrl,
      orderString: data.orderString,
      orderNo: data.orderNo,
    }
  } catch (err) {
    payResult.value = {
      success: false,
      message: err instanceof Error ? err.message : '创建失败',
    }
  } finally {
    submitting.value = false
  }
}

function openPayWindow(url: string) {
  window.open(url, '_blank', 'noopener')
}

async function checkStatus() {
  if (!payResult.value?.orderNo) return
  try {
    const res = await queryPayOrder(payResult.value.orderNo)
    const statusMap: Record<number, string> = {
      0: '待支付',
      1: '已支付',
      2: '已关闭',
      3: '已退款',
      4: '退款中',
    }
    ElMessage.info(`订单状态：${statusMap[res.data.status] || '未知'}`)
  } catch {
    ElMessage.error('查询失败')
  }
}
</script>

<style lang="scss" scoped>
.pay-result {
  padding: 16px 0;
}
</style>
