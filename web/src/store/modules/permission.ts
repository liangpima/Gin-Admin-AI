import type { MenuItem } from '@/api/menu'
import { defineStore } from 'pinia'
import type { RouteRecordRaw } from 'vue-router'
import { constantRoutes } from '@/router/routes/static'
import Layout from '@/layout/index.vue'

const viewModules = import.meta.glob('@/views/**/*.vue')

interface PermissionState {
  routes: RouteRecordRaw[]
  addRoutes: RouteRecordRaw[]
  routesLoaded: boolean
}

export const usePermissionStore = defineStore('permission', {
  state: (): PermissionState => ({
    routes: [],
    addRoutes: [],
    routesLoaded: false,
  }),

  actions: {
    setRoutes(routes: RouteRecordRaw[]) {
      this.addRoutes = routes
      this.routes = constantRoutes.concat(routes)
      this.routesLoaded = true
    },

    generateRoutes(menus: MenuItem[]) {
      const accessedRoutes = filterAsyncRoutes(menus)
      this.setRoutes(accessedRoutes)
      return accessedRoutes
    },

    resetRoutes() {
      this.routes = []
      this.addRoutes = []
      this.routesLoaded = false
    },
  },
})

// 路由节点类型。
//
// RouteRecordRaw 是联合类型，其中「单视图」那一支不含 children，
// 所以直接写 route.children = ... 会报「类型不可赋值」。
// 显式声明成「可带子路由」的形状，既保留 vue-router 的类型约束，
// 又允许递归挂载子路由。
type AppRoute = RouteRecordRaw & { children?: AppRoute[] }

function filterAsyncRoutes(menus: MenuItem[]): AppRoute[] {
  const res: AppRoute[] = []
  menus.forEach((menu) => {
    if (menu.type === 2) return

    // 直接写在一起而不是先存成布尔变量：存成变量后 TS 无法把
    // menu.children 收窄为非空，后面 filterAsyncRoutes(menu.children) 会报错
    const visibleChildren = (menu.children || []).filter((c) => c.type !== 2)
    const componentPath = menu.component
      ? viewModules[`/src/views/${menu.component}.vue`]
      : (visibleChildren.length > 0 ? Layout : undefined)

    if (!componentPath) return

    // children 直接写进字面量：RouteRecordRaw 的联合分支里
    // 「单视图」不含 children，事后赋值会报类型不可赋值
    const route: AppRoute = {
      path: menu.path || '',
      name: menu.name,
      component: componentPath,
      meta: {
        title: menu.title || menu.name,
        icon: menu.icon,
        hidden: menu.visible === 0,
        noCache: menu.isCache === 0,
      },
      ...(visibleChildren.length > 0 && menu.children
        ? { children: filterAsyncRoutes(menu.children) }
        : {}),
    }

    res.push(route)
  })
  return res
}
