import Cookies from 'js-cookie'

/**
 * 登录态标记的读写（P3-B2）。
 *
 * ## 为什么这里不再有 token
 *
 * token 自 P3-B1 起由服务端通过 **HttpOnly** cookie 下发，JS 读不到它 ——
 * 这正是本轮改造的目的（XSS 即使能执行脚本也拿不到凭据）。
 * 前端因此无法再靠「本地有没有 token」判断登录态，只能依赖服务端额外下发的
 * 一个**非敏感**标记 `logged_in`（见 internal/authcookie 的 LoginFlagCookieName）。
 *
 * 这也是为什么 `getToken` / `setToken` / `getRefreshToken` / `setRefreshToken`
 * 被**删除**而不是保留：只要它们还在，就总会有人拿它当鉴权依据，
 * 而那时读到的要么是 undefined（cookie 是 HttpOnly），要么是伪造值。
 * auth.spec.ts 里有一条用例专门守着「这些接口不得再出现」。
 *
 * ## 标记的语义边界（必须理解，否则会写出越权的判断）
 *
 * `logged_in=1` 只说明「浏览器手里可能有一份会话」，**不代表会话有效**：
 * cookie 可能已过期、token 可能已在服务端被吊销。所以它只用来回答
 * 「要不要去拉一次用户信息」，永远不能当成鉴权依据 —— 真正的鉴权在后端，
 * 前端所有基于它的分支都只是「少发一次注定 401 的请求」。
 * 即使被伪造，代价也只是多发一次 /auth/userInfo（随后 401 回来清会话）。
 *
 * ## 关于「undefined 与空串」这个坑
 *
 * 计划里提醒过：守卫要区分 `undefined`（还没探测）与 `''`（确认未登录），
 * 因为 `getToken()` 把两者揉成了同一个 falsy 值。
 * 本方案用**同步**读 cookie 的方式把这个坑整个绕开了 —— 不存在「探测中」
 * 这个中间态，所以只需要两态。（若改用「不存标记、守卫异步探测
 * /auth/userInfo」的方案，就必须把两者分开，否则冷启动会先闪一下登录页。）
 */

/** 与服务端 authcookie.LoginFlagCookieName 保持一致；改一处必须同时改另一处 */
const LoginFlagKey = 'logged_in'

/** 与服务端 authcookie.LoginFlagCookieValue 保持一致 */
const LoginFlagValue = '1'

/**
 * 是否带有登录态标记。
 *
 * 用**严格相等**而不是 truthy：`logged_in=0` / `logged_in=false` 的含义是
 * 「没有会话」，用 `Boolean(Cookies.get(...))` 会把它们判成已登录，
 * 守卫于是放行到业务页面，用户在一个看起来正常的界面上看到一堆 401。
 */
export function isLoggedIn(): boolean {
  return Cookies.get(LoginFlagKey) === LoginFlagValue
}

/**
 * 清除登录态标记。
 *
 * 什么时候需要前端自己清：**被 401 踢出**时 —— 那条路径没有服务端响应可以
 * 下发清除指令。不清的话，刷新页面会被标记骗回「已登录」分支，
 * 白拉一次 userInfo 再被踢一次，中间还会闪一下空白布局。
 * 主动登出走 /auth/logout，服务端会清掉包括本标记在内的全部 cookie，
 * 这里顺手清是为了让跳转前的状态立刻一致（不必等响应回来）。
 *
 * `path` 必须与下发时一致（`/`）。js-cookie 的 `remove` 默认用**当前页面路径**，
 * 而当前页面可能是 `/system/user` —— 用它去删一个 `Path=/` 的 cookie 什么也删不掉，
 * 并且**不会有任何报错**，现象是「登出后刷新又回到登录态」。
 */
export function clearLoginFlag(): void {
  Cookies.remove(LoginFlagKey, { path: '/' })
}

/**
 * CSRF 令牌的读取（P3-B3）。
 *
 * 与服务端的双提交校验配对：服务端在登录时下发一个**非 HttpOnly** 的随机
 * `csrf_token`，前端每次非 GET 请求把它放进 `X-CSRF-Token` 头，
 * 服务端只校验「头里的值 == cookie 里的值」（见 internal/middleware/csrf.go）。
 *
 * ## 为什么这个 cookie 可以（而且必须）被 JS 读到
 *
 * 它与 `logged_in` 是同一个道理：**不含凭据**。攻击者即使拿到这个值也没有用 ——
 * 它只是一个「证明请求确实由本域脚本发出」的信物。反过来，如果给它加上 HttpOnly，
 * 前端就读不到、也就放不进请求头，整个机制直接失效（且现象是「所有写操作 403」）。
 *
 * 真正的凭据（access / refresh token）依然是 HttpOnly，本文件**不提供**
 * 任何读取它们的接口 —— 这条边界由 auth.spec.ts 里的用例守着。
 *
 * ## 为什么是「每次请求现读」而不是登录后存到内存里
 *
 * 服务端会在续期时重新下发这个 cookie（值沿用不变）。现读 cookie 永远拿到
 * 服务端当前认可的那个值；存到内存则需要额外的同步逻辑，而一旦漏同步，
 * 现象是「每隔一段时间随机有几个写请求失败」—— 很难复现。
 */

/** 与服务端 authcookie.CSRFCookieName 保持一致；改一处必须同时改另一处 */
const CSRFCookieKey = 'csrf_token'

/**
 * 与服务端 authcookie.CSRFHeaderName 保持一致。
 *
 * 这个值还必须在 `cors.allow_headers` 里出现（config/config.yaml 与
 * deploy/config.docker.yaml 两份模板都要）—— 自定义头会触发 CORS 预检，
 * 漏配只会在跨域部署时暴露，而同源部署下完全正常。
 */
export const CSRFHeaderName = 'X-CSRF-Token'

/**
 * 读取当前应当携带的 CSRF 令牌。
 *
 * 未登录（还没拿到 cookie）时返回 undefined，调用方据此**不带头**：
 * 登录接口本身就是「还没有令牌」的状态，硬塞一个空值会让服务端
 * 把「空 == 空」判成通过（服务端已显式排除这种情况，但前端也不该依赖它兜底）。
 */
export function getCsrfToken(): string | undefined {
  return Cookies.get(CSRFCookieKey)
}

/**
 * 组装带 CSRF 令牌的请求头；没有令牌时返回空对象。
 *
 * 供**绕过 axios 的请求**使用（如 el-upload 内部的 XHR）——
 * 走 axios 的请求由 `api/index.ts` 的请求拦截器统一附加，不需要调它。
 */
export function csrfHeaders(): Record<string, string> {
  const token = getCsrfToken()
  return token ? { [CSRFHeaderName]: token } : {}
}
