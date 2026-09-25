import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'

import App from './App.vue'
import router from './router'
import { createPinia } from 'pinia'
import { getSiteInfo } from './api/config'
import { registerAppIcons } from './utils/icons'

import './assets/styles/tokens/light.scss'
import './assets/styles/tokens/dark.scss'
import './assets/styles/tokens/_index.scss'
import './assets/styles/reset.scss'
import './assets/styles/element-override.scss'
import './assets/styles/index.scss'

const app = createApp(App)

app.use(createPinia())
app.use(router)
app.use(ElementPlus, { locale: zhCn, size: 'default' })

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
