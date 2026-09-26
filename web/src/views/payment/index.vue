<template>
  <div class="app-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>支付订单</span>
        </div>
      </template>

      <div class="search-bar">
        <el-input
          v-model="queryParams.subject"
          placeholder="搜索订单标题"
          clearable
          style="width: 200px"
          @keyup.enter="loadData"
          @clear="loadData"
        />
        <el-select
          v-model="queryParams.channel"
          placeholder="支付渠道"
          clearable
          style="width: 140px"
          @change="loadData"
        >
          <el-option
            v-for="opt in getList('sys_pay_channel', DEFAULT_CHANNELS)"
            :key="opt.value"
            :label="opt.label"
            :value="opt.value"
          />
        </el-select>
        <el-select
          v-model="queryParams.status"
          placeholder="订单状态"
          clearable
          style="width: 140px"
          @change="loadData"
        >
          <el-option
            v-for="opt in getList('sys_pay_order_status', DEFAULT_ORDER_STATUS)"
            :key="opt.value"
            :label="opt.label"
            :value="opt.value"
          />
        </el-select>
        <el-button type="primary" @click="loadData">搜索</el-button>
      </div>

      <ResponsiveTable
        :data="tableData"
        :columns="columns"
        :loading="loading"
        :border="false"
        stripe
      >
        <template #amount="{ row }">
          <span class="amount-text">¥{{ (row.amount / 100).toFixed(2) }}</span>
        </template>
        <template #channel="{ row }">
          <el-tag v-if="row.channel === 'wechat'" type="success" size="small">微信</el-tag>
          <el-tag v-else-if="row.channel === 'alipay'" type="primary" size="small">支付宝</el-tag>
          <el-tag v-else size="small">{{ row.channel }}</el-tag>
        </template>
        <template #status="{ row }">
          <DictTag type="sys_pay_order_status" :value="row.status" />
        </template>
        <template #paidAt="{ row }">{{ row.paidAt ? formatDate(row.paidAt) : '-' }}</template>
        <template #createdAt="{ row }">{{ formatDate(row.createdAt) }}</template>
        <template #actions="{ row }">
          <!-- Element Plus 把插槽行推成 DefaultRow，而 handleClose/handleDetail
               形参是 PayOrder，故此处显式断言（数据来自本页查询，类型是可信的） -->
          <el-button
            v-if="row.status === 0"
            type="danger"
            link
            size="small"
            @click="handleClose(row as PayOrder)"
            >关闭</el-button
          >
          <el-button type="primary" link size="small" @click="handleDetail(row as PayOrder)"
            >详情</el-button
          >
        </template>
      </ResponsiveTable>

      <Pagination
        v-model:page="page"
        v-model:limit="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        :background="false"
        @pagination="loadData"
      />
    </el-card>

    <el-dialog v-model="detailVisible" title="订单详情" width="500px">
      <el-descriptions :column="1" border>
        <el-descriptions-item label="订单号">{{ detailData.orderNo }}</el-descriptions-item>
        <el-descriptions-item label="订单标题">{{ detailData.subject }}</el-descriptions-item>
        <el-descriptions-item label="金额"
          >¥{{ (detailData.amount / 100).toFixed(2) }}</el-descriptions-item
        >
        <el-descriptions-item label="渠道">{{
          getLabel('sys_pay_channel', detailData.channel)
        }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <DictTag type="sys_pay_order_status" :value="detailData.status" />
        </el-descriptions-item>
        <el-descriptions-item label="第三方交易号">{{
          detailData.tradeNo || '-'
        }}</el-descriptions-item>
        <el-descriptions-item label="支付时间">{{
          detailData.paidAt ? formatDate(detailData.paidAt) : '-'
        }}</el-descriptions-item>
        <el-descriptions-item label="创建时间">{{
          formatDate(detailData.createdAt)
        }}</el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getPayOrderList, closePayOrder, type PayOrder } from '@/api/payment'
import DictTag from '@/components/DictTag/index.vue'
import ResponsiveTable from '@/components/ResponsiveTable/index.vue'
import type { ResponsiveColumn } from '@/components/ResponsiveTable/types'
import { useDict } from '@/hooks/useDict'

// 列定义是唯一来源：桌面端表格列与手机端卡片字段都从这里派生
const columns: ResponsiveColumn<PayOrder>[] = [
  { label: '订单号', prop: 'orderNo', width: 200 },
  { label: '订单标题', prop: 'subject', minWidth: 150 },
  { label: '金额', slot: 'amount', width: 100, align: 'right' },
  { label: '渠道', slot: 'channel', width: 100, align: 'center' },
  { label: '状态', slot: 'status', width: 100, align: 'center' },
  { label: '第三方交易号', prop: 'tradeNo', width: 180 },
  { label: '支付时间', slot: 'paidAt', width: 180 },
  { label: '创建时间', slot: 'createdAt', width: 180 },
  { label: '操作', slot: 'actions', width: 120, fixed: 'right', hideInCard: true },
]

const loading = ref(false)
const tableData = ref<PayOrder[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)

const queryParams = reactive({
  subject: '',
  channel: '',
  status: '',
})

const detailVisible = ref(false)
const detailData = ref<PayOrder>({} as PayOrder)

// 渠道与状态选项由数据字典驱动（sys_pay_channel / sys_pay_order_status），
// 下面是字典缺失时的兜底 —— 字典被清空时筛选框不能变成空下拉。
const DEFAULT_CHANNELS = [
  { label: '微信支付', value: 'wechat', listClass: '', cssClass: '' },
  { label: '支付宝', value: 'alipay', listClass: '', cssClass: '' },
]
const DEFAULT_ORDER_STATUS = [
  { label: '待支付', value: '0', listClass: '', cssClass: '' },
  { label: '已支付', value: '1', listClass: '', cssClass: '' },
  { label: '已关闭', value: '2', listClass: '', cssClass: '' },
  { label: '已退款', value: '3', listClass: '', cssClass: '' },
  { label: '退款中', value: '4', listClass: '', cssClass: '' },
]

const { getList, getLabel } = useDict('sys_pay_channel', 'sys_pay_order_status')

function formatDate(dateStr: string) {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}

async function loadData() {
  loading.value = true
  try {
    const res = await getPayOrderList({
      subject: queryParams.subject,
      channel: queryParams.channel,
      status: queryParams.status,
      page: page.value,
      pageSize: pageSize.value,
    })
    tableData.value = res.data.list
    total.value = res.data.total
  } finally {
    loading.value = false
  }
}

async function handleClose(row: PayOrder) {
  try {
    await ElMessageBox.confirm(`确定要关闭订单「${row.orderNo}」吗？`, '提示', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning',
    })
    await closePayOrder(row.orderNo)
    ElMessage.success('订单已关闭')
    loadData()
  } catch (err) {
    // 用户点「取消」时 ElMessageBox 会 reject 出 'cancel' / 'close' 字符串，
    // 那是正常操作，不该记成错误；只有接口真的失败才留排查痕迹
    // （接口失败的提示由响应拦截器负责）
    if (err !== 'cancel' && err !== 'close') {
      console.warn('[payment] 关闭订单失败', err)
    }
  }
}

function handleDetail(row: PayOrder) {
  detailData.value = row
  detailVisible.value = true
}

onMounted(() => loadData())
</script>

<style lang="scss" scoped>
.amount-text {
  color: var(--el-color-danger);
  font-weight: bold;
}

.search-bar {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
}
</style>
