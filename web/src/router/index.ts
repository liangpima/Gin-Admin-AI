import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import NProgress from 'nprogress'
import 'nprogress/nprogress.css'
import { getToken } from '@/utils/auth'
import { useUserStore } from '@/store/modules/user'
import { usePermissionStore } from '@/store/modules/permission'
import { constantRoutes } from './routes/static'

const router = createRouter({
  history: createWebHistory(),
  routes: constantRoutes as RouteRecordRaw[],
  scrollBehavior: () => ({ top: 0 }),
})

const whiteList = ['/login', '/404']

router.beforeEach(async (to, _from, next) => {
  NProgress.start()
  document.title = (to.meta?.title as string) || 'Gin-Admin'

  const token = getToken()
  if (token) {
    if (to.path === '/login') {
      next('/')
      NProgress.done()
    } else {
      const userStore = useUserStore()
      if (userStore.roles.length === 0) {
        try {
          const userInfo = await userStore.getInfo()
          const permissionStore = usePermissionStore()
          const accessRoutes = permissionStore.generateRoutes(userInfo.menus)
          if (accessRoutes.length === 0) {
            userStore.logout()
            next('/404')
            NProgress.done()
            return
          }
          accessRoutes.forEach((route) => {
            router.addRoute(route)
          })
          // 按**路径**重导航，不要展开 `{ ...to }` 重放：
          // 兜底命中时 `to.name` 是 `NotFoundCatchAll`，而 vue-router 解析
          // location 对象时 name 优先于 path —— 展开重放会按 name 再命中一次
          // 兜底，绕回 404，业务页面永远打不开。
          next({ path: to.path, query: to.query, hash: to.hash, replace: true })
        } catch {
          userStore.logout()
          const permStore = usePermissionStore()
          permStore.resetRoutes()
          next('/login')
          NProgress.done()
        }
      } else {
        next()
      }
    }
  } else {
    if (whiteList.includes(to.path)) {
      next()
    } else {
      next('/login')
      NProgress.done()
    }
  }
})

router.afterEach(() => {
  NProgress.done()
})

export default router
