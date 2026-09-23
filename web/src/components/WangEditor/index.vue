<template>
  <div class="wangeditor-wrapper">
    <Toolbar
      :editor="editorRef"
      :defaultConfig="toolbarConfig"
      :mode="mode"
      class="wangeditor-toolbar"
    />
    <Editor
      v-model="valueHtml"
      :defaultConfig="editorConfig"
      :mode="mode"
      class="wangeditor-editor"
      @onCreated="handleCreated"
    />
    <ImagePicker v-model:visible="imagePickerVisible" @confirm="handleImagePick" />
    <ImagePicker v-model:visible="videoPickerVisible" type="video" @confirm="handleVideoPick" />
  </div>
</template>

<script setup lang="ts">
import '@wangeditor/editor/dist/css/style.css'
import { ref, shallowRef, watch, onBeforeUnmount } from 'vue'
import { Editor, Toolbar } from '@wangeditor/editor-for-vue'
import type { IDomEditor, IToolbarConfig } from '@wangeditor/editor'
import ImagePicker from '@/components/ImagePicker/index.vue'

const props = withDefaults(defineProps<{
  modelValue?: string
  mode?: 'default' | 'simple'
  height?: number
}>(), {
  modelValue: '',
  mode: 'default',
  height: 350,
})

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

const editorRef = shallowRef<IDomEditor>()
const valueHtml = ref(props.modelValue)
const imagePickerVisible = ref(false)
const videoPickerVisible = ref(false)

const toolbarConfig: Partial<IToolbarConfig> = {
  excludeKeys: ['fullScreen'],
}

const insertFnRef = ref<((url: string, alt?: string, href?: string) => void) | null>(null)

const editorConfig = {
  placeholder: '请输入内容...',
  MENU_CONF: {
    uploadImage: {
      customBrowseAndUpload(insertFn: (url: string, alt?: string, href?: string) => void) {
        insertFnRef.value = insertFn
        imagePickerVisible.value = true
      },
    },
    uploadVideo: {
      customBrowseAndUpload(insertFn: (url: string, poster?: string) => void) {
        insertFnRef.value = insertFn as (url: string, alt?: string, href?: string) => void
        videoPickerVisible.value = true
      },
    },
  },
}

function handleImagePick(url: string | string[]) {
  const urls = Array.isArray(url) ? url : [url]
  urls.forEach((u) => {
    insertFnRef.value?.(u, '', u)
  })
  insertFnRef.value = null
}

function handleVideoPick(url: string | string[]) {
  const u = Array.isArray(url) ? url[0] : url
  insertFnRef.value?.(u)
  insertFnRef.value = null
}

watch(
  () => props.modelValue,
  (val) => {
    if (val !== valueHtml.value) {
      valueHtml.value = val || ''
    }
  }
)

watch(valueHtml, (val) => {
  emit('update:modelValue', val)
})

function handleCreated(editor: IDomEditor) {
  editorRef.value = editor
}

onBeforeUnmount(() => {
  const editor = editorRef.value
  if (editor) {
    editor.destroy()
  }
})
</script>

<style lang="scss" scoped>
.wangeditor-wrapper {
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  overflow: visible;
  position: relative;
}

.wangeditor-toolbar {
  border-bottom: 1px solid var(--el-border-color);
  position: relative;
  z-index: 10;
}

.wangeditor-editor {
  min-height: 350px;
  max-height: 450px;
  overflow-y: auto;
  position: relative;
  z-index: 1;

  :deep(video) {
    max-width: 100%;
    max-height: 300px;
  }

  :deep(.w-e-text-container) {
    overflow: hidden;
  }

  :deep(iframe) {
    max-width: 100%;
  }
}
</style>
