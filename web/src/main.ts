import { createApp } from 'vue'

// Element Plus 样式改为按需引入（P3-A2）。
//
// 模板里的 <el-xxx> 由 unplugin-vue-components 自动带上对应样式，但
// **显式 import 的 API 绕过了解析器**，样式必须自己引 —— 否则 ElMessage 弹出来
// 没有样式，而且不报错，只是难看。全量 index.css 有 373 kB，其中绝大多数组件
// 本项目根本没用（见本地规划文档 docs/plan-p3-optional.md 的基线；
// 该文档按 .gitignore 不入库，只作过程记录）。
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/message-box/style/css'

import App from './App.vue'
import router from './router'
import { createPinia } from 'pinia'
import { getSiteInfo } from './api/config'
import { registerAppIcons } from './utils/icons'

// 样式入口只剩两份：
//   · reset.scss —— 基础重置（a 的颜色除外，那条被 index.scss 覆盖，见该文件注释）
//   · index.scss —— 全局变量与工具类（含 @include mobile 的移动端基线）
//
// ⚠️ 这里曾 import 过 tokens/{light,dark,_index}.scss 与 element-override.scss，
// 但**四个文件都只剩注释**（内容早已被删空，只留下「using Element Plus defaults」
// 这类说明），另有一个无人引用的 variables.scss。留着它们会误导后人以为
// 项目存在一套 design token / 暗色主题体系，从而去「修」一个不存在的东西。
// 已连同文件一并删除（2026-09-28）。要重新引入 token 体系时请新建文件，
// 别复活这几个空壳。
import './assets/styles/reset.scss'
import './assets/styles/index.scss'

const app = createApp(App)

app.use(createPinia())
app.use(router)

// 不再 app.use(ElementPlus)：它会全量注册组件、架空按需解析。
// 它原先顺带设置的 locale / size 改由 App.vue 的 <el-config-provider> 提供
// （Element Plus 的 provideGlobalConfig 在没有别的提供者时会写入全局配置，
//  所以 ElMessageBox 这类命令式 API 也能读到 locale，不会退回英文 OK/Cancel）。
// 只注册白名单里的图标，不是全量 293 个。
// 为什么要白名单、以及哪些站点靠全局注册才能渲染，见 utils/icons.ts 的注释。
registerAppIcons(app)

app.mount('#app')

getSiteInfo()
  .then((res) => {
    const logo = res.data?.['site.logo']
    if (logo) {
      const link =
        (document.querySelector("link[rel~='icon']") as HTMLLinkElement) ||
        document.createElement('link')
      link.rel = 'shortcut icon'
      link.type = 'image/x-icon'
      link.href = logo
      if (!link.parentNode) {
        document.head.appendChild(link)
      }
    }
  })
  .catch(() => {})
