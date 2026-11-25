<template>
  <section class="app-main">
    <!--
      `route` 取自 router-view 的插槽（而不是 useRoute()）：插槽里的才是
      **本次真正要渲染的**那条路由记录，两者在重定向/嵌套场景下可能不同。
      这里同时用它做 `:key` 与缓存名，避免出现「key 用 A、缓存名用 B」的错位。
    -->
    <!--
      `wrapForCacheName` 不是多余的：keep-alive 的 include 按**组件 name**
      匹配，而 cachedViews 存的是**路由 name**，本项目所有视图文件都叫
      index.vue（组件名恒为 "index"）—— 不包这一层，include 永远匹配不上，
      缓存静默失效。详见 utils/keepAlive.ts 的注释。

      这段注释必须放在 keep-alive **外面**：Vue 编译器要求 keep-alive
      恰好一个子节点，而 dev 模式下 HTML 注释也会被编译成子节点（
      X_KEEP_ALIVE_INVALID_CHILDREN）。
    -->
    <router-view v-slot="{ Component, route: current }">
      <transition name="fade-transform" mode="out-in">
        <keep-alive :include="cachedViews">
          <component :is="wrapForCacheName(Component, current.name)" :key="current.path" />
        </keep-alive>
      </transition>
    </router-view>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useTagsViewStore } from '@/store/modules/tagsView'
import { wrapForCacheName } from '@/utils/keepAlive'

const tagsViewStore = useTagsViewStore()

const cachedViews = computed(() => tagsViewStore.cachedViews)
</script>

<style lang="scss" scoped>
@use '@/assets/styles/responsive.scss' as *;

.app-main {
  flex: 1;
  padding: 20px;
  overflow: auto;
  background: var(--color-bg-page);

  @include mobile {
    padding: 12px;
  }

  @include tablet {
    padding: 16px;
  }
}
</style>
