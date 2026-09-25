// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest'
import Cookies from 'js-cookie'
import { clearLoginFlag, csrfHeaders, CSRFHeaderName, getCsrfToken, isLoggedIn } from '@/utils/auth'

/**
 * 登录态标记的单元测试（P3-B2）。
 *
 * 这组用例守的是 B2 最容易写错的地方：**「本地有没有凭据」这个判断依据换人了**。
 * 改造前是 `getToken()`（读一个 JS 可读的 cookie）；改造后 token 变成 HttpOnly、
 * JS 读不到，前端只能依赖服务端额外下发的**非敏感**标记 `logged_in`。
 *
 * 三条断言各自对应一种真实退化：
 *   1. 用 truthy 判断 → `logged_in=0` / `logged_in=false` 也会被判成「已登录」，
 *      守卫于是放行到业务页面，用户在「看起来正常」的界面上看到一堆 401
 *   2. 清除时不带 `path: '/'` → js-cookie 默认按**当前页面路径**删，
 *      在 /system/user 这类子路径下删不掉一个 Path=/ 的 cookie，
 *      而且**不会有任何报错**；表现是「登出后刷新又回到登录态」
 *   3. 模块里又冒出读取 token 的接口 → token 回到 JS 可达范围，
 *      B 组改造的收益（XSS 拿不到凭据）就白做了
 *
 * 用 jsdom 环境：标记就是 `document.cookie`，只有在真实 cookie jar 上测
 * 才能覆盖 Path / 值格式这些细节 —— 用 mock 掉的 Cookies 测等于什么都没测。
 */

const KEY = 'logged_in'

beforeEach(() => {
  // 同一个测试文件内 jsdom 的 cookie jar 是共享的，不清理会让用例互相污染，
  // 且结果随执行顺序变化 —— 这类用例比不写还危险。
  // 用 Cookies.remove 而不是手写 document.cookie：后者在路径不匹配时静默失效。
  Cookies.remove(KEY, { path: '/' })
  vi.restoreAllMocks()
})

describe('isLoggedIn：只认严格相等的 1', () => {
  it('没有标记时是未登录', () => {
    expect(isLoggedIn()).toBe(false)
  })

  it('标记为 1 时是已登录', () => {
    Cookies.set(KEY, '1', { path: '/' })
    expect(isLoggedIn()).toBe(true)
  })

  it('标记为 0 / false 时**不是**已登录（不能用 truthy 判断）', () => {
    // 若实现写成 `Boolean(Cookies.get(KEY))`，下面两个都会判成已登录 ——
    // 而它们的含义恰恰是「没有会话」，守卫会放行到业务页
    for (const v of ['0', 'false', 'no']) {
      Cookies.set(KEY, v, { path: '/' })
      expect(isLoggedIn(), `logged_in=${v} 不应判为已登录`).toBe(false)
    }
  })

  it('值里有空格等杂字符时不匹配（严格相等，不做 trim）', () => {
    // 服务端写的是字面量 "1"；若前端做了 trim/宽松比较，
    // 「值被别的代码改脏」这种情况就会被掩盖过去
    Cookies.set(KEY, ' 1', { path: '/' })
    expect(isLoggedIn()).toBe(false)
  })
})

describe('clearLoginFlag', () => {
  it('清除后立即变为未登录', () => {
    Cookies.set(KEY, '1', { path: '/' })
    expect(isLoggedIn()).toBe(true)

    clearLoginFlag()

    expect(isLoggedIn()).toBe(false)
  })

  it('必须带 path=/ —— 否则在子路径页面下删不掉', () => {
    // js-cookie 的 remove 默认用**当前页面路径**。用户在 /system/user 上点登出时，
    // 不带 path 的 remove 删的是 `/system/user` 作用域下的同名 cookie，
    // 而服务端下发的是 Path=/ 那个 —— 结果「登出成功，刷新后仍是登录态」。
    // 这里用 spy 断言参数，因为 jsdom 里改不了 window.location.pathname
    // （改了也测不到 js-cookie 内部的默认值来源）。
    const remove = vi.spyOn(Cookies, 'remove')

    clearLoginFlag()

    expect(remove).toHaveBeenCalledWith(KEY, { path: '/' })
  })
})

describe('模块边界：不再有任何 token 存取接口', () => {
  it('不导出 getToken/setToken/getRefreshToken/setRefreshToken', async () => {
    // 这条是**防回退**的：B2 删掉这些接口之后，若有人为了「兼容旧代码」
    // 把它们加回来，token 就又回到 JS 可达范围，B1 的 HttpOnly 收益归零。
    // 而功能上完全看不出来 —— 登录照常能用。
    const mod = await import('@/utils/auth')

    for (const name of ['getToken', 'setToken', 'getRefreshToken', 'setRefreshToken']) {
      expect(mod, `不应再导出 ${name}`).not.toHaveProperty(name)
    }
  })
})

/**
 * CSRF 令牌的读取（P3-B3）。
 *
 * 这组用例守的是 double-submit 的前端一半。它与上面那组是**镜像关系**：
 * `logged_in` 不能当鉴权依据，而 `csrf_token` 恰恰必须能被 JS 读到 ——
 * 两者都是「非敏感 cookie」，但一个只用来决定要不要拉 userInfo，
 * 另一个要放进请求头。把这两个 cookie 混用（比如用 logged_in 当令牌，
 * 或给 csrf_token 加上 HttpOnly）都会让功能在「看起来正常」的状态下悄悄坏掉。
 */
describe('CSRF 令牌：读 cookie、拼请求头', () => {
  const CSRF = 'csrf_token'

  beforeEach(() => {
    Cookies.remove(CSRF, { path: '/' })
  })

  it('没有令牌时返回 undefined（未登录时不该带头）', () => {
    // 登录接口本身就是「还没有令牌」的状态。若这里返回空串，
    // 调用方会写出 `X-CSRF-Token: `（空值头），而服务端校验的是
    // 「头非空 且 与 cookie 相等」—— 空头应当由前端就不发，而不是靠服务端兜底。
    expect(getCsrfToken()).toBeUndefined()
  })

  it('有令牌时原样读出（不做 trim / 不改写）', () => {
    Cookies.set(CSRF, 'abc123', { path: '/' })
    expect(getCsrfToken()).toBe('abc123')
  })

  it('csrfHeaders() 无令牌时返回空对象', () => {
    expect(csrfHeaders()).toEqual({})
  })

  it('csrfHeaders() 有令牌时返回正确的头名与值', () => {
    Cookies.set(CSRF, 'abc123', { path: '/' })
    expect(csrfHeaders()).toEqual({ 'X-CSRF-Token': 'abc123' })
  })

  it('头名必须是 X-CSRF-Token —— 与服务端 authcookie.CSRFHeaderName 逐字一致', () => {
    // 拼错（如 X-CSRF-Token 写成 X-Csrf-Token 之外的其它名字）不会有任何报错，
    // 现象是**所有非 GET 请求 403**，而 GET 一切正常 —— 很容易被当成权限问题。
    expect(CSRFHeaderName).toBe('X-CSRF-Token')
  })

  it('读的是 csrf_token 而不是 logged_in —— 两个 cookie 不能混用', () => {
    // 用 logged_in 当令牌的话，服务端比对的是 csrf_token cookie，
    // 永远对不上 → 所有写请求 403
    Cookies.set('logged_in', '1', { path: '/' })
    expect(getCsrfToken()).toBeUndefined()
    expect(csrfHeaders()).toEqual({})
  })
})
