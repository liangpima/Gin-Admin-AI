<template>
  <div class="login-container">
    <div class="login-bg">
      <div class="login-bg__pattern"></div>
      <div class="login-bg__content">
        <h1 class="login-bg__title">{{ siteInfo.name || 'Gin-Admin' }}</h1>
        <p class="login-bg__subtitle">{{ siteInfo.title || '后台管理系统' }}</p>
        <div class="login-bg__features">
          <div class="feature-item">
            <el-icon :size="20"><Check /></el-icon>
            <span>基于 Go + Gin + GORM</span>
          </div>
          <div class="feature-item">
            <el-icon :size="20"><Check /></el-icon>
            <span>Vue3 + Element Plus</span>
          </div>
          <div class="feature-item">
            <el-icon :size="20"><Check /></el-icon>
            <span>RBAC 权限管理</span>
          </div>
          <div class="feature-item">
            <el-icon :size="20"><Check /></el-icon>
            <span>多租户支持</span>
          </div>
        </div>
      </div>
    </div>

    <div class="login-form-wrapper">
      <div class="login-form-container">
        <div class="login-form-header">
          <div class="login-form-logo">
            <img v-if="siteInfo.logo" :src="siteInfo.logo" style="height: 40px; width: auto; object-fit: contain; border-radius: 8px;" />
            <svg v-else viewBox="0 0 32 32" width="40" height="40" fill="none" xmlns="http://www.w3.org/2000/svg">
              <rect width="32" height="32" rx="8" fill="var(--color-primary)"/>
              <path d="M8 16L16 8L24 16L16 24L8 16Z" fill="white" opacity="0.9"/>
            </svg>
          </div>
          <h2 class="login-form-title">{{ siteInfo.name || '欢迎回来' }}</h2>
          <p class="login-form-desc">{{ siteInfo.title || '请登录您的账户' }}</p>
        </div>

        <el-form ref="loginFormRef" :model="loginForm" :rules="loginRules" class="login-form">
          <el-form-item prop="username">
            <el-input
              v-model="loginForm.username"
              placeholder="请输入用户名"
              prefix-icon="User"
              size="large"
            />
          </el-form-item>

          <el-form-item prop="password">
            <el-input
              v-model="loginForm.password"
              type="password"
              placeholder="请输入密码"
              prefix-icon="Lock"
              size="large"
              show-password
              @keyup.enter="handleLogin"
            />
          </el-form-item>

          <el-form-item>
            <ClickCaptcha ref="captchaRef" @success="onCaptchaSuccess" />
          </el-form-item>

          <el-button
            :loading="loading"
            type="primary"
            size="large"
            class="login-btn"
            @click="handleLogin"
          >
            登 录
          </el-button>
        </el-form>

        <div class="login-form-footer">
          <span>默认账号: admin / admin123</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useUserStore } from '@/store/modules/user'
import { ElMessage, type FormInstance } from 'element-plus'
import { getSiteInfo } from '@/api/config'
import ClickCaptcha from '@/components/ClickCaptcha/index.vue'

const router = useRouter()
const userStore = useUserStore()

const loginFormRef = ref<FormInstance>()
const captchaRef = ref<InstanceType<typeof ClickCaptcha>>()
const loading = ref(false)
const captchaVerified = ref(false)
const captchaToken = ref('')
const siteInfo = reactive({
  title: '',
  name: '',
  logo: '',
})

// 不要预填任何默认账号密码：生产构建会随浏览器分发，
// 预填 admin/admin123 等于把默认凭据公开给所有人，
// 也会让后端「生产环境拒绝默认密钥启动」的防护形同虚设。
const loginForm = reactive({
  username: '',
  password: '',
})

const loginRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
}

async function loadSiteInfo() {
  try {
    const res = await getSiteInfo()
    const data = res.data || {}
    siteInfo.title = data['site.title'] || ''
    siteInfo.name = data['site.name'] || ''
    siteInfo.logo = data['site.logo'] || ''
    if (siteInfo.title) {
      document.title = siteInfo.title
    }
  } catch (err) {
    // 站点信息是登录页的锦上添花，取不到就保留默认文案，不阻断登录
    console.warn('[login] 站点信息加载失败，将使用默认标题', err)
  }
}

function onCaptchaSuccess(token: string) {
  captchaVerified.value = true
  captchaToken.value = token
}

