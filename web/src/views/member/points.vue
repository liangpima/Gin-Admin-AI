<template>
  <div class="app-container">
    <div class="search-form">
      <el-form :model="queryParams">
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="会员ID">
              <el-input
                v-model.number="queryParams.memberId"
                placeholder="请输入会员ID"
                clearable
                @keyup.enter="handleSearch"
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="类型">
              <el-select
                v-model="queryParams.type"
                placeholder="全部"
                clearable
                style="width: 100%"
              >
                <el-option label="获取" :value="1" />
                <el-option label="消费" :value="2" />
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
          <span>积分明细</span>
        </div>
      </template>

      <ResponsiveTable :data="tableData" :columns="columns" :loading="loading">
        <template #change="{ row }">
          <span :class="row.change > 0 ? 'points-positive' : 'points-negative'">
            {{ row.change > 0 ? '+' : '' }}{{ row.change }}
          </span>
        </template>
        <template #type="{ row }">
          <el-tag :type="row.type === 1 ? 'success' : 'warning'" size="small">{{
            row.type === 1 ? '获取' : '消费'
          }}</el-tag>
        </template>
        <template #createdAt="{ row }">{{ formatDateTime(row.createdAt) }}</template>
      </ResponsiveTable>

      <Pagination
        v-model:page="queryParams.page"
        v-model:limit="queryParams.pageSize"
        :total="total"
        layout="total, prev, pager, next"
        :background="false"
        @pagination="loadData"
      />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { getPointsLogList, type PointsLogItem } from '@/api/member'
import { formatDateTime } from '@/utils/format'
import ResponsiveTable from '@/components/ResponsiveTable/index.vue'
import type { ResponsiveColumn } from '@/components/ResponsiveTable/types'

// 列定义是唯一来源：桌面端表格列与手机端卡片字段都从这里派生。
// 本页是只读列表，没有操作列。
const columns: ResponsiveColumn<PointsLogItem>[] = [
  { label: 'ID', prop: 'id', width: 60 },
  { label: '会员ID', prop: 'memberId', width: 80 },
  { label: '变更积分', slot: 'change', width: 100, align: 'right' },
  { label: '类型', slot: 'type', width: 80 },
  { label: '来源', prop: 'source', minWidth: 120 },
  { label: '关联订单号', prop: 'orderNo', minWidth: 160 },
  { label: '备注', prop: 'remark', minWidth: 120, showOverflowTooltip: true },
  { label: '时间', slot: 'createdAt', width: 170 },
]

const loading = ref(false)
const tableData = ref<PointsLogItem[]>([])
const total = ref(0)

const queryParams = reactive({
  memberId: undefined as number | undefined,
  type: undefined as number | undefined,
  page: 1,
  pageSize: 10,
})

async function loadData() {
  loading.value = true
  try {
    const res = await getPointsLogList(queryParams)
    tableData.value = res.data.list
    total.value = res.data.total
  } finally {
    loading.value = false
  }
}

function handleSearch() {
  queryParams.page = 1
  loadData()
}

function handleReset() {
  queryParams.memberId = undefined
  queryParams.type = undefined
  handleSearch()
}

onMounted(() => loadData())
</script>

<style lang="scss" scoped>
.points-positive {
  color: var(--el-color-success);
  font-weight: 500;
}

.points-negative {
  color: var(--el-color-danger);
  font-weight: 500;
}

.table-card {
  :deep(.el-card__header) {
    border-bottom-color: var(--color-border-lighter);
  }
}
</style>
