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
| A3 manualChunks | ✅ 已完成（**有偏离**） | `vite.config.ts` 加 `build.rollupOptions.output.manualChunks`：`vendor-vue` 110.8 kB、`vendor-utils` 50.7 kB、`vendor-wangeditor` 792.9 kB（固定名，供 A5 门禁按名识别）。**计划里的 `vendor-element` 刻意没做** —— 见下方「A3 的偏离」 |
| A4 wangEditor 异步 | ✅ 已完成 | `views/settings/agreement.vue` 改 `defineAsyncComponent(() => import('@/components/WangEditor/index.vue'))`；`agreement` chunk **820.91 kB → 8.41 kB**，wangEditor 被拆到独立的 `vendor-wangeditor`。实机验证：点开「新增协议」弹窗，工具栏 40 个按钮 + 编辑区 + `contenteditable` + 占位符全部正常，控制台无 error |
| A5 产物体积门禁 | ✅ 已完成 | 新增 `web/scripts/check-bundle.mjs`（读 `dist/index.html` 解析首屏引用 + `gzipSync` 算体积 + 按 `-<hash>.js` 剥名匹配例外表）；接进 `npm run build`（`vue-tsc && vite build && node scripts/check-bundle.mjs`）与 CI 的 `构建 + 产物体积门禁` 步骤。实测首屏 JS gzip **137.9 kB** / CSS **13.1 kB**，通过 |
| B1 后端双读 cookie | ✅ 已完成（**有一处偏离**） | 新增 `security.token_transport`（header/both/cookie，默认 both）+ `cookie_secure` + `cookie_same_site`；`middleware.Auth` 抽出 `extractToken`（头优先、其次 cookie）；新增 `internal/authcookie` 包负责下发/清除；登录/刷新写 cookie、登出清 cookie 并**吊销 refresh token**；`deploy/config.docker.yaml` 补上**此前完全缺失的 security 段**；启动日志打印 cookie 策略。前端一行未改。**`refresh_token` 的 Path 从计划的 `/api/v1/auth/refresh` 放宽到 `/api/v1/auth`**，理由见 B.3 的注 |
| B2 前端 cookie-only | ✅ 已完成（**有 3 处计划外改动**） | 删 `getToken/setToken/…` 改为非敏感 `logged_in` 标记；守卫改同步判定；`api/index.ts` 加 `withCredentials`、去掉 Authorization 注入、续期不再传 body；`Upload` 组件去掉手写的 `Authorization` 头。新增 `utils/auth.spec.ts`（7）+ `router/index.spec.ts`（9）；后端补 `/auth/refresh` 的 cookie 取值（**计划外**）。实机：B2 冒烟 **27/27**、浏览器端 **13/13**（含「刷新后仍是登录态」）。详见下方「B2 的偏离」 |
| B3 CSRF | ✅ 已完成（**计划外修掉一个既有缺陷**） | double-submit：`authcookie` 下发非 HttpOnly 的 `csrf_token`（32 字节 hex，寿命跟 refresh token）；前端 `api/index.ts` 非 GET 请求带 `X-CSRF-Token`（`Upload` 组件手工补，它不走 axios）；新增 `middleware.CSRF`（只校验「走 cookie 认证的非安全方法」，走头的 API 客户端自动豁免）。验证：后端冒烟 **21/21**、浏览器端 **18/18**、变异验证 4 组全转红。**顺带修掉 `DrainBody`**：见下方「B3 顺带修掉的既有缺陷」 |

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

