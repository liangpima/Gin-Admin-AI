<template>
  <div class="tags-view-container">
    <el-scrollbar class="tags-scrollbar">
      <div class="tags-view-wrapper">
        <router-link
          v-for="tag in visitedViews"
          :key="tag.path"
          :to="tag.path"
          :class="isActive(tag) ? 'active' : ''"
          class="tags-view-item"
        >
          <span class="tags-view-item__text">{{ tag.title }}</span>
          <el-icon
            v-if="!tag.meta?.affix"
            class="tags-view-item__close"
            @click.prevent.stop="closeTag(tag)"
          >
            <Close />
          </el-icon>
        </router-link>
      </div>
    </el-scrollbar>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useTagsViewStore, type TagView } from '@/store/modules/tagsView'

const route = useRoute()
const router = useRouter()
const tagsViewStore = useTagsViewStore()

const visitedViews = computed(() => tagsViewStore.visitedViews)

function isActive(tag: TagView): boolean {
  return tag.path === route.path
}

function closeTag(tag: TagView) {
  tagsViewStore.delView(tag)
  if (isActive(tag)) {
    const views = tagsViewStore.visitedViews
    if (views.length > 0) {
      const lastView = views[views.length - 1]
      router.push(lastView.path).catch(() => {})
    } else {
      router.push('/').catch(() => {})
    }
  }
}
</script>

<style lang="scss" scoped>
.tags-view-container {
  height: 34px;
  background: var(--el-bg-color);
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.tags-view-wrapper {
  display: flex;
  align-items: center;
  padding: 0 8px;
  height: 34px;
  gap: 6px;
}

.tags-view-item {
  display: inline-flex;
  align-items: center;
  padding: 0 10px;
  height: 26px;
  border: 1px solid var(--el-border-color);
  color: var(--el-text-color-regular);
  background: var(--el-bg-color);
  font-size: 12px;
  text-decoration: none;
  border-radius: 3px;
  cursor: pointer;
  transition: all 0.15s;
  white-space: nowrap;

  &:hover {
    color: var(--el-color-primary);
    border-color: var(--el-color-primary-light-5);
    background: var(--el-color-primary-light-9);
  }

  &.active {
    background-color: var(--el-color-primary);
    color: #fff;
    border-color: var(--el-color-primary);
    font-weight: 500;

    .tags-view-item__close {
      color: rgba(255, 255, 255, 0.7);

      &:hover {
        color: #fff;
        background: rgba(255, 255, 255, 0.2);
      }
    }
  }
}

.tags-view-item__text {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tags-view-item__close {
  margin-left: 4px;
  cursor: pointer;
  border-radius: 50%;
  font-size: 12px;
  padding: 1px;
  transition: all 0.15s;

  &:hover {
    background-color: var(--el-fill-color);
  }
}
</style>