function resetCaptcha() {
  captchaVerified.value = false
  captchaToken.value = ''
  // 验证码是一次性的（后端校验通过即作废），登录失败后必须重新验证：
  // reset() 会清掉「已验证」状态并丢弃当前图，用户点触发器会拿到新图
  captchaRef.value?.reset()
}

async function handleLogin() {
  const valid = await loginFormRef.value?.validate().catch(() => false)
  if (!valid) return

  if (!captchaVerified.value) {
    ElMessage.warning('请先完成验证码')
    return
  }

  loading.value = true
  try {
    await userStore.login(loginForm.username, loginForm.password, captchaToken.value)
    ElMessage.success('登录成功')
    router.push('/')
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '登录失败')
    // 验证码一次性，登录失败后必须重新验证
    resetCaptcha()
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadSiteInfo()
})
</script>

<style lang="scss" scoped>
@use '@/assets/styles/responsive.scss' as *;

.login-container {
  min-height: 100vh;
  display: flex;
  background: var(--color-bg-page);
  overflow: visible;
}

// Left panel
.login-bg {
  flex: 1;
  background: linear-gradient(135deg, #1a1c2e 0%, #1a365d 50%, #0f2440 100%);
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
  overflow: hidden;
  padding: 40px;

  @include mobile {
    display: none;
  }
}

.login-bg__pattern {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-image:
    radial-gradient(circle at 20% 80%, rgba(64, 158, 255, 0.15) 0%, transparent 50%),
    radial-gradient(circle at 80% 20%, rgba(103, 194, 58, 0.1) 0%, transparent 50%),
    radial-gradient(circle at 40% 40%, rgba(230, 162, 60, 0.08) 0%, transparent 50%);
  pointer-events: none;
}

.login-bg__content {
  position: relative;
  z-index: 1;
  max-width: 480px;
}

.login-bg__title {
  font-size: 48px;
  font-weight: 700;
  color: #fff;
  margin: 0 0 8px;
  letter-spacing: -0.5px;
}

.login-bg__subtitle {
  font-size: 18px;
  color: rgba(255, 255, 255, 0.6);
  margin: 0 0 40px;
}

.login-bg__features {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.feature-item {
  display: flex;
  align-items: center;
  gap: 12px;
  color: rgba(255, 255, 255, 0.8);
  font-size: 15px;

  .el-icon {
    color: var(--color-success);
  }
}

// Right panel - form
.login-form-wrapper {
  width: 480px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px;
  background: var(--color-bg-card);
  border-left: 1px solid var(--color-border-lighter);
  overflow: visible;

  @include mobile {
    width: 100%;
    border-left: none;
    padding: 24px;
  }
}

.login-form-container {
  width: 100%;
  max-width: 360px;
  overflow: visible;
}

.login-form-header {
  margin-bottom: 36px;
  text-align: center;
}

.login-form-logo {
  display: flex;
  justify-content: center;
  margin-bottom: 20px;
}

.login-form-title {
  font-size: var(--font-size-2xl);
  font-weight: var(--font-weight-bold);
  color: var(--color-text-primary);
  margin: 0 0 8px;
}

.login-form-desc {
  font-size: var(--font-size-base);
  color: var(--color-text-secondary);
  margin: 0;
}

.login-form {
  :deep(.el-input__wrapper) {
    padding: 4px 12px;
    border-radius: var(--radius-md);
    box-shadow: 0 0 0 1px var(--color-border-base) inset;
    transition: all var(--transition-fast);

    &:hover {
      box-shadow: 0 0 0 1px var(--color-primary-300) inset;
    }

    &.is-focus {
      box-shadow: 0 0 0 1px var(--color-primary) inset, 0 0 0 3px var(--color-primary-50);
    }
  }

  :deep(.el-form-item) {
    margin-bottom: 24px;
  }

  :deep(.el-form-item__content) {
    overflow: visible !important;
    height: auto !important;
  }
}

.login-btn {
  width: 100%;
  height: 44px;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  border-radius: var(--radius-md);
  margin-top: 8px;
  background: linear-gradient(135deg, var(--color-primary) 0%, var(--color-primary-600) 100%);
  border: none;
  transition: all var(--transition-fast);

  &:hover {
    transform: translateY(-1px);
    box-shadow: 0 4px 12px var(--color-primary-300);
  }

  &:active {
    transform: translateY(0);
  }
}

.login-form-footer {
  margin-top: 24px;
  text-align: center;
  font-size: var(--font-size-xs);
  color: var(--color-text-placeholder);
}
</style>
