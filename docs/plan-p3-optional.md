# P3 可选小项实施计划

> 生成于 **2026-09-25**，所有数据均为当日实测。
> ⚠️ **动手前先复核现状** —— `docs/review-fix-plan.md` 里已经吃过一次「待办清单静默失真」的亏
> （P3-3 的三条待办在做之前就已经不成立，因为没人回头更新）。
>
> 本文件是 `docs/review-fix-plan.md` 的 **P3-2**（token 迁 httpOnly cookie）与
> **P3-3**（前端代码分割）两个「记录但不在本轮做」的可选小项的详细实施计划。
> 单独成文的理由：两者各自都有 4~5 个阶段、涉及前后端与部署配置，塞进主计划会把
> 那份文档的主线（修复进度追踪）冲淡。

## 执行进度

| 步骤 | 状态 | 说明 |
|---|---|---|
| B4 自动续期 | ✅ 已完成 | 拆出来先做（修的是正在发生的缺陷）。`web/src/api/index.ts` 实现 401 → 续期 → 重放；新增 `web/src/api/refresh.spec.ts`（17 用例）；变异验证 5/5 转红。提交 `765a90f` |
| A1 图标白名单 | ✅ 已完成 | 主包 1267.9 → **1133.1 kB**（gzip 412.6 → 367.4）。新增 `web/src/utils/icons.ts` + `icons.spec.ts`（7 用例）；变异验证 5/5 转红；CDP 实机确认侧边栏 21/21 菜单图标都在。提交 `3067556` |
| — 前置修复：404 兜底回归 | ✅ 已完成 | A2 建视觉基线时发现**所有动态路由都渲染 404**（P3-2 的 catch-all 用了 `redirect`，它在 `beforeEach` 之前被解析）。兜底改 `component` + 守卫改按路径重导航；`static.spec.ts` 6 → 8 用例。详见 `docs/review-fix-plan.md` P3-2 段 |
| A2 去全量 EP 注册 | ✅ 已完成 | 首屏 **1506 kB → 473.6 kB**（JS 1133.16 → 385.08 kB、CSS 373 → 88.49 kB），约 **−69%**；dist 3.0M → 2.3M。`main.ts` 去 `app.use(ElementPlus)` + 全量 CSS；`App.vue` 补 `<el-config-provider>` 顶 locale/size；`role/index.vue` 的 `ElTree` 改 type-only import（显式 import 会让解析器跳过 `<el-tree>`，连带丢样式）。验证：22 页像素级对比无回归、48 项样式完整性校验、21 页控制台无解析失败、`v-loading` 实测生效 |
| A3 manualChunks | ⏳ 待做 | |
| A4 wangEditor 异步 | ⏳ 待做 | |
| A5 产物体积门禁 | ⏳ 待做 | |
| B1 后端双读 cookie | ⏳ 待做 | |
| B2 前端 cookie-only | ⏳ 待做 | |
| B3 CSRF | ⏳ 待做 | |

---

## 项目 A · 前端代码分割

### A.1 基线（2026-09-25 实测 `npm run build`）

| 产物 | 原始 | gzip | 说明 |
|---|---|---|---|
| `index-EHzuD36u.js` | **1237 kB** | 412 kB | 主包，**每次首屏必载** |
| `agreement-B8dowNhF.js` | **801 kB** | 285 kB | wangEditor，仅协议页用 |
| `index-14KwwuTl.css` | **373 kB** | 51 kB | element-plus **全量** CSS |
| `dist/` 合计 | 3.0 MB | | |

`vite` 已给出明确警告：

```
(!) Some chunks are larger than 500 kB after minification.
```

也就是说 **这个问题一直在被报告，只是没人看构建输出**。

### A.2 根因（3 条，按收益排序）

**① `main.ts:2,16` 全量注册 Element Plus —— 把按需解析完全架空了**

```ts
import ElementPlus from 'element-plus'          // ← 全量
import 'element-plus/dist/index.css'            // ← 全量 CSS，373 kB
app.use(ElementPlus, { locale: zhCn, size: 'default' })
```

`vite.config.ts` 里配了 `unplugin-vue-components` + `ElementPlusResolver`（按需），
但全量 `app.use` 让**所有组件同时进主包**。解析器其实还在跑 —— 证据是产物里
同时存在 `el-switch-*.css`、`el-table-column-*.css` 这类按需 CSS，**说明同一份
样式被引入了两遍**。

**② `main.ts:3,20-22` 全量注册 293 个图标**

```ts
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component)
}
```

`@element-plus/icons-vue/dist/index.js` = **320 kB / 293 个图标**。全仓实际只用到 **约 30 个**：

