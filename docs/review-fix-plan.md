# Gin-Admin 修复与完善计划

> 依据：2026-09-24 全量代码审查（综合评分 70/100）。
> 目标：P0+P1 完成后约 80 分，全部完成约 85–88 分。
> 节奏建议：P0 一次提交一个修复项，全部带回归测试；P1 涉及迁移，单独开分支。

---

## 阶段 P0 · 安全修复（1–2 人天，上线前必须）

### P0-1 角色提权：授权必须向上收敛 [High]
- **问题**：`internal/module/system/service/role_service.go:73-77, 133-137` 对 `req.MenuIds`
  直接落库并 `syncPolicies()`，没有校验操作者自己是否持有这些权限；
  `user_service.go:93-108 normalizeRoleIDs` 同样只校验"属于本租户"，不校验"我能否授予"。
  拥有 `system:role:add/edit` 的低权管理员可给自己的角色挂全量菜单 → 垂直提权到超管。
- **修法**（复用现有 Casbin 设施，不引入新依赖）：
  1. `middleware` 包新增导出函数 `OperatorCanGrant(c *gin.Context, menuIDs []uint) (bool, error)`：
     取操作者角色（复用 `resolveRoleCodes`）→ 加载目标 menu 的 `permission` 列表
     （`sys_menu` 按 ID 批查）→ 逐个 `currentEnforcer().Enforce(role, "default", perm, "*")`。
     任一权限不持有即 false。操作者含 `admin` 角色直接放行（与 `casbin.go:96` 通配策略一致）。
  2. `role_service.Create/Update` 与 `user_service.UpdateRoles`（给用户绑角色时，校验目标角色的
     权限集是操作者权限集的子集）在写库前调用，失败返回 `common.Forbidden`。
  3. 空 `MenuIds` / 仅目录型菜单（`permission == ""`）不拦截。
- **验收**：非 admin 账号创建角色勾选超出自身权限的菜单 → 403；admin 不受影响；
  新增单测覆盖"子集放行 / 超集拒绝 / admin 跳过"三种情况。

### P0-2 登录限频可被伪造 IP 绕过 [High]
- **问题**：`internal/module/system/controller/auth_controller.go:41` 用 `c.ClientIP()`，
  全仓无 `SetTrustedProxies`（gin 默认信任所有代理头）→ 每次伪造 `X-Forwarded-For`
  即绕过 IP 维度锁定；`login_guard.go:51-57` 在 Redis 故障时 fail-open，
  缓存抖动期暴力破解窗口完全敞开。
- **修法**：
  1. `config.go` 的 Server 段加 `trusted_proxies: []string`（默认空）；
     `router.Setup` 里 `r.SetTrustedProxies(cfg)` —— 空列表 = 不信任任何代理头，
     `ClientIP()` 直接用连接对端 IP，伪造失效；部署在 nginx 后时填内网网段。
  2. `login_guard` 加配置 `security.login_fail_closed: true`（默认 true）：
     Redis 查询失败时**拒绝本次登录**（返回"服务繁忙"），与 `middleware/auth.go`
     吊销检查的 fail-closed 策略对齐；确需可用性优先的环境可显式改 false，但告警日志保留。
- **验收**：连续伪造不同 `X-Forwarded-For` 登录失败，第 6 次仍被锁；
  单测覆盖 fail-closed 分支。

### P0-3 验证码自动化成本 [Medium]（修正：不能删 `chars`）✅ 已完成
- **核实结论**：`chars` 是"请依次点击 X Y Z"的提示语，前端
  `web/src/components/ClickCaptcha/index.vue:73,145,206` 依赖它渲染与计数，
  **属于设计而非答案泄漏**（坐标 `points` 仅存 Redis，未下发）。直接删除会破坏交互。