> **实施时的偏离（2026-09-25 补记）**：上面的 `vendor-element` **没有做**，这是有意的。
> 这条计划写在 A2 之前，当时 `element-plus` 是**整体**进主包的，所以「把 element-plus
> 整块抽成 vendor」是合理的。A2 去掉全量注册后，`unplugin-vue-components` 的按需解析
> 才真正生效，Vite 已经把 element-plus **按组件**自动拆成了 `el-table-column` /
> `el-select` / `el-tree` / `el-form-item` … 几十个小 chunk（其中 `el-table-column`
> 88.6 kB **只在首屏用**，其余多数按路由懒加载）。
> 此时再写一条 `manualChunks` 把 element-plus 合并回一个 `vendor-element`，
> 等于**把 A2 的收益吐回去**：首屏会从「只下首屏真正用到的那几个组件」
> 退回「下一次全量 element-plus」。
>
> 所以 A3 只对**真正跨页共享且不随 A2 拆分**的包做分包：`vue`/`vue-router`/`pinia`/
> `@vue/*` → `vendor-vue`，`axios`/`js-cookie`/`nprogress`/`path-to-regexp` → `vendor-utils`，
> 另把 `@wangeditor/*` 钉成固定名 `vendor-wangeditor`（哈希名无法被 A5 门禁按名识别）。
> **A3 的验收口径也随之从「主包只剩业务代码」改成「第三方大包有稳定 chunk 名 +
> 业务代码改动不波及它们」**，体积指标交给 A5 的门禁去守。

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
  - **实际口径（2026-09-25 定稿）**：`vendor-wangeditor` 792.9 kB **仍然超 500 kB**，
    上游库体积决定，压不下来。处理方式是给它一条**带理由和上限的显式例外**
    （`check-bundle.mjs` 的 `CHUNK_ALLOWLIST`：`≤ 900 kB`），
    而**不是**删掉检查、也不是调大 vite 的 `chunkSizeWarningLimit` 了事 ——
    排除掉它就等于允许它无限增长。它已异步化，只在协议页加载，不进首屏。
    除此之外的所有 chunk 都 ≤ 500 kB。
- 主包 gzip ≤ 150 kB；CSS gzip ≤ 60 kB
  - 实测：首屏 JS gzip **137.9 kB**、CSS gzip **13.1 kB**，均在阈值内（`check-bundle.mjs` 已固化）
- 侧边栏 17 个数据库图标**逐个页面**确认正常显示
- 中文 locale 正常（分页文案、日期选择器、上传按钮）
- `npm run test` / `typecheck` / `lint` / `format:check` 全绿
- 图标白名单用例**变异验证**：从表里删掉一个 DB 在用的图标 → 用例转红

#### 验证记录（2026-09-25 实测）

A3/A4/A5 都是「改动本身不报错、出问题只有肉眼能看见」的类型，所以全部实机验证：

| 手法 | 脚本（`runtime/`，已 gitignore） | 结果 |
|---|---|---|
| 22 页批量巡检（整页加载 → 查最终路径 / 菜单挂载 / 表格 / 控制台 error） | `cov/verify_a3a4.mjs` | 21 个业务页全部停在原路径、菜单 21 项、**控制台 0 条 error/warning**；`/nope/nope` 渲染 404 页且保留原路径 |
| 协议页编辑器专项（真的点「新增协议」按钮，轮询等异步组件） | `cov/verify_a4_editor.mjs` | 工具栏 40 个按钮、编辑区、`contenteditable`、占位符「请输入内容...」全部正常 |
| 产物门禁自测 | `scripts/check-bundle.mjs` | 通过：首屏 JS gzip 137.9 ≤ 150、CSS gzip 13.1 ≤ 60；`vendor-wangeditor` 792.9 ≤ 900（例外） |

**两个「先怀疑代码、结果发现是断言错」的坑（都留了注释）**：

1. 第一版编辑器探测查了**列表页**的 DOM —— 而 `/settings/agreement` 是列表页，
   编辑器在弹窗里，于是「编辑器未渲染」是**假阳性**。改成真的点按钮才验到真东西。
2. 控制台那条 `编辑区域高度 < 300px 这可能会导致 modal hoverbar 定位异常` 一开始被判成
   A4 引入的回归（异步化确实会改变组件挂载时机，从而可能改变容器测量高度 —— 这个
   因果链是成立的，所以不能靠推理否掉）。**用对照实验证伪**：把 `defineAsyncComponent`
   临时改回静态 import 再跑一次，这条 warning 一字不差地照旧出现 →
   与 A4 无关，是 wangEditor 自身的 advisory。改完再把异步写法还原。

### A.6 风险与回滚