- 模板里直接写的（21）：`Aim ArrowDown Check CircleCheck CircleClose Close Delete Document Edit Expand Fold HomeFilled Menu Moon Operation Plus Refresh Sunny SwitchButton User VideoCamera`
- 数据库 `sys_menu.icon` 里的（**17 个**，实测 `SELECT DISTINCT icon FROM gin.sys_menu`）：
  `Briefcase Coin Collection Document FolderOpened Lock Menu Message OfficeBuilding Position PriceTag Tools TrendCharts Upload User UserFilled Wallet`

**③ `vite.config.ts` 完全没有 `build` 段**

没有 `rollupOptions.output.manualChunks` → 无 vendor 分包。后果是**任何一次业务代码
改动都会让整个 1237 kB 的缓存失效**。而 `deploy/nginx/default.conf` 给 `/assets/`
设了 `expires 1y; immutable` —— 缓存策略是对的，但没有分包，等于每次发版
所有用户重新下载 412 kB（gzip）。

### A.3 约束（踩了就会坏功能）

- ⚠️ **`SidebarItem.vue:13` 用 `<component :is="item.meta.icon" />` 按字符串名解析图标**，
  图标名来自数据库。所以**不能**直接删掉全局注册，必须改成**白名单注册表**。
  这同时是个安全收益：现在数据库里填任何字符串都会被当成组件名去解析。
- ⚠️ `app.use(ElementPlus, { locale: zhCn, size: 'default' })` 除了注册组件，还**全局设置了
  locale 与 size**。删掉它必须补 `<el-config-provider :locale="zhCn" size="default">`，
  否则分页「共 x 条」、日期选择器、上传按钮文案会退回英文。`App.vue` 目前只有一行
  `<router-view />`，正是加它的位置。
- ⚠️ `ElementPlusResolver` **只处理模板里的组件标签**，不处理 JS 调用。
  全仓有 3 个函数式 API 在用：`ElMessage`(73 处)、`ElMessageBox`(23 处)、`ElLoading`(1 处)。
  删掉全量 CSS 后必须确认这三者的样式仍在（按需解析不会自动带上它们的 CSS）。
  **这是本项最容易漏、且只有肉眼能发现的一步。**
- ⚠️ 各处的 `import { ElMessage } from 'element-plus'` 是**具名导入**，不受 `app.use` 影响，
  删掉全量注册后照常工作 —— 不要顺手把它们也删了。

### A.4 分步计划

**A1 · 图标改白名单注册表**（收益最大、风险最低，先做）

- 新建 `web/src/utils/icons.ts`：显式 import 那约 30 个图标，导出
  `export const appIcons: Record<string, Component>`
- `main.ts` 改为遍历 `appIcons` 注册，删掉 `import * as ElementPlusIconsVue`
- 新增 vitest 用例断言「数据库菜单在用的 17 个图标名都在表里」——
  否则将来加菜单时图标会**静默变空白**（`<component :is="不存在的名字">` 不报错，只是不渲染）
- 预期：主包 **−250~300 kB**

**A2 · 去掉 Element Plus 全量注册与全量 CSS**

- `main.ts` 删 3 行：`import ElementPlus`、`app.use(ElementPlus, ...)`、`import 'element-plus/dist/index.css'`
- `App.vue` 用 `<el-config-provider :locale="zhCn" size="default">` 包住 `<router-view />`
- 逐个确认 `ElMessage` / `ElMessageBox` / `ElLoading` 的样式仍在；缺则显式补
  `import 'element-plus/theme-chalk/el-message.css'` 等
- **必做**：`npm run build` 后用真实浏览器逐页比对（表格、分页、弹窗、消息提示、上传、富文本）
- 预期：CSS 373 kB → 约 120 kB；主包 **−300~400 kB**

**A3 · 加 `manualChunks` 分包**（可与 A1/A2 并行）

- `vite.config.ts` 加 `build.rollupOptions.output.manualChunks`：
  - `vendor-vue`：`vue` / `vue-router` / `pinia`
  - `vendor-element`：`element-plus` / `@element-plus/icons-vue`
  - `vendor-utils`：`axios` / `js-cookie` / `nprogress` / `path-to-regexp`
- 目的**不是**减少总量，而是让框架层与业务层的缓存独立 ——
  改业务代码不再让用户重下 element-plus
- 预期：主包只剩约 200 kB 业务代码，`vendor-*` 长期命中 `immutable` 缓存

**A4 · wangEditor 异步化**（2 行改动）

- `views/settings/agreement.vue:159` 的
  `import WangEditor from '@/components/WangEditor/index.vue'`
  → `defineAsyncComponent(() => import('@/components/WangEditor/index.vue'))`
