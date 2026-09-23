<template>
  <div class="app-container">
    <el-card class="table-card">
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane label="操作日志" name="operation">
          <div class="search-form">
            <el-form :model="opQuery">
              <el-row :gutter="16">
                <el-col :xs="24" :sm="12" :md="8">
                  <el-form-item label="模块标题">
                    <el-input v-model="opQuery.title" placeholder="请输入" clearable @keyup.enter="loadOpLogs" />
                  </el-form-item>
                </el-col>
                <el-col :xs="24" :sm="12" :md="8">
                  <el-form-item>
                    <el-button type="primary" @click="loadOpLogs">搜索</el-button>
                  </el-form-item>
                </el-col>
              </el-row>
            </el-form>
          </div>
          <el-table :data="opLogs" v-loading="opLoading" border stripe>
            <el-table-column prop="id" label="ID" width="70" />
            <el-table-column prop="title" label="模块标题" width="100" />
            <el-table-column prop="operatorName" label="操作人" width="90" />
            <el-table-column prop="requestMethod" label="请求方法" width="100" />
            <el-table-column prop="requestUrl" label="请求URL" min-width="150" show-overflow-tooltip />
            <el-table-column prop="status" label="状态" width="70">
              <template #default="{ row }">
                <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">{{ row.status === 1 ? '成功' : '失败' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="ip" label="IP" width="120" />
            <el-table-column prop="costTime" label="耗时(ms)" width="80" />
            <el-table-column label="操作时间" width="170">
              <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
            </el-table-column>
          </el-table>
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
            <el-form :model="loginQuery">
              <el-row :gutter="16">
                <el-col :xs="24" :sm="12" :md="8">
                  <el-form-item label="用户名">
                    <el-input v-model="loginQuery.username" placeholder="请输入" clearable @keyup.enter="loadLoginLogs" />
                  </el-form-item>
                </el-col>
                <el-col :xs="24" :sm="12" :md="8">
                  <el-form-item>
                    <el-button type="primary" @click="loadLoginLogs">搜索</el-button>
                  </el-form-item>
                </el-col>
              </el-row>
            </el-form>
          </div>
          <el-table :data="loginLogs" v-loading="loginLoading" border stripe>
            <el-table-column prop="id" label="ID" width="70" />
            <el-table-column prop="username" label="用户名" width="100" />
            <el-table-column prop="ip" label="IP" width="120" />
            <el-table-column prop="browser" label="浏览器" width="120" />
            <el-table-column prop="os" label="操作系统" width="120" />
            <el-table-column prop="status" label="状态" width="70">
              <template #default="{ row }">
                <el-tag :type="row.status === 1 ? 'success' : 'danger'" size="small">{{ row.status === 1 ? '成功' : '失败' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="msg" label="消息" min-width="100" show-overflow-tooltip />
            <el-table-column label="登录时间" width="170">
              <template #default="{ row }">{{ formatDateTime(row.loginTime) }}</template>
            </el-table-column>
          </el-table>
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
import { getOperationLogs, getLoginLogs , type OperationLogItem, LoginLogItem} from '@/api/log'
import { formatDateTime } from '@/utils/format'

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
  } finally { opLoading.value = false }
}

async function loadLoginLogs() {
  loginLoading.value = true
  try {
    const res = await getLoginLogs(loginQuery)
    loginLogs.value = res.data.list
    loginTotal.value = res.data.total
  } finally { loginLoading.value = false }
}

function handleTabChange() {
  if (activeTab.value === 'operation') loadOpLogs()
  else loadLoginLogs()
}

onMounted(() => { loadOpLogs() })
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
