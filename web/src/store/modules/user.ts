import { defineStore } from 'pinia'
import { login as loginApi, getUserInfo, logout as logoutApi } from '@/api/auth'
import { clearLoginFlag } from '@/utils/auth'
import type { UserInfoResult } from '@/api/auth'
import { usePermissionStore } from './permission'
import router from '@/router'

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

    async getInfo() {
      const res = await getUserInfo()
      this.userInfo = res.data
      this.roles = res.data.roles.map((r) => r.code)
      this.buttons = res.data.buttons
      return res.data
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
      permissionStore.resetRoutes()
      router.getRoutes().forEach((route) => {
        if (route.name && !['Login', 'Dashboard', 'NotFound'].includes(route.name as string)) {
          router.removeRoute(route.name)
        }
      })
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