- 让 801 kB 的 chunk 不再阻塞协议页自身渲染（页面壳与表单先出来）
- 收益只在该页生效，但代价几乎为零

**A5 · 加产物体积回归门禁**（推荐，与 P2-2 的教训一致）

- 新建 `web/scripts/check-bundle.mjs`：读 `dist/assets/` 统计各 chunk 原始 + gzip 体积，
  超阈值则 `exit 1`
- 阈值建议：**首屏主包 gzip ≤ 150 kB**、**任意 chunk ≤ 500 kB**（与 vite 的警告线一致）
- 接进 `package.json` 的 `build` 之后或 CI
- **为什么必须有**：本次问题的本质是「产物悄悄涨到 1.27 MB 而无人发现」——
  与 P2-2 那个「`golangci-lint` 因为版本不对而根本没在检查代码，`continue-on-error`
  又让它连失败都不显眼」是**同一类失效**。不加门禁，改完过几个月还会涨回来

### A.5 验收标准

- `npm run build` **无** chunk > 500 kB 警告
- 主包 gzip ≤ 150 kB；CSS gzip ≤ 60 kB
- 侧边栏 17 个数据库图标**逐个页面**确认正常显示
- 中文 locale 正常（分页文案、日期选择器、上传按钮）
- `npm run test` / `typecheck` / `lint` / `format:check` 全绿
- 图标白名单用例**变异验证**：从表里删掉一个 DB 在用的图标 → 用例转红

### A.6 风险与回滚

- **最大风险是 A2 的样式回归**，且**只有肉眼能发现** → 必须实机逐页比对，不能只跑测试
- 回滚：A1/A2 是 `main.ts` 与 `App.vue` 的独立改动，`git revert` 单个提交即可；
  A3 只影响构建配置；A4 两行；A5 是新增文件

---

## 项目 B · token 迁 httpOnly cookie

### B.1 现状（2026-09-25 实测）

| 环节 | 现状 | 位置 |
|---|---|---|
| 存储 | access + refresh 都在 **JS 可读** cookie（`js-cookie`） | `web/src/utils/auth.ts` |
| 传输 | 前端注入 `Authorization: Bearer` 头 | `web/src/api/index.ts:44` |
| 后端读取 | `middleware.Auth` **只认** `Authorization` 头，**不读 cookie** | `internal/middleware/auth.go:20-33` |
| 后端写入 | **不设任何 cookie** | — |
| CORS | `allow_credentials: true` 已开 | `config/config.yaml:56` |
| 部署形态 | dev 走 Vite proxy、prod 走 nginx `location /api/` → **两种环境都同源**，不产生跨域 | `deploy/nginx/default.conf` |
| CSRF | **前后端都没有任何防护**（实测 `grep -i csrf` 为空） | — |
| 自动续期 | **`refreshToken()` API 定义了但从未被调用** | `web/src/api/auth.ts:35` |
| 登录态判定 | 路由守卫**同步**读 `getToken()` | `web/src/router/index.ts:24` |
| 前端测试环境 | vitest `environment: 'node'`，**jsdom 未安装** | `web/vitest.config.ts` |

**本项的真实收益只有一个**：把 token 从 XSS 可达范围里拿掉。
代价是引入 CSRF 面、重构登录态判定、以及一个必须处理的部署耦合。

### B.2 真正的障碍（这才是要单独立项的原因）

**① httpOnly cookie 对 JS 不可见 → 路由守卫的 `getToken()` 会永远返回 `undefined`。**

守卫是**同步**的（`router/index.ts:24`），而「服务端有没有给我发 cookie」只能靠一次请求探测。
直接改会变成「永远判定未登录 → 死循环跳 `/login`」。必须引入新的登录态判定机制（见 B2）。

**② 引入 cookie 就等于引入 CSRF 面。**

现在 token 在自定义头里，浏览器**不会自动携带**，天然免疫 CSRF；
改 cookie 后浏览器会自动带，必须显式处理。

**③ `SameSite` 语义与部署形态耦合，且这个耦合必须显式表达。**

dev 与 prod 都是同源 → `SameSite=Lax` 够用且最安全。
但**一旦将来前后端分域名部署**（`admin.example.com` + `api.example.com`），
Lax 会让 cookie 不被携带，必须改 `SameSite=None; Secure` ——
而那正是 CSRF 风险最高的配置。**不能让这个决定藏在默认值里**，
要写成配置项并在启动日志打印当前取值（同 `server.trusted_proxies` 的做法）。

