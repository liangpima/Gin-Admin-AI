<template>
  <div class="app-container">
    <div class="search-form">
      <el-form :model="queryParams">
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="会员ID">
              <el-input v-model.number="queryParams.memberId" placeholder="请输入会员ID" clearable @keyup.enter="handleSearch" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="8" :lg="6">
            <el-form-item label="类型">
              <el-select v-model="queryParams.type" placeholder="全部" clearable style="width: 100%">
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

      <el-table :data="tableData" v-loading="loading" border>
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column prop="memberId" label="会员ID" width="80" />
        <el-table-column label="变更积分" width="100" align="right">
          <template #default="{ row }">
            <span :class="row.change > 0 ? 'points-positive' : 'points-negative'">
              {{ row.change > 0 ? '+' : '' }}{{ row.change }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="80">
          <template #default="{ row }">
            <el-tag :type="row.type === 1 ? 'success' : 'warning'" size="small">{{ row.type === 1 ? '获取' : '消费' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="source" label="来源" min-width="120" />
        <el-table-column prop="orderNo" label="关联订单号" min-width="160" />
        <el-table-column prop="remark" label="备注" min-width="120" show-overflow-tooltip />
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        </el-table-column>
      </el-table>

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
import { getPointsLogList , type PointsLogItem} from '@/api/member'
import { formatDateTime } from '@/utils/format'

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