- **真正弱点**：脚本拿到 `chars` + `bg` 后可模板匹配免人工点选。
- **实施结果**：
  1. **生成接口按 IP 限流**：`cache.IncrWindow` 提供固定窗口计数原语，
     同 IP 生成 ≤ 10 次/分钟；计数失败按 fail-closed 拒绝
     （`Generate` 紧接着本来就要写 Redis，不引入新的不可用场景；
     反过来若放行，让计数失败即可无限刷图）。取不到 IP 时跳过限流而非共用空键。
  2. **一次验证一次提交**：核实后发现 `Verify` 原本已无条件 `Del`、登录侧
     `ConsumeVerifiedToken` 也已一次性消费，本条**无需改动**，仅补注释说明
     其目的（防止对同一张图穷举坐标，把 ±40px 的搜索空间摊薄成多次尝试）。
  3. **凭证绑定客户端来源**：验证码数据与通过凭证都记录生成时的客户端 IP，
     验证与登录两步都比对来源。**偏离原计划的一处**：原计划写的是
     "前端生成指纹、登录请求带上"，但那类指纹由客户端提供、可被随意伪造
     （打码平台只要把调用方的指纹原样回传即可），防不住它声称要防的转手使用；
     改用**服务端观测到的 IP**，不可伪造，且前端零改动。
     代价是移动网络切换出口时用户需重做验证码（概率低、重试即可），
     以及 NAT 下同出口的攻击者仍可复用 —— 已在注释中写明这条边界。
     旧格式凭证（无 `|` 分隔）跳过比对，避免升级瞬间让在途验证码全部失效。
- **验收**：同一 token 第二次使用失效；失败后不可重试；跨来源凭证被拒。
- **⚠️ 部署提示**：IP 维度的限流（本项与 P0-2）在反向代理之后会退化为全站共用额度，
  必须在 `server.trusted_proxies` 中填代理网段，否则 5 次登录失败即锁全站。


### P0-4 默认密钥护栏补强 [Low]
- 现状：`config.ValidateSecurity` 仅在 `mode=release` 拦截 —— 正确。
- 补充：`mode=debug` 且监听 `0.0.0.0` 时打印醒目告警（现只提示 Swagger 开启）；
  `.env.example` 加 `JWT_SECRET` 生成指引（`openssl rand -base64 48`）。

---

## 阶段 P1 · 多租户补全（3–4 人天，需要迁移脚本）

### P1-1 `sys_dept` 租户隔离 [Medium]
- **问题**：`model/dept.go` 用 `common.BaseModel`（无 `tenant_id`），
  `dept_repository.go:42` 全表查询 → 跨租户部门可枚举/改名/删除。
- **修法**：
  1. `SysDept` 换成 `common.TenantBaseModel`；迁移脚本
     `sql/migrations/2026-09-25-dept-tenant.sql`：`ALTER TABLE sys_dept ADD COLUMN tenant_id ...`
     + 回填（按 `sys_user.dept_id` 归属推断，推断不出的归平台租户 0，脚本里输出待人工确认清单）。
  2. `dept_repository` 所有查询包 `common.TenantScope`；`dept_service` 传入 tenantID。
  3. 加唯一索引 `(tenant_id, name)` 需先确认业务是否允许同名部门 —— 不允许则加，允许则跳过。
- **验收**：租户 A token 查 `/dept/tree` 只见本租户部门。

### P1-2 全局表语义显式化 [Medium]
- **现状**：`config`（含 OSS 密钥）、`dict`、`agreement` 三表无 `tenant_id`，
  任一租户管理员可经 `config_controller.go:123-148 BatchSave` 改写全平台 OSS 凭据。
- **修法**（按表分级，不一刀切加租户字段）：
  - `dict`：字典本应全局共享 → **保持全局**，但在 model 注释里写明"有意的平台级数据"。
  - `config`：拆两段 —— `sys_config` 保持全局但**写权限码收窄**为仅 `admin` 角色
    （`router.go` 的 permConfigEdit 校验加操作者角色判断），站点展示类 key 另开租户级表
    或按 `key` 前缀白名单放行租户编辑。
  - `agreement`：用户协议通常租户各有版本 → 加 `tenant_id`，迁移同 P1-1。
- **验收**：租户管理员修改 OSS 配置 → 403；字典下拉全租户可用。

### P1-3 `GetTenantID` 返回 0 的静默旁路 [Medium]
- **问题**：`common/context.go:22-29` 取不到租户返回 0，而 `TenantScope(db,0)` 不过滤
  → 任何导致上下文丢失的路径都变成全表访问。
- **修法**：`middleware.Auth()` 解析出的 JWT 若 `tenantID==0` **且**账号不是平台级
  （在 `sys_user` 加 `is_platform` 标记，或约定 `tenant_id=0` 即平台账号并强制显式声明），
  直接 401 拒绝；`TenantScope` 语义保留（内部回调仍需用），但从"默认行为"变为
  "必须显式传 0 才不过滤"，并加注释 + 单测固化该语义。
- **验收**：删掉/伪造上下文的请求无法触发无租户过滤查询（加一条集成测试断言）。

### P1-4 LIKE 通配符转义 [Low]
- 全仓 18 处 `"%"+input+"%"` 未转义 `%`/`_` → 可传 `%%%` 拉宽匹配探测数据。
- **修法**：`common` 加 `EscapeLike(s string) string`（转义后建议配合 `ESCAPE '\\'`），
  18 处统一替换；加单测。

