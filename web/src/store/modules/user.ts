import { defineStore } from 'pinia'
import { login as loginApi, getUserInfo, logout as logoutApi } from '@/api/auth'
import { clearLoginFlag } from '@/utils/auth'
import type { UserInfoResult } from '@/api/auth'
import type { RouteRecordRaw } from 'vue-router'
import { constantRoutes } from '@/router/routes/static'
import { usePermissionStore } from './permission'
import { useTagsViewStore } from './tagsView'
import router from '@/router'

/**
 * 收集路由树里所有具名路由的 name（**含子路由**）。
 *
 * 必须递归：`constantRoutes` 里 `/` 的子路由 `dashboard` 同样是常量路由，
 * 只取顶层会把它误判成动态路由删掉 —— 而它正是登录后的落地页。
 */
function collectRouteNames(routes: RouteRecordRaw[]): Set<string> {
  const names = new Set<string>()
  const walk = (list: RouteRecordRaw[]) => {
    list.forEach((route) => {
      if (route.name) {
        names.add(route.name as string)
      }
      // RouteRecordRaw 是联合类型，「单视图」那一支不含 children，
      // 直接写 route.children 会报类型错误
      const children = (route as { children?: RouteRecordRaw[] }).children
      if (children && children.length > 0) {
        walk(children)
      }
    })
  }
  walk(routes)
  return names
}

interface UserState {
  userInfo: UserInfoResult | null
  roles: string[]
  buttons: string[]
}

