<template>
  <div class="dashboard">
    <el-row :gutter="16">
      <el-col :xs="24" :sm="12" :md="6" v-for="(item, index) in statCards" :key="item.label">
        <div class="stat-card" :class="item.colorClass" :style="{ animationDelay: `${index * 0.1}s` }">
          <div class="stat-card__icon">
            <el-icon :size="24"><component :is="item.icon" /></el-icon>
          </div>
          <div class="stat-card__info">
            <div class="stat-card__value">{{ animatedValues[index] }}</div>
            <div class="stat-card__label">{{ item.label }}</div>
          </div>
        </div>
      </el-col>
    </el-row>

    <div class="welcome-card">
      <div class="welcome-card__header">
        <h3>欢迎使用 Gin-Admin</h3>
      </div>
      <div class="welcome-card__body">
        <p>Gin-Admin 是一套基于 Go + Gin + GORM + Vue3 + Element Plus 的后台管理框架。</p>
        <p>适用于活动管理、CRM、Agent管理平台、企业内部管理系统等中大型业务系统。</p>

        <div class="tech-stack">
          <h4>技术栈</h4>
          <div class="tech-tags">
            <el-tag>Go 1.24+</el-tag>
            <el-tag type="success">Gin</el-tag>
            <el-tag type="warning">GORM</el-tag>
            <el-tag type="danger">MySQL</el-tag>
            <el-tag type="info">Redis</el-tag>
            <el-tag>Vue3</el-tag>
            <el-tag type="success">Vite</el-tag>
            <el-tag type="warning">Element Plus</el-tag>
          </div>
        </div>

        <div class="modules-grid">
          <div class="module-item" v-for="mod in modules" :key="mod.title">
            <div class="module-item__icon" :class="mod.colorClass">
              <el-icon :size="18"><component :is="mod.icon" /></el-icon>
            </div>
            <div class="module-item__info">
              <div class="module-item__title">{{ mod.title }}</div>
              <div class="module-item__desc">{{ mod.desc }}</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { getDashboardStats, type DashboardStats } from '@/api/dashboard'

const stats = ref<DashboardStats>({
  userCount: 0,
  roleCount: 0,
  menuCount: 0,
  deptCount: 0,
  postCount: 0,
  configCount: 0,
  logCount: 0,
})

const animatedValues = ref<number[]>([0, 0, 0, 0])

const statCards = computed(() => [
  { label: '用户总数', icon: 'User', colorClass: 'color-primary' },
  { label: '角色数量', icon: 'UserFilled', colorClass: 'color-success' },
  { label: '菜单数量', icon: 'Menu', colorClass: 'color-warning' },
  { label: '部门数量', icon: 'OfficeBuilding', colorClass: 'color-danger' },
])

const modules = [
  { title: '用户管理', desc: '用户增删改查', icon: 'User', colorClass: 'color-primary' },
  { title: '角色管理', desc: '权限分配', icon: 'UserFilled', colorClass: 'color-success' },
  { title: '菜单管理', desc: '三级菜单', icon: 'Menu', colorClass: 'color-warning' },
  { title: '部门管理', desc: '树形结构', icon: 'OfficeBuilding', colorClass: 'color-danger' },
  { title: '系统设置', desc: '系统配置', icon: 'Tools', colorClass: 'color-info' },
  { title: '参数管理', desc: '参数配置', icon: 'Setting', colorClass: 'color-primary' },
  { title: '数据字典', desc: '字典维护', icon: 'Notebook', colorClass: 'color-success' },
  { title: '日志管理', desc: '操作与登录日志', icon: 'Document', colorClass: 'color-warning' },
]

function animateCount(target: number, index: number) {
  if (target === 0) {
    animatedValues.value[index] = 0
    return
  }
  const duration = 800
  const step = target / (duration / 16)
  let current = 0
  const timer = setInterval(() => {
    current += step
    if (current >= target) {
      animatedValues.value[index] = target
      clearInterval(timer)
    } else {
      animatedValues.value[index] = Math.floor(current)
    }
  }, 16)
}

