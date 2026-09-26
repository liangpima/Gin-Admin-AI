<template>
  <div class="app-container">
    <el-card class="table-card">
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane label="操作日志" name="operation">
          <div class="search-form">
            <CollapsibleFilter>
              <el-form :model="opQuery">
                <el-row :gutter="16">
                  <el-col :xs="24" :sm="12" :md="8">
                    <el-form-item label="模块标题">
                      <el-input
                        v-model="opQuery.title"
                        placeholder="请输入"
                        clearable
                        @keyup.enter="loadOpLogs"
                      />
                    </el-form-item>
                  </el-col>
                  <el-col :xs="24" :sm="12" :md="8">
                    <el-form-item>
                      <el-button type="primary" @click="loadOpLogs">搜索</el-button>
                    </el-form-item>
                  </el-col>
                </el-row>
              </el-form>
            </CollapsibleFilter>
          </div>
          <ResponsiveTable :data="opLogs" :columns="opColumns" :loading="opLoading" :stripe="true">
            <template #opStatus="{ row }">
              <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">{{
                row.status === 1 ? '成功' : '失败'
              }}</el-tag>
            </template>
            <template #opCreatedAt="{ row }">{{ formatDateTime(row.createdAt) }}</template>
          </ResponsiveTable>
          <Pagination
            v-model:page="opQuery.page"
            :total="opTotal"
            layout="total, prev, pager, next"
            :background="false"
            @pagination="loadOpLogs"
          />
        </el-tab-pane>

        <el-tab-pane label="登录日志" name="login">
          <div class="search-form">
            <CollapsibleFilter>
              <el-form :model="loginQuery">
                <el-row :gutter="16">
                  <el-col :xs="24" :sm="12" :md="8">
                    <el-form-item label="用户名">
                      <el-input
                        v-model="loginQuery.username"
                        placeholder="请输入"
                        clearable
                        @keyup.enter="loadLoginLogs"
                      />
                    </el-form-item>
                  </el-col>
                  <el-col :xs="24" :sm="12" :md="8">
                    <el-form-item>
                      <el-button type="primary" @click="loadLoginLogs">搜索</el-button>
                    </el-form-item>
                  </el-col>
                </el-row>
              </el-form>
            </CollapsibleFilter>
          </div>
          <ResponsiveTable
            :data="loginLogs"
            :columns="loginColumns"
            :loading="loginLoading"
            :stripe="true"
          >
            <template #loginStatus="{ row }">
              <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">{{
                row.status === 1 ? '成功' : '失败'
              }}</el-tag>
            </template>
            <template #loginTime="{ row }">{{ formatDateTime(row.loginTime) }}</template>
          </ResponsiveTable>
          <Pagination
            v-model:page="loginQuery.page"
            :total="loginTotal"
            layout="total, prev, pager, next"
            :background="false"
            @pagination="loadLoginLogs"
          />
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { getOperationLogs, getLoginLogs, type OperationLogItem, LoginLogItem } from '@/api/log'
import { formatDateTime } from '@/utils/format'
import ResponsiveTable from '@/components/ResponsiveTable/index.vue'
import CollapsibleFilter from '@/components/CollapsibleFilter/index.vue'
import type { ResponsiveColumn } from '@/components/ResponsiveTable/types'

// 两个 Tab 各一份列定义。插槽名刻意加前缀（op*/login*）：
// 两个表格在同一个组件里，同名插槽会互相串用。
const opColumns: ResponsiveColumn<OperationLogItem>[] = [
  { label: 'ID', prop: 'id', width: 70 },
  { label: '模块标题', prop: 'title', width: 100 },
  { label: '操作人', prop: 'operatorName', width: 90 },
  { label: '请求方法', prop: 'requestMethod', width: 100 },
  { label: '请求URL', prop: 'requestUrl', minWidth: 150, showOverflowTooltip: true },
  { label: '状态', slot: 'opStatus', width: 70 },
  { label: 'IP', prop: 'ip', width: 120 },
  { label: '耗时(ms)', prop: 'costTime', width: 80 },
  { label: '操作时间', slot: 'opCreatedAt', width: 170 },
]

const loginColumns: ResponsiveColumn<LoginLogItem>[] = [
  { label: 'ID', prop: 'id', width: 70 },
  { label: '用户名', prop: 'username', width: 100 },
  { label: 'IP', prop: 'ip', width: 120 },
  { label: '浏览器', prop: 'browser', width: 120 },
  { label: '操作系统', prop: 'os', width: 120 },
  { label: '状态', slot: 'loginStatus', width: 70 },
  { label: '消息', prop: 'msg', minWidth: 100, showOverflowTooltip: true },
  { label: '登录时间', slot: 'loginTime', width: 170 },
]

const activeTab = ref('operation')

const opLoading = ref(false)
const opLogs = ref<OperationLogItem[]>([])
const opTotal = ref(0)
const opQuery = reactive({ title: '', page: 1, pageSize: 10 })

const loginLoading = ref(false)
const loginLogs = ref<LoginLogItem[]>([])
const loginTotal = ref(0)
const loginQuery = reactive({ username: '', page: 1, pageSize: 10 })

async function loadOpLogs() {
  opLoading.value = true
  try {
    const res = await getOperationLogs(opQuery)
    opLogs.value = res.data.list
    opTotal.value = res.data.total
  } finally {
    opLoading.value = false
  }
}

async function loadLoginLogs() {
  loginLoading.value = true
  try {
    const res = await getLoginLogs(loginQuery)
    loginLogs.value = res.data.list
    loginTotal.value = res.data.total
  } finally {
    loginLoading.value = false
  }
}

function handleTabChange() {
  if (activeTab.value === 'operation') loadOpLogs()
  else loadLoginLogs()
}

onMounted(() => {
  loadOpLogs()
})
</script>

<style lang="scss" scoped>
.table-card {
  :deep(.el-card__header) {
    border-bottom-color: var(--color-border-lighter);
  }

  :deep(.el-tabs__header) {
    margin-bottom: 16px;
  }
}
</style>