**④ Swagger / curl / 第三方调用方会失效** —— 它们不会自动带 cookie。
所以 `Authorization` 头**不能删**，只能作为并存路径。

### B.3 分阶段计划（每阶段独立上线、独立可回滚）

**B1 · 后端支持「双读」Cookie（前端一行不改）**

- 新增配置 `security.token_transport: header|both|cookie`，**默认 `both`**。
  用显式默认值兜底（参照 `IsLoginFailClosed()` 的教训：Go 的零值恰好落在不安全那侧，
  所以漏配必须落到安全侧）
- `middleware.Auth` 抽出 `extractToken(c) (token, source string)`：
  **`Authorization` 头优先，其次 cookie**（`access_token`）。返回 source 便于日志区分
- 登录 / 刷新成功时 `Set-Cookie`：
  - `access_token`：`HttpOnly; SameSite=Lax; Path=/`
  - `refresh_token`：`HttpOnly; SameSite=Lax; Path=/api/v1/auth/refresh`
    （**收窄 Path**：refresh token 只在这一个端点上需要，收窄能显著减少暴露面）
  - `Secure` 由配置决定（HTTPS 部署必须开）；`MaxAge` **从 `jwt.access_expire` /
    `refresh_expire` 读，不写死**
- 登出时 `Set-Cookie` 清空（`MaxAge=-1`）
- **`deploy/config.docker.yaml` 必须同步改** —— 两份模板漏改一处不会报错，
  只会静默行为不一致；`config.TestConfigTemplatesParse` 会**断言取值**，用来兜住这个
- 验收：`curl` 直接打 `/auth/login` 能看到 `Set-Cookie`；带 cookie 打 `/auth/userInfo` 通过；
  带 `Authorization` 头的老路径照常工作
- **本阶段前端一行不改，风险为零，可以先上生产**

**B2 · 前端切换 cookie-only + 重建登录态判定**

- **先补测试**：vitest 引入 `jsdom`（当前是 `node` 环境），为路由守卫补用例。
  **顺序不能反** —— 先有守卫用例，才能保证「改坏了」被抓住
- `api/index.ts`：加 `withCredentials: true`（同源下非必需，但显式声明意图）；
  注入 `Authorization` 头的逻辑由 `token_transport` 决定是否保留（保留它是回滚开关）
- `utils/auth.ts`：删 `getToken/setToken/getRefreshToken/setRefreshToken`，
  改为只维护一个**非敏感的登录态标记**，两个方案：
  - **方案 ①（推荐）**：服务端额外下发一个 `logged_in=1` 的**非 HttpOnly** cookie，
    JS 只读它判断「要不要去拉用户信息」。它不含任何凭据，被伪造也只能让前端多发一次
    `/auth/userInfo`（然后 401 回来清会话）—— 攻击者拿不到任何东西
  - 方案 ②：不存标记，守卫改成**异步探测** `/auth/userInfo`。更干净，
    但每次冷启动多一次请求，且守卫要处理「探测中」的中间态（否则会闪一下登录页）
- 守卫要区分 `undefined`（**还没探测**）与 `''`（**确认未登录**）—— 现在这两个值
  被 `getToken()` 揉成了同一个，是 B2 最容易写错的地方
- `store/modules/user.ts`：`state.token` 不再从 cookie 读；`clearSession()` 去掉 `removeToken()`
- 验收：登录 → **刷新页面仍是登录态**；DevTools 里 `document.cookie` **看不到任何 token**；
  登出后刷新回到登录页

**B3 · CSRF 防护**

- **主防线：`SameSite=Lax`**，写成配置项 + 启动日志打印当前值
- **纵深防御：双提交 Cookie（double-submit）**
  - 登录时额外下发一个**非 HttpOnly** 的 `csrf_token`（随机值）
  - 前端每次非 GET 请求把它放进 `X-CSRF-Token` 头
  - 中间件校验「头里的值 == cookie 里的值」（无需服务端存储）
  - 只对**非 GET/HEAD/OPTIONS** 生效，且**只对走 cookie 认证的请求**生效 ——
    走 `Authorization` 头的 API 客户端自动豁免。这是 B1 双读设计带来的额外好处
- 测试（`internal/middleware`）：缺头拒绝 / 头与 cookie 不一致拒绝 / GET 放行 /
  走 `Authorization` 头时豁免
- **变异验证**：把校验改成恒真 → 用例必须转红

**B4 · 补上自动续期（顺带修掉「refresh token 只写不用」）**

- 现状：`refreshToken()` API 定义了但从未被调用 → 用户 **2 小时后**
  （`jwt.access_expire: 7200`）会被直接踢到登录页，而手里明明有 **7 天有效**的
  refresh token。**这是当前就存在的体验缺陷**，与 cookie 改造无关，只是同一片代码