---

## 阶段 P2 · 工程质量（4–6 人天，可与业务并行）

### P2-1 测试补齐（当前最大短板）
- controller 层 3.1%、upload 14.3%、payment 24.6%、middleware 21.1%。
- 目标：核心路径（auth 流程、casbin 拒绝、租户过滤、支付回调验签）补
  **httptest 集成测试**（`internal/testsupport` 的 sqlite 基建已就绪，
  `main_test.go` 模式可复制），目标全仓覆盖率 41.8% → 60%+。
- 优先级：`router` 包（0 测试，但它是权限登记的中枢）> payment notify > upload > auth。

### P2-2 golangci-lint 转阻断
- `ci.yml:76-89` 现为 `continue-on-error: true`，注释已写明"基线清理干净后去掉"。
- 动作：本地跑一轮 golangci-lint → 修掉/豁免现有告警 → 删 `continue-on-error`。

### P2-3 消除样板与忽略错误
- `_ =` 忽略错误 8 处 → 逐一评估，至少降为日志 + 注释说明为何可忽略。
- 后端 14 处 `ShouldBindQuery` + 分页 3 种写法（`NormalizePageSize`/`NormalizePage`/`GetPageParams`）
  → 统一为一个 `common.BindPage(c, &req)`；前端 13 份 CRUD 样板 → 抽
  `useCrud` hook（`loadData/handleAdd/handleEdit/handleDelete/pagination` 收口）。
- 前端 `Result`/`PageResult` 在 `api/index.ts` 与 `types/api.ts` 双份定义 → 删一处。

### P2-4 前端工程设施
- 引入 ESLint（`eslint-plugin-vue` + `@typescript-eslint`）+ Prettier，先以
  warning 级别落地一轮再收紧 —— 与后端 lint 同样策略，避免 CI 长红。
- 目前前端**零测试**：至少给 `useDict`、`api` 拦截器、`permission.generateRoutes`
  三个纯逻辑补 vitest 用例（不需要组件测试）。

---

## 阶段 P3 · 体验与长期（5–8 人天，按优先级排期）

### P3-1 移动端响应式（此前已反馈未完成）
- 现状：全仓仅 4 条 `@media`，`useResponsive` 只覆盖布局壳，23 个视图无断点；
  `MobileAction` 仅 user 页启用；`index.scss:300` 的弹窗 92% 宽已修。
- 动作：以 `views/system` 5 页 + `member` + `payment` 为第一批，补
  表格→卡片式折叠（<768px）、筛选区抽屉化、`MobileAction` 推广到所有带操作列的表格；
  每页在 375px 宽实测（用 CDP Emulation 方式，见工作日志）。

### P3-2 路由与鉴权收尾（前端）
- `router/index.ts:51-53`：`roles` 非空即放行 → 补"目标 path 必须命中 accessRoutes
  或白名单"的显式校验；补 `pathMatch(.*)` 404 catch-all（现在未匹配路由仅控制台告警）。
- token 从 `js-cookie` 迁到 httpOnly cookie：需后端 `Set-Cookie` 配合 + CSRF 防护
  （SameSite=Strict 已够用，Bearer 头方案可并存过渡）。改造面较大，单独立项。

### P3-3 清理项
- 6 个孤儿组件（SvgIcon/TableSkeleton/PageHeader/RightPanel/Upload，0 引用）
  → 删除或接入使用；`DictTag`/`MobileAction` 各只 1 处用 → 推广或删。
- `docs/docs.go`（2413 行生成物）入库 → 加 `.gitignore` + CI 里 `swag init --diff` 校验一致性。
- `views/system/post/index.vue:105` handleDelete 无 try/catch → 补。
- 11 处空 catch 吞错 → 至少 `console.warn` + 面向用户提示。

---

## 里程碑与预期分数

| 节点 | 完成项 | 预期分数 |
|---|---|---|
| 当前 | — | **70** |
| T+2 天 | P0 全部 | **78** |
| T+1 周 | P0+P1（含迁移） | **83** |
| T+2 周 | +P2（lint 阻断、覆盖率 60%+、useCrud、eslint） | **86** |
| 持续 | P3 | **88** |

**每项的验收方式统一为**：新增回归测试 + `make check` 全绿（与 CI 等价）。
P1 迁移脚本必须在全新空库上完整跑一遍 `init.sql` + 全部 migrations 验证幂等。