- **最大风险是 A2 的样式回归**，且**只有肉眼能发现** → 必须实机逐页比对，不能只跑测试
- 回滚：A1/A2 是 `main.ts` 与 `App.vue` 的独立改动，`git revert` 单个提交即可；
  A3 只影响构建配置；A4 两行；A5 是新增文件（`web/scripts/check-bundle.mjs`）
  加上三处调用点（`web/package.json` 的 `build` / `.github/workflows/ci.yml` / `Makefile`），
  要回滚需一并处理，否则 `npm run build` 会因找不到脚本而失败

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
  - `refresh_token`：`HttpOnly; SameSite=Lax; Path=/api/v1/auth`
    （**收窄 Path**，但比原计划放宽了一级，见下面的注）
  - `Secure` 由配置决定（HTTPS 部署必须开）；`MaxAge` **从 `jwt.access_expire` /
    `refresh_expire` 读，不写死**
- 登出时 `Set-Cookie` 清空（`MaxAge=-1`），并且**先吊销 refresh token 再清 cookie**
- **`deploy/config.docker.yaml` 必须同步改** —— 两份模板漏改一处不会报错，
  只会静默行为不一致；`config.TestConfigTemplatesParse` 会**断言取值**，用来兜住这个
- 验收：`curl` 直接打 `/auth/login` 能看到 `Set-Cookie`；带 cookie 打 `/auth/userInfo` 通过；
  带 `Authorization` 头的老路径照常工作
- **本阶段前端一行不改，风险为零，可以先上生产**

> **实施时的偏离（2026-09-25 补记）**：`refresh_token` 的 Path 原计划是
> `/api/v1/auth/refresh`（更窄），实施时改为 **`/api/v1/auth`**。
> 原因：cookie 的 Path 决定浏览器**会不会把它发到某个路径**，写成
> `/api/v1/auth/refresh` 就永远发不到 `/auth/logout`，服务端拿不到它去吊销 ——
> 结果是「登出只拉黑了 access token，手里握着 refresh token 的人仍可换发新的
> access token」，等于没登出。而这一点在 `auth_service.LogoutByToken` 的注释里
> **已经踩过一次**（历史上正是把 access token 当 refresh token 去删）。
> `/api/v1/auth` 已排除全部业务接口（`/system`、`/member`、`/payment`…），
> 相对 `Path=/` 仍是显著收窄。`internal/authcookie` 里有一条用例专门钉住
> 「必须覆盖 /auth/logout、且不得覆盖业务接口」。
>
> **另一处实施决定**：`cookie_secure` 的**默认值是 `false`**，这与本项目
> 「默认值即安全值」的惯例相反，是刻意的 —— 自带编排是纯 HTTP
> （`deploy/nginx/default.conf` 只监听 80），默认 `true` 会让浏览器直接丢弃
> cookie、登录态完全建立不起来，属于「安全默认值把默认部署打死」。
> 因此改为：两份模板**显式写出**该值（`TestConfigTemplatesParse` 断言它必须非 nil），
> 且启动日志打印当前取值并附风险提示。`cookie_same_site=none` 但 `secure=false`
> 会被 `Validate` **拒绝启动**（浏览器一定丢弃这种组合，属于硬约束）。

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