- `api/index.ts` 响应拦截器：收到 401 时先用 refresh token 换新 access token，
  **重放原请求**；刷新失败才走 `handleLogout()`
- 必须处理**并发刷新**：多个请求同时 401 时只发一次刷新请求，其余等它 ——
  否则并发消耗 refresh token 会把用户踢出去
- 必须处理**刷新请求自身的 401**（不能再触发刷新，否则无限递归）
- 测试：打桩 axios adapter，覆盖「单个 401 刷新成功并重放」「3 个并发 401 只发 1 次刷新」
  「刷新失败清会话」

### B.4 验收标准

- DevTools → Cookies：`access_token` / `refresh_token` 的 `HttpOnly` 列为 ✅，
  `document.cookie` 读不到任何 token（**这是本项的唯一真正收益，必须实测**）
- Swagger UI 仍可用（走 `Authorization` 头路径）
- 伪造 `Origin: https://evil.example` 的跨站 POST 被 CORS 拒绝；即使绕过 CORS，
  也因 `SameSite=Lax` 不带 cookie
- `token_transport` 三种取值都有用例；两份配置模板断言取值
- 全量 `make check` 全绿

### B.5 风险与回滚

- **最高风险是 B2 的守卫改动** → 必须**先**补 vitest + jsdom 的守卫用例
- **`token_transport` 就是回滚开关**：出问题改回 `header`，前端 `Authorization`
  注入逻辑留在 `if` 后面即可立刻回退
- **不要「一次性删掉前端 cookie 代码」** —— 那样出问题只能整包回滚

---

## 排期与依赖

| 项 | 依赖 | 建议顺序 |
|---|---|---|
| A1 图标白名单 | 无 | **先做**（收益最大、风险最低） |
| A2 去全量 EP 注册 | A1 | |
| A3 manualChunks | 无 | 可与 A1/A2 并行 |
| A4 wangEditor 异步 | 无 | 顺手 |
| A5 产物体积门禁 | A1~A4 | 收尾 |
| B1 后端双读 cookie | 无 | 与 A 组完全独立，可并行 |
| B2 前端 cookie-only | **先补 vitest + jsdom 守卫用例** | |
| B3 CSRF | B2 | |
| B4 自动续期 | **无** | 见下 |

**建议把 B4 拆出来单独先做。** 它与 cookie 改造没有任何依赖，修的是一个
**当前正在发生的体验缺陷**（2 小时后被踢），且改动集中在 `api/index.ts` 一个文件。
把它绑在 B 组里一起等排期，等于让一个已经存在的 bug 继续挂着。

---

## 两个项目的共同点

都不是「加功能」，而是**修掉一个已经在发生、但没人看见的退化**：

- **A 组**：产物悄悄涨到 1.27 MB，vite 早就给了警告，只是没人看构建输出
- **B 组**：refresh token 早就定义了，只是从没被调用 ——
  「用户 2 小时被踢」到底是设计如此还是忘了接线，已经分不清了

处理方式也一样：**先立一个会响的观测项**（A5 的体积门禁、B 组的 cookie 属性断言），
否则改完还是没人知道它有没有再退化。这与 P2-2 的教训是同一条 ——
`golangci-lint` 因为版本不对而根本没在检查代码，而 `continue-on-error: true`
让它连失败都不显眼。**观察项在观察项失效时是最危险的组合。**

---

## 附：本次实测命令与原始数据

```bash
# 产物体积（web/）
rm -rf dist && npm run build            # 看 chunk 列表与 vite 的 >500kB 警告
ls -S dist/assets/*.js | head -5        # 主包 1237 kB / agreement 801 kB
du -sh dist/                            # 3.0 MB
ls -la node_modules/element-plus/dist/index.css          # 357653 B
ls -la node_modules/@element-plus/icons-vue/dist/index.js # 320786 B
node -e "console.log(Object.keys(require('@element-plus/icons-vue')).length)"  # 293

# 数据库实际使用的菜单图标（17 个）
/i/phpstudy_pro/Extensions/MySQL5.7.26/bin/mysql.exe -uroot -p123456 -N \
  -e "SELECT DISTINCT icon FROM gin.sys_menu WHERE icon <> '' ORDER BY icon;"

# token 相关现状
grep -rn "csrf\|CSRF" web/src internal router          # 空
grep -rn "refreshToken" web/src/api web/src/store      # 只有 logout 用到，无自动续期
grep -rn "Authorization" internal/middleware/auth.go   # 只读头，不读 cookie
```
