<template>
  <!--
    筛选区的移动端折叠。

    桌面端：**不渲染任何多余元素**（只有一层无布局样式的 div），DOM 与视觉和改造前一致。
    手机端：默认折叠，只留一个「筛选」按钮，点开才显示表单。

    为什么要做：8 个页面实测有 3~9 个筛选字段，而 `:xs="24"` 让每个字段在手机上
    占满一整行 —— 不折叠的话进页面要先划过一整屏才看到表格。

    为什么不用 `el-collapse`：它会带自己的边框、标题栏与展开动画，
    桌面端就得再写 CSS 把它「藏起来但内容常显」，反而更绕；
    这里只需要一个「手机上折叠」的开关，专用件比覆盖第三方样式更稳。
  -->
  <div class="collapsible-filter">
    <button
      v-if="isMobile"
      type="button"
      class="collapsible-filter__toggle"
      :aria-expanded="open"
      @click="open = !open"
    >
      <el-icon><Filter /></el-icon>
      <span>{{ open ? '收起筛选' : '筛选' }}</span>
      <el-icon class="collapsible-filter__arrow" :class="{ 'is-open': open }">
        <ArrowDown />
      </el-icon>
    </button>
    <!-- 用 v-show 而不是 v-if：折叠时保留表单内容与已填状态，展开不用重填 -->
    <div v-show="!isMobile || open" class="collapsible-filter__body">
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ArrowDown, Filter } from '@element-plus/icons-vue'
import { useResponsive } from '@/hooks/useResponsive'

const { isMobile } = useResponsive()

// 默认折叠：手机上进页面先看到表格，需要筛选再点开
const open = ref(false)
</script>

<style lang="scss" scoped>
.collapsible-filter__toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 8px 0;
  font-size: 14px;
  color: var(--color-text-secondary);
  cursor: pointer;
  background: none;
  border: none;
}

.collapsible-filter__arrow {
  margin-left: auto;
  transition: transform var(--transition-fast);

  &.is-open {
    transform: rotate(180deg);
  }
}
</style>