export const useUserStore = defineStore('user', {
  // state 里**没有** token（P3-B2）：token 由服务端通过 HttpOnly cookie 下发，
  // 浏览器自动携带，前端既读不到也不需要读。留在 state 里只会诱使后人拿它做判断。
  state: (): UserState => ({
    userInfo: null,
    roles: [],
    buttons: [],
  }),

  actions: {
    /**
     * 登录。
     *
     * 不再保存 token：服务端在同一个响应里下发了 HttpOnly cookie，
     * 后续请求由浏览器自动携带。响应体里仍返回 token，那是留给
     * Swagger / curl 这类不会自动带 cookie 的调用方的。
     *
     * 「登录成功」的判定就是这里没抛错 —— 不要去读 `logged_in` 标记：
     * 它与响应体同属一个响应，读它既多余，又会在 Set-Cookie 被浏览器
     * 拒绝（例如 Secure 配错）时给出一个「看着成功其实没登录」的结果。
     */
    async login(username: string, password: string, captchaToken: string) {
      await loginApi({ username, password, captchaToken })
    },

    /**
     * 拉取当前用户信息并写入 store。
     *
     * 三个数组字段都做了空值兜底。后端 `vo.UserInfoResponse` 是用
     * `make([]T, 0)` 构造的（见 `auth_service.go` 的 `GetUserInfo`），
     * 正常情况下不会是 null —— 所以这里不是在掩盖一个已知缺陷，
     * 而是防**契约漂移**：网关裁剪字段、后端换实现、或新增字段未同步，
     * 都会让下面的 `roles.map` 与守卫里的 `menus` 消费点抛 TypeError。
     *
     * 那个 TypeError 会被守卫的 `catch` 吞掉 → 现象是「刚登录就被踢回登录页」，
     * 排查时几乎不可能联想到「某个数组字段没了」。兜底成空数组后退化成
     * 「无权限」这一**安全方向**：守卫发现 accessRoutes 为空会明确跳到 /404，
     * 至少是个可诊断的结果。
     */
    async getInfo() {
      const res = await getUserInfo()
      const info = res.data
      const roles = info?.roles ?? []
      const buttons = info?.buttons ?? []
      const menus = info?.menus ?? []

      this.userInfo = info
      this.roles = roles.map((r) => r.code)
      this.buttons = buttons

      // 必须返回**归一化后**的对象：守卫直接取返回值里的 menus 去
      // generateRoutes，返回原始 info 会让上面的兜底形同虚设。
      return { ...info, roles, buttons, menus }
    },

    /**
     * 清理本地会话（不发任何请求）。
     *
     * 抽出来是因为「主动登出」与「被 401 踢出」必须做完全相同的事。
     * 早前 api/index.ts 里的 401 处理只做了 permissionStore.$reset()，
     * 没有 router.removeRoute —— 于是上一个高权限会话经 addRoute 注册的路由
     * 仍留在 router 实例里。换个低权限账号登录时，路由守卫看到 roles 非空
     * 就直接放行，直接改 URL 就能打开那些页面（后端接口会 403，
     * 但页面已可见并且会发出请求）。
     *
     * 这里清的是**登录态标记**而不是 token：真正的凭据是 HttpOnly cookie，
     * 前端删不掉它（这正是设计目标），也不需要删 ——
     * 被 401 踢出时那份 access token 已经无效了；主动登出则由服务端负责清除。
     */
    clearSession() {
      this.userInfo = null
      this.roles = []
      this.buttons = []
      clearLoginFlag()

      const permissionStore = usePermissionStore()

      // 只摘掉**动态注册**的路由，判断依据是「是否属于 constantRoutes」，
      // 而不是硬编码一串路由名。
      //
      // 此前写的是 `!['Login','Dashboard','NotFound'].includes(name)` ——
      // 但兜底路由的 name 实际是 `NotFoundCatchAll`（见 routes/static.ts），
      // 白名单里那个 'NotFound' 是 /404 页面路由的名字，两者对不上。
      // 于是每次登出 / 被 401 踢出都会顺手把兜底路由删掉；它属于
      // constantRoutes，不会再被 addRoute 加回来 →
      // **重新登录后访问任何未匹配路径都是空白页**，只有整页刷新才能恢复。
      //
      // 从 constantRoutes 反推白名单可以从根上杜绝这类漂移：
      // 以后改路由名、新增常量路由都不需要同步维护这里。
      const constantNames = collectRouteNames(constantRoutes)
      router.getRoutes().forEach((route) => {
        if (route.name && !constantNames.has(route.name as string)) {
          router.removeRoute(route.name)
        }
      })

      // 标签栏（TagsView）也是「上一个会话」的状态，必须一起清。
      //
      // 不清的后果有两种，都不报错：
      //   ① 标签指向的正是上面刚被摘掉的动态路由 → 点进去命中兜底路由显示 404，
      //      而标签还挂在那里，用户会以为「页面坏了」；
      //   ② 标签指向的路由恰好新账号也有 → 页面能开，但 `cachedViews` 里
      //      残留的是上一会话的**组件实例**，`<keep-alive :include>` 会直接
      //      复用它 —— 列表页的筛选条件、分页、已加载数据会串到新账号身上，
      //      看起来像「数据错乱」而不是「缓存没清」。
      //
      // 放在 clearSession 而不是 logout：api/index.ts 的 401 处理直接调
      // clearSession，绕过 logout —— 只清 logout 会让被踢出这条路径漏清。
      // 清空不会导致「首页标签丢失」：登录后落到 /dashboard 时，
      // router.afterEach 的 addView 会把它重新加回来。
      useTagsViewStore().delAllViews()

      permissionStore.resetRoutes()
    },

    async logout() {
      try {
        // 不再传 refresh token：它现在由 HttpOnly cookie 携带
        // （Path=/api/v1/auth，覆盖 /auth/logout），服务端自己取。
        // 前端读不到它，也就传不了 —— 传个空串反而会让服务端以为
        // 「这次登出无需吊销」，把它留在 Redis 里直到自然过期。
        await logoutApi()
      } catch (err) {
        // 登出必须「尽力而为」：服务端吊销失败（网络异常、refreshToken 已过期）
        // 也不能把用户卡在登录态，本地会话照清不误，只留一条排查痕迹
        console.warn('[user] 服务端登出失败，已仅清理本地会话', err)
      }
      this.clearSession()
    },

    hasButton(code: string): boolean {
      return this.buttons.includes(code)
    },

    hasRole(code: string): boolean {
      return this.roles.includes(code)
    },
  },
})