> **实施时的偏离（2026-09-25 补记）**
>
> **① 计划之外必须补的后端改动：`/auth/refresh` 要能「body 为空、凭据在 cookie」。**
> 计划把 B2 写成纯前端阶段（后端 B1 已改完），但漏了一点：`RefreshTokenRequest.RefreshToken`
> 带 `binding:"required"`，而 B2 之后前端**读不到** refresh token（HttpOnly），
> 请求体必然是空的 —— 不改后端的话，浏览器端**所有**自动续期都会在参数绑定这一步 400，
> 现象是「token 一过期就被踢回登录页」，与 B4 修过的缺陷一模一样，极难怀疑到参数绑定。
> 因此补了三处：`dto.RefreshTokenRequest` 去掉 `required`；Controller 用
> `errors.Is(err, io.EOF)` 把「空 body」与「坏 JSON」分开（前者继续走 cookie 取值，后者仍 400）；
> 取值改走 `authcookie.RefreshFromRequest`（body 优先、其次 cookie），与 Logout 同一套逻辑。
> 用例：`TestAuthControllerRefreshAcceptsCookieCredential`（4 组场景，含坏 JSON 与无凭据）。
>
> **② `logged_in` 标记的有效期跟着 refresh token（7 天），不跟 access token（2 小时）。**
> 跟 access token 走的话，2 小时后标记先失效，用户刷新页面时会被守卫在
> **发出任何请求之前**就判定未登录并跳登录页 —— B4 的「401 → 续期 → 重放」根本没机会执行。
> 跟着 refresh token 走，守卫才会放行到 `/auth/userInfo`，由那里的 401 触发续期。
> 代价是「标记还在但 access token 已过期」成为常态，这是设计允许的：
> 标记本来就不代表会话有效（`LoginFlagCookieName` 与前端 `utils/auth.ts` 都写明了这点）。
>
> **③ 回滚粒度变了：`token_transport: header` 不再是 B2 之后的一键回滚开关。**
> 计划里写「注入 `Authorization` 头的逻辑……保留它是回滚开关」，但 B2 删掉 `getToken()`
> 之后前端**没有数据源**可注入。把后端改回 `header` 会让两边都拿不到凭据
> （后端不发 cookie、前端不注入头），结果是全站登录不上。
> 所以 B2 之后回滚的粒度是「前后端一起回退到 B2 之前的提交」。
> 这一点写进了 `api/index.ts` 的注释 —— 否则后人会以为改个配置就能回滚。
>
> **另外两处顺带的决定：**
> - `api/auth.ts` 的 `refreshToken()` 封装**删掉**了（B4 之后自动续期直接调 `/auth/refresh`，
>   那个封装需要调用方自己传 token，而 B2 之后前端根本拿不到）。留着会重演 B4 修过的
>   「定义了但没人用」的坑。
> - `components/Upload/index.vue` 去掉手动塞的 `Authorization: Bearer ${getToken()}`
>   （JS 读不到 HttpOnly cookie，只会拼出 `Bearer undefined`，后端当成「Token格式错误」
>   返回 401 —— 比不传更糟，因为 `Authorization` 头在 both 模式下是**优先**的）；
>   el-upload 的 XHR 在同源下会自动带 cookie。
>
> **测试基础设施的两处补强：**
> - vitest 引入 `jsdom`，用**文件级** `// @vitest-environment jsdom` 覆盖（默认仍是 node）。
>   新增 `utils/auth.spec.ts`（7 用例）与 `router/index.spec.ts`（9 用例）。
>   守卫用例的 `@/utils/auth` mock **只导出 `isLoggedIn`** —— 实现若回退去调 `getToken()`，
>   会直接抛 `is not a function`，这比「断言调用了新接口」更能防回退
>   （变异验证里这一处让 8 个用例同时转红）。
> - 本地联调用的 `runtime/smoke/serve.mjs` 修了一个 bug：它用 `headers.forEach` 转发
>   `Set-Cookie`，而 undici 会把**多个 Set-Cookie 合并成逗号分隔的一个**，浏览器无法可靠拆回，
>   导致只有部分 cookie 被保存（现象是「登录返回 200 但下一个请求 401」）。
>   改用 `getSetCookie()` 逐个转发。**这是本地脚本的问题，不是产品代码的问题** ——
>   直连 8080 一直是 3 个 Set-Cookie，只有经这个自建代理才变成 1 个。
>
> **实机验证的另外两个坑（都记在脚本注释里，避免下次重踩）：**
> - 浏览器端验证必须用 `http://localhost:3000` 而不是 `127.0.0.1:3000`：
>   `cors.allow_origins` 里写的是前者，Origin 对不上时浏览器发的 POST
>   （`Content-Type: application/json` 属非简单请求）会先发 OPTIONS 预检，
>   后端 CORS 直接回 **403 且 body 为空** —— 看起来像「登录静默失败」。
>   GET 请求不受影响（简单请求不发预检），所以问题只在登录/登出这类 POST 上暴露。
> - 「导航后是否仍在目标页」的判据**不能只看 `location.pathname`**：
>   导航瞬间 pathname 就已经是目标值了，守卫的重定向还没发生 ——
>   第一版据此报「停在 /dashboard」，而实际上登录是 403 失败的。

