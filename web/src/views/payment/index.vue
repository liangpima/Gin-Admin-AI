<template>
  <div class="app-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>支付订单</span>
        </div>
      </template>

      <div class="search-bar">
        <el-input v-model="queryParams.subject" placeholder="搜索订单标题" clearable style="width: 200px" @keyup.enter="loadData" @clear="loadData" />
        <el-select v-model="queryParams.channel" placeholder="支付渠道" clearable style="width: 140px" @change="loadData">
          <el-option v-for="opt in getList('sys_pay_channel', DEFAULT_CHANNELS)" :key="opt.value" :label="opt.label" :value="opt.value" />
        </el-select>
        <el-select v-model="queryParams.status" placeholder="订单状态" clearable style="width: 140px" @change="loadData">
          <el-option v-for="opt in getList('sys_pay_order_status', DEFAULT_ORDER_STATUS)" :key="opt.value" :label="opt.label" :value="opt.value" />
        </el-select>
        <el-button type="primary" @click="loadData">搜索</el-button>
      </div>

      <el-table :data="tableData" v-loading="loading" stripe>
        <el-table-column prop="orderNo" label="订单号" width="200" />
        <el-table-column prop="subject" label="订单标题" min-width="150" />
        <el-table-column label="金额" width="100" align="right">
          <template #default="{ row }">
            <span class="amount-text">¥{{ (row.amount / 100).toFixed(2) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="渠道" width="100" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.channel === 'wechat'" type="success" size="small">微信</el-tag>
            <el-tag v-else-if="row.channel === 'alipay'" type="primary" size="small">支付宝</el-tag>
            <el-tag v-else size="small">{{ row.channel }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <DictTag type="sys_pay_order_status" :value="row.status" />
          </template>
        </el-table-column>
        <el-table-column prop="tradeNo" label="第三方交易号" width="180" />
        <el-table-column label="支付时间" width="180">
          <template #default="{ row }">
            {{ row.paidAt ? formatDate(row.paidAt) : '-' }}
          </template>
        </el-table-column>
        <el-table-column label="创建时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.createdAt) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120" fixed="right">
          <!-- Element Plus 把插槽行推成 DefaultRow，而 handleClose/handleDetail
               形参是 PayOrder，故此处显式断言（数据来自本页查询，类型是可信的） -->
          <template #default="{ row }">
            <el-button v-if="row.status === 0" type="danger" link size="small" @click="handleClose(row as PayOrder)">关闭</el-button>
            <el-button type="primary" link size="small" @click="handleDetail(row as PayOrder)">详情</el-button>
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

    <el-dialog v-model="detailVisible" title="订单详情" width="500px">
      <el-descriptions :column="1" border>
        <el-descriptions-item label="订单号">{{ detailData.orderNo }}</el-descriptions-item>
        <el-descriptions-item label="订单标题">{{ detailData.subject }}</el-descriptions-item>
        <el-descriptions-item label="金额">¥{{ (detailData.amount / 100).toFixed(2) }}</el-descriptions-item>
        <el-descriptions-item label="渠道">{{ getLabel('sys_pay_channel', detailData.channel) }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <DictTag type="sys_pay_order_status" :value="detailData.status" />
        </el-descriptions-item>
        <el-descriptions-item label="第三方交易号">{{ detailData.tradeNo || '-' }}</el-descriptions-item>
        <el-descriptions-item label="支付时间">{{ detailData.paidAt ? formatDate(detailData.paidAt) : '-' }}</el-descriptions-item>
        <el-descriptions-item label="创建时间">{{ formatDate(detailData.createdAt) }}</el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { getPayOrderList, closePayOrder, type PayOrder } from '@/api/payment'
import DictTag from '@/components/DictTag/index.vue'
import { useDict } from '@/hooks/useDict'

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
  } catch {}
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
