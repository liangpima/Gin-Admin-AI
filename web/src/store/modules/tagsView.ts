import { defineStore } from 'pinia'
import type {
  LocationQueryRaw,
  RouteLocationNormalized,
  RouteMeta,
  RouteParamsRaw,
} from 'vue-router'

export interface TagView {
  path: string
  name?: string | symbol | null | undefined
  title?: string
  // 直接用 vue-router 的类型，别用 any 把路由元信息与查询参数抹平
  meta?: RouteMeta
  query?: LocationQueryRaw
  params?: RouteParamsRaw
}

interface TagsViewState {
  visitedViews: TagView[]
  cachedViews: string[]
}

export const useTagsViewStore = defineStore('tagsView', {
  state: (): TagsViewState => ({
    visitedViews: [],
    cachedViews: [],
  }),

  actions: {
    addView(view: RouteLocationNormalized) {
      this.addVisitedView(view)
      this.addCachedView(view)
    },

    addVisitedView(view: RouteLocationNormalized) {
      if (this.visitedViews.some((v) => v.path === view.path)) return
      this.visitedViews.push({
        path: view.path,
        name: view.name,
        title: view.meta?.title as string,
        meta: view.meta,
        query: view.query,
        params: view.params,
      })
    },

    addCachedView(view: RouteLocationNormalized) {
      const name = view.name as string
      if (!name) return
      if (this.cachedViews.includes(name)) return
      if (view.meta?.noCache) return
      this.cachedViews.push(name)
    },

    delView(view: TagView) {
      this.delVisitedView(view)
      this.delCachedView(view)
    },

    delVisitedView(view: TagView) {
      const index = this.visitedViews.findIndex((v) => v.path === view.path)
      if (index > -1) this.visitedViews.splice(index, 1)
    },

    delCachedView(view: TagView) {
      const name = view.name as string
      const index = this.cachedViews.indexOf(name)
      if (index > -1) this.cachedViews.splice(index, 1)
    },

    delAllViews() {
      this.visitedViews = []
      this.cachedViews = []
    },

    delOtherViews(view: TagView) {
      this.visitedViews = this.visitedViews.filter((v) => v.path === view.path)
      const name = view.name as string
      this.cachedViews = name ? [name] : []
    },
  },
})
