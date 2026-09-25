<template>
  <!--
    Element Plus 的全局配置（语言 / 尺寸）。
    原来靠 main.ts 的 app.use(ElementPlus, { locale, size }) 提供；P3-A2 去掉
    全量注册后必须由 ConfigProvider 顶上 —— 否则 ElMessageBox 的按钮文案、
    分页「前往 x 页」、表格空态等都会退回英文默认值。
  -->
  <el-config-provider :locale="zhCn" size="default">
    <router-view />
  </el-config-provider>
</template>

<script setup lang="ts">
import zhCn from 'element-plus/es/locale/lang/zh-cn'
</script>
