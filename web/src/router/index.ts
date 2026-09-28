import { createRouter, createWebHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import NProgress from 'nprogress'
import 'nprogress/nprogress.css'
import { isLoggedIn } from '@/utils/auth'
import { useUserStore } from '@/store/modules/user'
import { usePermissionStore } from '@/store/modules/permission'
import { useTagsViewStore } from '@/store/modules/tagsView'
import { constantRoutes } from './routes/static'

const router = createRouter({
  history: createWebHistory(),
  routes: constantRoutes as RouteRecordRaw[],
  scrollBehavior: () => ({ top: 0 }),
})

const whiteList = ['/login', '/404']

/**
 * 不进标签栏的路径 / 路由名。
 *
 * 登录页与 404 都不是「工作页面」：出现在标签栏里既无意义，
 * 关闭它还会把用户带到一个空状态。兜底路由（NotFoundCatchAll）同理 ——
 * 它的 `path` 是用户敲错的原始地址，进标签栏只会让人困惑。
 */
const noTagPaths = ['/login', '/404']
const noTagRouteNames = ['NotFoundCatchAll']

router.beforeEach(async (to, _from, next) => {
  NProgress.start()
  document.title = (to.meta?.title as string) || 'Gin-Admin'

  // 判定依据是服务端下发的**非敏感**登录态标记，不是本地 token ——
  // token 现在是 HttpOnly，JS 根本读不到（P3-B2）。
  // 这个标记只回答「要不要去拉用户信息」：它可能已过期或已在服务端被吊销，
  // 所以下面的 getInfo 失败分支（catch）才是真正的兜底。
  const loggedIn = isLoggedIn()
  if (loggedIn) {
    if (to.path === '/login') {
      next('/')
      NProgress.done()
    } else {
      const userStore = useUserStore()
      const permissionStore = usePermissionStore()
      // 「动态路由是否已注册」的判定依据是 permissionStore.routesLoaded，
      // **不能**用 `userStore.roles.length === 0`：
      //   · 一个合法但没有任何角色的账号，roles 恒为空数组 —— 于是每次导航
      //     都会重新 getInfo() + generateRoutes() + addRoute()，菜单被重复注册，
      //     且每次导航都多一次用户信息请求；
      //   · routesLoaded 有明确的置位/复位时机（generateRoutes 置 true、
      //     resetRoutes / clearSession 置 false），语义正是这里要问的问题。
      // Sidebar.vue 早就用的是 routesLoaded，两处判定原本不一致。
      if (!permissionStore.routesLoaded) {
        try {
          const userInfo = await userStore.getInfo()
          const accessRoutes = permissionStore.generateRoutes(userInfo.menus)
          if (accessRoutes.length === 0) {
            // 与下面 catch 分支同一个问题：必须**同步**清会话。
            // 不同步清的话，`next('/404')` 执行时 `logged_in` 仍在，
            // 守卫对 /404 重入 → roles 仍为空 → 再拉一次 userInfo →
            // accessRoutes 仍为 0 → 再 next('/404')，同样形成死循环。
            userStore.clearSession()
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
          // 必须先**同步**清会话再跳转，不能调异步的 `logout()`。
          //
          // `logout()` 是 `await logoutApi()` 之后才 `clearSession()` ——
          // 也就是说清 `logged_in` 标记的那一步要等一次网络往返。
          // 而下面的 `next('/login')` 是同步执行的：它跑的时候标记仍在，
          // 守卫重入后走「loggedIn && to.path === '/login' → next('/')」，
          // 回到业务路径 → 再次 getInfo() 失败 → **重定向死循环**，
          // 每轮还都发一个注定失败的请求，直到那个 fire-and-forget 的
          // logout settle（网络不可用时最坏 30s 超时）。
          //
          // 触发条件是「getInfo 因**非 401** 原因失败」：后端 500、网关 502、
          // 超时都会走到这里。401 路径之所以看着没事，是因为响应拦截器里的
          // handleLogout 会**同步**清标记 —— 那是另一条路径，不能用来推断
          // 这一条也安全。
          //
          // clearSession() 是同步的，且已经包含 resetRoutes + removeRoute，
          // 所以不必再单独调 permissionStore.resetRoutes()。
          userStore.clearSession()
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

router.afterEach((to) => {
  NProgress.done()

  // 标签栏（TagsView）与 keep-alive 的唯一数据源。
  //
  // 放在 afterEach 而不是 beforeEach：只有**真正完成**的导航才该进标签栏。
  // 被守卫重定向或中止的导航（未登录跳 /login、无可用菜单跳 /404）不该留下标签 ——
  // 那会让标签栏出现用户从未真正打开过的页面。
  //
  // ⚠️ 此前这个调用点**根本不存在**（全仓 grep `addView` 只命中定义处），
  // 后果是连锁的两件事：
  //   ① `visitedViews` 恒为 `[]` → 标签栏渲染 0 项，整个组件形同摆设；
  //   ② `<keep-alive :include="cachedViews">` 收到空数组 → **任何页面都不缓存**，
  //      每次切路由都重新挂载，列表页的筛选条件、分页、滚动位置全丢并重复请求。
  // 两者都没有任何报错，所以能一直不被发现。
  if (noTagPaths.includes(to.path)) return
  if (to.name && noTagRouteNames.includes(to.name as string)) return
  useTagsViewStore().addView(to)
})

export default router