onMounted(async () => {
  try {
    const res = await getDashboardStats()
    stats.value = res.data
    const values = [res.data.userCount, res.data.roleCount, res.data.menuCount, res.data.deptCount]
    values.forEach((v, i) => animateCount(v, i))
  } catch (err) {
    // 统计加载失败时保持 0 值展示，页面其余部分仍然可用
    console.warn('[dashboard] 统计数据加载失败', err)
  }
})
</script>

<style lang="scss" scoped>
.dashboard {
  animation: fadeInUp 0.4s ease;
}

@keyframes fadeInUp {
  from {
    opacity: 0;
    transform: translateY(12px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

// Color classes for stat cards and module icons
.color-primary {
  --card-color: var(--el-color-primary);
  --card-bg: var(--el-color-primary-light-9);
}
.color-success {
  --card-color: var(--el-color-success);
  --card-bg: var(--el-color-success-light-9);
}
.color-warning {
  --card-color: var(--el-color-warning);
  --card-bg: var(--el-color-warning-light-9);
}
.color-danger {
  --card-color: var(--el-color-danger);
  --card-bg: var(--el-color-danger-light-9);
}
.color-info {
  --card-color: var(--el-color-info);
  --card-bg: var(--el-color-info-light-9);
}

.stat-card {
  background: var(--color-bg-card);
  border-radius: var(--card-radius);
  padding: var(--spacing-xl);
  display: flex;
  align-items: center;
  gap: var(--spacing-lg);
  border: 1px solid var(--color-border-lighter);
  box-shadow: var(--shadow-sm);
  transition: all var(--transition-normal);
  animation: fadeInUp 0.5s ease both;

  &:hover {
    transform: translateY(-2px);
    box-shadow: var(--shadow-md);
    border-color: var(--card-color);
  }
}

.stat-card__icon {
  width: 52px;
  height: 52px;
  border-radius: var(--radius-lg);
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--card-bg);
  color: var(--card-color);
  flex-shrink: 0;
  transition: transform var(--transition-fast);

  .stat-card:hover & {
    transform: scale(1.05);
  }
}

.stat-card__info {
  flex: 1;
}

.stat-card__value {
  font-size: 28px;
  font-weight: var(--font-weight-bold);
  color: var(--color-text-primary);
  line-height: 1;
  font-variant-numeric: tabular-nums;
}

.stat-card__label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  margin-top: var(--spacing-xs);
}

.welcome-card {
  margin-top: var(--spacing-xl);
  background: var(--color-bg-card);
  border-radius: var(--card-radius);
  border: 1px solid var(--color-border-lighter);
  box-shadow: var(--shadow-sm);
  overflow: hidden;
  animation: fadeInUp 0.6s ease both;
}

.welcome-card__header {
  padding: var(--spacing-lg) var(--spacing-xl);
  border-bottom: 1px solid var(--color-border-lighter);

  h3 {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
  }
}

.welcome-card__body {
  padding: var(--spacing-xl);

  p {
    color: var(--color-text-regular);
    line-height: var(--line-height-relaxed);
    margin: 0 0 var(--spacing-sm);
  }
}

.tech-stack {
  margin-top: var(--spacing-xl);

  h4 {
    margin: 0 0 var(--spacing-md);
    font-size: var(--font-size-base);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
  }
}

.tech-tags {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-sm);
}

.modules-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: var(--spacing-md);
  margin-top: var(--spacing-xl);
}

.module-item {
  display: flex;
  align-items: center;
  gap: var(--spacing-md);
  padding: var(--spacing-md);
  border-radius: var(--radius-md);
  border: 1px solid var(--color-border-lighter);
  transition: all var(--transition-fast);

  &:hover {
    background: var(--color-bg-hover);
    border-color: var(--color-primary-200);
  }
}

.module-item__icon {
  width: 36px;
  height: 36px;
  border-radius: var(--radius-md);
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--card-bg);
  color: var(--card-color);
  flex-shrink: 0;
}

.module-item__title {
  font-size: var(--font-size-base);
  font-weight: var(--font-weight-medium);
  color: var(--color-text-primary);
}

.module-item__desc {
  font-size: var(--font-size-xs);
  color: var(--color-text-secondary);
  margin-top: 2px;
}
</style>
