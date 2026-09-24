<template>
  <div class="error-page">
    <div class="error-content">
      <div class="error-illustration">
        <svg viewBox="0 0 400 300" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <linearGradient id="grad1" x1="0%" y1="0%" x2="100%" y2="100%">
              <stop offset="0%" style="stop-color: var(--color-primary); stop-opacity: 0.2" />
              <stop offset="100%" style="stop-color: var(--color-primary); stop-opacity: 0.05" />
            </linearGradient>
          </defs>
          <!-- Background -->
          <rect width="400" height="300" fill="url(#grad1)" rx="16" />
          <!-- Buildings -->
          <rect
            x="40"
            y="120"
            width="60"
            height="180"
            rx="4"
            fill="var(--color-border-base)"
            opacity="0.3"
          />
          <rect
            x="50"
            y="130"
            width="15"
            height="15"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.3"
          />
          <rect
            x="75"
            y="130"
            width="15"
            height="15"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.2"
          />
          <rect
            x="50"
            y="155"
            width="15"
            height="15"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.2"
          />
          <rect
            x="75"
            y="155"
            width="15"
            height="15"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.3"
          />

          <rect
            x="120"
            y="80"
            width="80"
            height="220"
            rx="4"
            fill="var(--color-border-base)"
            opacity="0.4"
          />
          <rect
            x="130"
            y="90"
            width="18"
            height="18"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.3"
          />
          <rect
            x="158"
            y="90"
            width="18"
            height="18"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.2"
          />
          <rect
            x="130"
            y="118"
            width="18"
            height="18"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.2"
          />
          <rect
            x="158"
            y="118"
            width="18"
            height="18"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.3"
          />
          <rect
            x="130"
            y="146"
            width="18"
            height="18"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.3"
          />
          <rect
            x="158"
            y="146"
            width="18"
            height="18"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.2"
          />

          <rect
            x="220"
            y="140"
            width="70"
            height="160"
            rx="4"
            fill="var(--color-border-base)"
            opacity="0.35"
          />
          <rect
            x="230"
            y="150"
            width="15"
            height="15"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.2"
          />
          <rect
            x="255"
            y="150"
            width="15"
            height="15"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.3"
          />
          <rect
            x="230"
            y="175"
            width="15"
            height="15"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.3"
          />
          <rect
            x="255"
            y="175"
            width="15"
            height="15"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.2"
          />

          <rect
            x="310"
            y="160"
            width="50"
            height="140"
            rx="4"
            fill="var(--color-border-base)"
            opacity="0.25"
          />
          <rect
            x="318"
            y="168"
            width="12"
            height="12"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.2"
          />
          <rect
            x="340"
            y="168"
            width="12"
            height="12"
            rx="2"
            fill="var(--color-primary)"
            opacity="0.3"
          />

          <!-- 404 text -->
          <text
            x="200"
            y="170"
            text-anchor="middle"
            font-size="72"
            font-weight="700"
            fill="var(--color-primary)"
            opacity="0.9"
          >
            404
          </text>
        </svg>
      </div>

      <h1 class="error-title">页面不存在</h1>
      <p class="error-desc">抱歉，您访问的页面不存在或已被移除</p>

      <div class="error-actions">
        <el-button type="primary" size="large" class="error-btn" @click="$router.push('/')">
          <el-icon><HomeFilled /></el-icon>
          返回首页
        </el-button>
        <el-button size="large" class="error-btn-outline" @click="$router.back()">
          <el-icon><Back /></el-icon>
          返回上页
        </el-button>
      </div>
    </div>
  </div>
</template>

<style lang="scss" scoped>
@use '@/assets/styles/responsive.scss' as *;

.error-page {
  min-height: 80vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--spacing-xl);
}

.error-content {
  text-align: center;
  max-width: 500px;
  animation: fadeInUp 0.5s ease;
}

@keyframes fadeInUp {
  from {
    opacity: 0;
    transform: translateY(20px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.error-illustration {
  margin-bottom: var(--spacing-xl);

  svg {
    width: 100%;
    max-width: 400px;
    height: auto;
  }
}

.error-title {
  font-size: var(--font-size-2xl);
  font-weight: var(--font-weight-bold);
  color: var(--color-text-primary);
  margin: 0 0 var(--spacing-sm);
}

.error-desc {
  font-size: var(--font-size-base);
  color: var(--color-text-secondary);
  margin: 0 0 var(--spacing-2xl);
}

.error-actions {
  display: flex;
  gap: var(--spacing-md);
  justify-content: center;

  @include mobile {
    flex-direction: column;
  }
}

.error-btn {
  min-width: 140px;
  height: 40px;
  border-radius: var(--radius-md);
}

.error-btn-outline {
  min-width: 140px;
  height: 40px;
  border-radius: var(--radius-md);
}
</style>