**B3 · CSRF 防护** ✅ 已完成

- **主防线：`SameSite=Lax`** —— 即 B1 落地的 `security.cookie_same_site`，
  启动日志会打印当前取值（`CookiePolicySummary`）
- **纵深防御：双提交 Cookie（double-submit）**
  - 登录/续期额外下发一个**非 HttpOnly** 的 `csrf_token`（32 字节随机 hex，
    寿命跟 refresh token），清理由登出负责
  - 前端每次非 GET 请求把它放进 `X-CSRF-Token` 头
  - `middleware.CSRF` 校验「头里的值 == cookie 里的值」
    （`subtle.ConstantTimeCompare`，**服务端不存储任何东西**）
  - 只对**非 GET/HEAD/OPTIONS** 生效，且**只对走 cookie 认证的请求**生效 ——
    走 `Authorization` 头的 API 客户端自动豁免。这是 B1 双读设计带来的额外好处
  - 挂载位置：`Auth()` 之后、`CasbinAuth()` 之前。位置有语义：豁免规则依赖
    Auth 写进上下文的「凭据来源」；放在鉴权与操作日志之前，则被拦下的伪造请求
    不会消耗一次鉴权查询与一次日志写库
- 测试（`internal/middleware/csrf_test.go`）：15 组判定表（缺头 / 缺 cookie /
  两者都缺 / 值不一致 / 仅大小写不同 / PUT·DELETE·PATCH / GET·HEAD·OPTIONS 放行 /
  走头豁免 / 上下文无来源时 fail-closed）+ header 模式短路 + `isSafeMethod` 白名单方向

**三处「看起来可以简化、简化了就坏」的地方（都写进了代码注释）**：

1. **`csrf_token` 必须非 HttpOnly**。它看起来像凭据，很容易被后人「顺手加强」——
   而 double-submit 的机制就是「JS 读出 cookie 放进请求头」，加了 HttpOnly
   等于把机制本身关掉。退化后的现象极具迷惑性：GET 全部正常、登录正常，
   **只有写操作 403**。
2. **续期时不得轮换令牌**（`csrfTokenFor` 沿用请求已带的旧值）。续期发生在
   **并发请求的中间**：若此时换新值，那些「已读好 cookie、拼好头但还没发出去」
   的请求会带着旧值、而 cookie 已是新值 → 头与 cookie 不一致 → 403。
   窗口很窄但真实存在，现象是「偶发某一个请求失败」。
   沿用旧值安全性没有任何损失：令牌只承担「与 cookie 同源可比对」的职责，
   不负责标识会话新鲜度（那是 refresh token 的事）。
3. **令牌生成失败时不下发**（`csrfTokenFor` 返回空串则跳过写 cookie）。
   方向必须是安全的：宁可让前端拿不到令牌（后续写请求被拒），
   也不能退回一个可预测的值 —— 那会让 double-submit 退化成「填什么都通过」。

**两处计划外的接线**（不改则功能不成立）：

- `cors.allow_headers` 必须加 `X-CSRF-Token`（两份配置模板 + `cors.go` 的兜底默认值）。
  漏配的后果很隐蔽：同源部署（dev 的 Vite proxy、prod 的 nginx 反代）**根本不发预检**，
  漏了也一切正常；只有当有人把前端挪到另一个域名、自定义头开始触发预检时，
  所有写操作才会集体失败。`TestConfigTemplatesParse` 因此断言两份模板都必须含它。
- `Upload` 组件必须**手工**带这个头：`el-upload` 用自己的 XHR，不经过 axios 的
  请求拦截器，拿不到那里附加的 `X-CSRF-Token`。漏了这一行的现象是
  「其他写操作都正常，只有上传 403」。取值用 `computed` 而非挂载时取一次 ——
  令牌由服务端在登录/续期时下发，组件挂载时可能还没有。

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

#### B1 验证记录（2026-09-25 实测）

