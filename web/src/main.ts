import { createApp } from 'vue'
import ElementPlus from 'element-plus'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import 'element-plus/dist/index.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'

import App from './App.vue'
import router from './router'
import { createPinia } from 'pinia'
import { getSiteInfo } from './api/config'

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

for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component)
}

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
