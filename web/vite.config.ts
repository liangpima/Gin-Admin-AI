import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { resolve } from 'path'

/**
 * 从模块 id 里取出 node_modules 下的包名（正确处理 `@scope/name` 形式）。
 * 返回 undefined 表示这个模块不在 node_modules 里（业务代码）。
 */
function pkgOfNodeModules(id: string): string | undefined {
  const normalized = id.replace(/\\/g, '/')
  const m = /node_modules\/(@[^/]+\/[^/]+|[^/]+)\//.exec(normalized)
  return m?.[1]
}

export default defineConfig({
  plugins: [
    vue(),
    AutoImport({
      resolvers: [ElementPlusResolver()],
      imports: ['vue', 'vue-router', 'pinia'],
      dts: 'src/auto-imports.d.ts',
    }),
    Components({
      resolvers: [ElementPlusResolver()],
      dts: 'src/components.d.ts',
    }),
  ],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/uploads': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    /**
     * ⚠️ 构建输出里**会一直有**一条
     * `(!) Some chunks are larger than 500 kB after minification.`
     * —— 指向 `vendor-wangeditor`（~793 kB）。这是**预期内的**：
     * 它的体积由上游决定，且已通过 `defineAsyncComponent` 异步化，只在协议页加载。
     *
     * 这里**刻意不调 `chunkSizeWarningLimit`**：调了就等于「用一个更大的阈值把问题
     * 藏起来」，而真正的门禁是 `scripts/check-bundle.mjs` —— 它对**任意** chunk 卡
     * 500 kB，只给 vendor-wangeditor 一条**带理由和上限**（≤900 kB）的显式例外。
     * 也就是说：新出现的超标 chunk 会被 `npm run build` **阻断**，
     * 而不是靠这条没人看的警告。
     */
    rollupOptions: {
      output: {
        /**
         * 只把「框架内核」与「工具库」拆成独立 chunk，**刻意不动 element-plus**。
         *
         * 计划文档里原本还想拆一个 vendor-element，但那条是 A2 之前写的：
         * 当时 `app.use(ElementPlus)` 把所有组件塞进主包，拆出来能明显缩小首屏。
         * A2 之后 Vite 已经**按组件自动拆**出 el-table-column / el-select /
         * el-tree / el-form-item … 这些 chunk 里有些只被懒加载路由用到；
         * 再按包名强行合并成一个大 vendor-element，会让首屏把它们一起下载 ——
         * 反而把 A2 的收益吐回去。所以这里对 element-plus 返回 undefined，
         * 交回 Vite 的默认策略。
         *
         * 拆 vendor-vue 的收益是**缓存粒度**而不是体积：vendor-vue 与业务代码
         * 分属不同文件，改业务代码时它的哈希不变，用户不必重下框架。
         * vendor-wangeditor 则顺带给那个 ~800 kB 的库一个**固定名字**，
         * A5 的体积门禁才能按名字给它显式上限（哈希名做不到）。
         */
        manualChunks(id) {
          const pkg = pkgOfNodeModules(id)
          if (!pkg) return
          if (pkg.startsWith('@wangeditor/')) return 'vendor-wangeditor'
          if (pkg === 'vue' || pkg === 'vue-router' || pkg === 'pinia' || pkg.startsWith('@vue/')) {
            return 'vendor-vue'
          }
          if (['axios', 'js-cookie', 'nprogress', 'path-to-regexp'].includes(pkg)) {
            return 'vendor-utils'
          }
          return
        },
      },
    },
  },
})