| 手法 | 位置 | 结果 |
|---|---|---|
| 单元：传输方式归一化 / 三种取值的「接受头 / 接受 cookie」组合 / Secure 与 SameSite 默认值 / 非法取值与 `same_site=none` 的启动期拒绝 / 启动日志摘要内容 | `config/config_test.go` | 全绿 |
| 单元：cookie 属性（HttpOnly、Path、MaxAge 来自 jwt 配置、SameSite、Secure）、header 模式下**一个 cookie 都不发**、登出清空的 Path 与下发一致、refresh token 取值优先级 | `internal/authcookie/cookie_test.go` | 全绿 |
| 单元：`extractToken` 的 17 组取值（头优先 / header 模式不读 cookie / 坏头不回退 / `Bearer ` 空值 / 大小写） | `internal/middleware/auth_token_test.go` | 全绿 |
| **端到端中间件**：真 `Auth()` + 真 Redis，7 组「传输方式 × 有头/有 cookie」全部断言到「处理函数是否真的被执行」 | `internal/middleware/auth_cookie_integration_test.go` | 全绿 |
| Controller 接线：登出是否真的下发清空 cookie（读真实响应头） | `user_auth_controller_test.go` | 全绿 |
| **实机冒烟**（`runtime/smoke/cookie_smoke.py`，打真服务） | — | **18/18 通过** |

实机冒烟覆盖的关键几条（都是单测覆盖不到「浏览器真实收到什么」的部分）：

- 登录响应里 `Set-Cookie: access_token=…; Path=/; Max-Age=7200; HttpOnly; SameSite=Lax`
  与 `refresh_token=…; Path=/api/v1/auth; Max-Age=604800; HttpOnly; SameSite=Lax`
- 只带 cookie 打 `/auth/userInfo` 通过；不带凭据 401；带 `Authorization` 头照常通过
- 登出下发两个 `Max-Age=0` 的 cookie，**且此后原 refresh token 换不出新 token**
  （返回 401「refresh token已过期」）—— 这条是「登出是否真的登出」的唯一判据

**变异验证（3/3 转红，均已恢复）**：

1. `extractToken` 去掉 cookie 分支 → `TestExtractToken` 与端到端用例同时转红
2. `writeCookie` 的 HttpOnly 由 `true` 改 `false` → cookie 属性用例转红
   （这条退化在功能上完全看不出来：登录照常能用）
3. `Transport()` 的默认值由 both 改成 header → 归一化用例转红

**顺带修掉的一个既有缺口**：`deploy/config.docker.yaml` **完全没有 `security` 段**
（`config/config.yaml` 有）。mapstructure 遇到缺失的段会静默留零值，
表现是「本地开发是对的、容器里行为不一样」且没有任何报错。
现已补齐，并让 `TestConfigTemplatesParse` 断言 `token_transport` /
`cookie_same_site` 的取值、以及 `cookie_secure` **必须显式配置**（不得依赖代码默认值）。

#### B3 验证记录（2026-09-25 实测）

| 手法 | 位置 | 结果 |
|---|---|---|
| 单元：15 组判定表（安全方法放行 / 缺头 / 缺 cookie / 两者都缺 / 值不一致 / 仅大小写不同 / PUT·DELETE·PATCH / 走头豁免 / 上下文无来源 fail-closed） | `internal/middleware/csrf_test.go` | 全绿 |
| 单元：`isSafeMethod` 白名单方向（TRACE / CONNECT / 空串 / 小写 `get` 都必须落在「需校验」一侧） | 同上 | 全绿 |
| 单元：`csrf_token` 属性（非 HttpOnly、Path=/、MaxAge 取自 refresh_expire、64 位 hex）、续期沿用不轮换、每次登录随机 | `internal/authcookie/cookie_test.go` | 全绿 |
| 单元：请求拦截器只在非 GET 上带头、无令牌时不写空值头、不缓存令牌 | `web/src/api/interceptor.spec.ts` | 全绿 |
| 单元：`getCsrfToken` / `csrfHeaders` 读的是 `csrf_token` 而不是 `logged_in` | `web/src/utils/auth.spec.ts` | 全绿 |
| **实机冒烟**（`runtime/smoke/b3_smoke.py`，打真服务） | — | **21/21 通过** |
| **浏览器端**（`runtime/smoke/b3_browser.mjs`，真 Chrome + CDP 抓请求头） | — | **18/18 通过** |

