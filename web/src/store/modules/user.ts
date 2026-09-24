import { defineStore } from 'pinia'
import { login as loginApi, getUserInfo, logout as logoutApi } from '@/api/auth'
import { setToken, getToken, removeToken, setRefreshToken, getRefreshToken } from '@/utils/auth'
import type { UserInfoResult } from '@/api/auth'
import { usePermissionStore } from './permission'
import router from '@/router'

interface UserState {
  token: string
  refreshToken: string
  userInfo: UserInfoResult | null
  roles: string[]
  buttons: string[]
}

export const useUserStore = defineStore('user', {
  state: (): UserState => ({
    token: getToken() || '',
    refreshToken: getRefreshToken() || '',
    userInfo: null,
    roles: [],
    buttons: [],
  }),

  actions: {
    async login(username: string, password: string, captchaToken: string) {
      const res = await loginApi({ username, password, captchaToken })
      this.token = res.data.accessToken
      this.refreshToken = res.data.refreshToken
      setToken(res.data.accessToken)
      setRefreshToken(res.data.refreshToken)
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
     */
    clearSession() {
      this.token = ''
      this.refreshToken = ''
      this.userInfo = null
      this.roles = []
      this.buttons = []
      removeToken()

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
        // 必须带上 refreshToken，否则服务端无法定位并吊销它，
        // 登出后该凭据仍可换发新的 access token
        await logoutApi(this.refreshToken || getRefreshToken())
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
