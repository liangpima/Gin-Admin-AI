import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

/**
 * 单元测试专用配置。
 *
 * 为什么不复用 vite.config.ts：
 *   - 那份配置里的 AutoImport / Components 是为**运行时**服务的（自动引入
 *     Element Plus 组件与 API），测试里不需要，反而会让「测试为什么失败」
 *     多一层不确定性
 *   - 单独一份还能避免测试与构建的插件版本互相牵制
 *
 * 只保留两样必需的东西：解析 .vue 的插件、以及 `@` 别名。
 */
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  test: {
    // 默认用 node 环境：绝大多数用例是纯逻辑（字典加载、响应拦截器、路由生成），
    // 不需要 DOM，跑起来也更快。
    //
    // 需要 DOM 的用例（cookie 读写、路由守卫里改 document.title）在**文件首行**
    // 加 `// @vitest-environment jsdom` 单独覆盖 —— 这样 jsdom 的启动开销
    // 只落在真正需要它的文件上，而不是全局（P3-B2 引入 jsdom 时就是这么做的）。
    environment: 'node',
    include: ['src/**/*.spec.ts'],
    // 测试文件与源码同目录：改哪个模块就顺手能看到它的用例
    reporters: ['default'],
  },
})