实机冒烟覆盖的关键几条（单测覆盖不到「浏览器/客户端真实收到什么」）：

- 登录下发 4 个 cookie，其中 `csrf_token` **不含 HttpOnly**、`Path=/`、
  `Max-Age=604800`（跟 refresh token 而非 access token）
- `GET /auth/userInfo` 不带 CSRF 头照常 200（跨站 `<img>`/`<script>` 触发的正是 GET）
- 缺头 / 头与 cookie 不一致 → 403，文案统一为「请求校验失败，请刷新页面后重试」
- 带对令牌 → 不再是 403 而是走到参数校验（400）；只带 `Authorization` 头、无 cookie
  无 CSRF 头 → 同样通过（Swagger / curl 无需任何改动）
- 续期换发了新 access token 而 `csrf_token` 一字未变；**且用续期前的令牌仍能通过**
  （证明没有竞态窗口）
- 登出清掉 `csrf_token`（`Max-Age=0`），且此后原 refresh token 换不出新 token

浏览器端覆盖的关键几条（前端的另一半，单测把 `getCsrfToken` 打了桩验不到）：

- `document.cookie` 里**能**读到 `csrf_token`（64 位 hex），而 `access_token` /
  `refresh_token` 仍然读不到 —— 两者是「非敏感 cookie」但用途相反，不能混用
- CDP 抓真实请求头：点「退出登录」发出的 `POST /auth/logout` 确实带了
  `X-CSRF-Token`，**且值与 cookie 完全一致**，服务端返回 200
- **反向对照**：把 `csrf_token` 从 cookie 里删掉再点登出 → 前端不发这个头、
  服务端 **403**。这一条是整组里最重要的 —— 没有它，「带令牌能过」也可能只是
  因为后端根本没在校验

**变异验证（4 组全部转红，均已恢复）**：

| 变异 | 转红的用例 |
|---|---|
| 把 CSRF 校验改成恒真（`if false && ...`） | `TestCSRFDoubleSubmit` 的 9 个子用例 |
| **删掉 `authorized.Use(middleware.CSRF())` 这一行**（未接线） | 只有**实机冒烟**转红（18/21，恰好是那 3 条 CSRF 断言）——单测直接调 `middleware.CSRF()`，接线错了照样全绿。这类「接线」缺陷只能靠真请求发现 |
| 前端 `CSRF_METHODS` 改成空数组（不再带头） | `interceptor.spec.ts` 4 个用例 + **浏览器端 3 条断言**（含 403） |
| — | — |

> 第三条值得单独说：它在**单测**与**真浏览器**两侧同时转红，说明这组验证
> 不是「断言了实现细节」，而是真的覆盖了「头有没有发出去、服务端收不收」。

### B3 顺带修掉的既有缺陷：提前拒绝的响应会丢（`middleware.DrainBody`）

**这不是 B3 引入的，是 B3 的冒烟脚本把它撞出来的** —— 而且是个存在已久、
一直没被发现的传输层缺陷。`runtime/smoke/b3_smoke.py` 第一次跑到
「头与 cookie 不一致 → 403」时抛 `ConnectionResetError`，但服务端日志里
那条 403 好好地记着：**响应写出来了，客户端却没收到。**

**成因（已对照 Go 源码逐条核实）**：

1. 请求带 body 且被**提前拒绝**（401/403/429）—— 这些路径都在读 body 之前返回；
2. 调用方用 `Connection: close`；
3. net/http 的 `(*http.body).Close()` 有个**有顺序的 switch**：

   ```go
   case b.sawEOF:                  // 已读到 EOF → 空操作
   case b.hdr == nil && b.closing: // ← Connection: close → 直接跳过，不读
   case b.doEarlyClose:            // ← 读掉最多 maxPostHandlerReadBytes(256KB)
   default:                        // ← 全部读完
   ```

   `doEarlyClose` 对**每个**服务端请求都置为 true（`server.go`），
   也就是说 **keep-alive 的请求，handler 返回后 net/http 本来就会补读**；
   只有 `closing` 这一条分支被显式跳过（注释理由是 "no point in reading to EOF"）。
