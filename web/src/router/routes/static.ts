import type { RouteRecordRaw } from 'vue-router'
import Layout from '@/layout/index.vue'

export const constantRoutes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/login/index.vue'),
    meta: { title: '登录', hidden: true },
  },
  {
    path: '/',
    component: Layout,
    redirect: '/dashboard',
    children: [
      {
        path: 'dashboard',
        name: 'Dashboard',
        component: () => import('@/views/dashboard/index.vue'),
        meta: { title: '首页', icon: 'HomeFilled', affix: true },
      },
    ],
  },
  {
    path: '/404',
    name: 'NotFound',
    component: () => import('@/views/error/404.vue'),
    meta: { title: '404', hidden: true },
  },
  // 兜底路由：**必须放在最后**，且**必须用 component，不能写成 redirect**。
  //
  // Vue Router 4 按路径「具体度」打分匹配，而不是按注册顺序 —— 所以这条
  // catch-all 不会盖住之后通过 addRoute 动态注册的业务路由（`/dashboard`、
  // `/system/user` 这类具体路径的分数都高于通配）。
  //
  // 没有它的时候，访问未匹配的路径只会得到：控制台一条
  // "No match found for location" 警告 + 一片空白页。用户的感受是「页面坏了」，
  // 而不是「地址写错了」—— 这两件事需要给出不同的反馈。
  //
  // 它也顺带覆盖了「有 token 但访问自己没权限的页面」：那条路由根本没被
  // addRoute 注册，于是落到这里显示 404。不需要在全局守卫里再单独校验
  // 「目标 path 是否命中 accessRoutes」—— 未注册的路径本来就渲染不出内容。
  //
  // ⚠️ 不要改回 `redirect: '/404'`。vue-router 的 `pushWithRedirect` 里，
  // `handleRedirectRecord` 跑在 `navigate()`（`beforeEach` 在其中）**之前**，
  // 于是守卫拿到的 `to` 已经是 `/404`，原始路径只剩在 `to.redirectedFrom` 里。
  // 结果是「先 push 到未注册的业务路由 → 守卫里 addRoute → 按 to 重导航」
  // 这条链路永远回不到原路径，**所有业务页面都变 404**。
  // 用 component 时守卫能看见真实路径，兜底与动态路由才能共存。
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFoundCatchAll',
    component: () => import('@/views/error/404.vue'),
    meta: { title: '404', hidden: true },
  },
]