4. 于是关闭连接时接收缓冲区里还压着未读数据（或数据在关闭后才到达），
   内核发的不是 FIN 而是 **RST**；RST 会让对端内核**丢弃已收到但尚未被读走的
   数据** —— 包括刚写出去的那个 401/403。

**为什么必须修**：本项目自己的部署配置就会触发它 —— `deploy/nginx/default.conf`
设了 `proxy_http_version 1.1` 却**没有** `proxy_set_header Connection ""`，
nginx 因此对上游发 `Connection: close`（这正是「要开 upstream keepalive 就必须
显式清空 Connection 头」那条经典配置的由来）。后果直接砸在两个已完成的改造上：
B4 的「401 → 续期 → 重放」拿不到 401（nginx 报 502），B3 的 403 前端也看不到。
触发门槛还很低：只要**请求体比响应晚到**就行（慢速链路、body 稍大）。

**修法**：新增 `middleware.DrainBody`，注册在**最外层**（第一个），
在 `c.Next()` 之后把未读完的 body 补读掉（上限 256KB，与 net/http 同值）。
它**不是发明新行为，而是消除两条路径的差异** —— 让 `Connection: close` 的请求
也享受 keep-alive 请求本来就有的待遇。

几处刻意的取舍：

- **挂最外层而不是在每个 `c.Abort()` 旁边各写一遍**：那种写法迟早会漏，
  而漏掉的那一处只会表现为「偶发网络错误」，没人会联想到请求体
- **不判断 `c.IsAborted()`**：请求体是否被读完是传输层的事实，不该取决于
  某个中间件是否记得调用 Abort（handler 提前 return 而没 Abort 同样会留下未读 body）
- **有 256KB 上限**：这个函数运行在**尚未通过鉴权**的请求上，无上限地读完
  等于让匿名调用方用一个 `Content-Length` 就能让我们替他把数据收完
- **不加更短的读超时**：那会让「body 比响应晚到」的慢速客户端重新落回 RST，
  把要修的场景又修坏。补读的阻塞受 `server.read_timeout`（60s）约束，
  与 net/http 对 keep-alive 请求本就会做的事一致，**没有引入新的资源占用形态**

**验证**：

- `runtime/smoke/probe_csrf_reset.py` 四组对照实验（每组 12 次）：

  | 场景 | 修复前 | 修复后 |
  |---|---|---|
  | `Connection: close` + 有 body + 错头 | **4/12** 收到响应 | **12/12** |
  | `Connection: close` + 有 body + 无头 | **3/12** 收到响应 | **12/12** |
  | `Connection: close` + 无 body + 错头 | 12/12 | 12/12 |
  | keep-alive + 有 body + 错头 | 12/12 | 12/12 |

  中间两行是关键对照：去掉「未读 body」或去掉「服务端会关连接」任一变量，
  现象就消失 —— 这正是「成因是 RST 而非 CSRF 校验本身」的判据。
- `internal/middleware/drain_body_test.go` 6 条用例，`DrainBody` 覆盖率 100%；
  变异验证 4/4 转红。

**写用例时踩到的一个坑（值得记住）**：真连接用例的**初版是一条永远绿的假用例**。
它用 `http.Client` 循环 10 次，断言「总能收到 401」—— 在「把补读整个删掉」的
变异版本上照样 10/10 通过。原因有两层：

1. `http.Client` 一发出请求就立刻读响应，把 RST 与响应之间的竞速掩盖掉了；
2. 即使改用裸 socket，只要把请求头与 body **一次性写出去**，body 就会和请求头
   落在同一个 TCP 段里、被 net/http 读请求头时的 bufio 一并吞进用户态 ——
   关闭时内核接收缓冲区是空的，发的是 FIN 而不是 RST。

最终改成**显式制造「body 晚到」**：只写请求头 → 等 50ms 让服务端把响应写完并关闭
→ 再写 body → 等 300ms 让 RST 确定到达 → 最后才读。两次延迟把竞速变成确定性，
修复前稳定报 `connection reset`，修复后稳定拿到 401。

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
| B3 CSRF | B2 | ✅ 已完成 |
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
