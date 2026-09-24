# Gin-Admin 修复与完善计划

> 依据：2026-09-24 全量代码审查（综合评分 70/100）。
> 目标：P0+P1 完成后约 80 分，全部完成约 85–88 分。
> 节奏建议：P0 一次提交一个修复项，全部带回归测试；P1 涉及迁移，单独开分支。

## 执行进度

| 阶段 | 状态 | 说明 |
|---|---|---|
| P0-1 角色提权 | ✅ 已完成 | `791c85c` 授权向上收敛 + 保留编码护栏，新增 9 个用例 |
| P0-2 限频绕过 | ✅ 已完成 | `bc0676a` trusted_proxies + fail-closed，router 包从 0 测试到 3 个 |
| P0-3 验证码 | ✅ 已完成 | `9b00513` 生成限流 + 凭证绑定来源；2 条原计划项经核实已存在或改法 |
| P0-4 密钥护栏 | ✅ 已完成 | 开发模式启动告警逐条打印 + `server.host` + 部署配置补白名单 |
| P1-4 LIKE 转义 | ✅ 已完成 | `8c45aa4` 18 处统一转义 |
| P1-3 租户 0 旁路 | ✅ 已完成 | `0e7d72d` 平台级身份需持有 admin 角色 |
| P1-1 dept 租户隔离 | ✅ 已完成 | 模型/仓储/服务/控制器 + 迁移（两步回填）+ BuildTreeForest |
| P1-2 全局表语义 | ✅ 已完成 | dict 显式声明全局；config 写权限收窄为 admin；agreement 加 tenant_id |
| P2-1 测试补齐 | 🚧 进行中 | 49.2% → **56.9%**（目标 60%）；已补 payment/upload/middleware/controller 三批 |
| P2-2 lint 转阻断 | ✅ 已完成 | 基线 29 → **0**，`e15142b`；顺带修掉「观察项本身失效」 |
| P2-3 消除样板 | 🚧 进行中 | 后端已完成（BindPage 收口 + 忽略错误）；前端 useCrud 与类型去重未做 |
| P2-4 前端工程 | ⏳ 未开始 | |

**⚠️ 升级须知（P0-2/P0-3 带来的部署影响）**

IP 维度的限流（登录失败 5 次/15 分钟、验证码生成 10 次/分钟）现在按
`c.ClientIP()` 计数，而该值默认**只取连接对端地址**。因此：

- **裸机直连部署**：无需改动，行为正确。
- **反向代理之后**（nginx / SLB / 容器编排）：**必须**在 `server.trusted_proxies`
  填入代理网段。漏配的后果不是"限流失效"而是"限流过度" —— 所有用户共用代理
  的一个 IP，任意 5 次登录失败会锁死全站 15 分钟。
  `deploy/config.docker.yaml` 已按 `172.16.0.0/12` 预置（见该文件注释）。
- 启动日志会明确打印当前 IP 计数依据，可用于确认配置是否生效。

---

## 阶段 P0 · 安全修复（1–2 人天，上线前必须）

### P0-1 角色提权：授权必须向上收敛 [High] ✅ 已完成
- **问题**：`internal/module/system/service/role_service.go:73-77, 133-137` 对 `req.MenuIds`
  直接落库并 `syncPolicies()`，没有校验操作者自己是否持有这些权限；
  `user_service.go:93-108 normalizeRoleIDs` 同样只校验"属于本租户"，不校验"我能否授予"。
  拥有 `system:role:add/edit` 的低权管理员可给自己的角色挂全量菜单 → 垂直提权到超管。
- **实施结果**：
  1. `middleware` 新增 `RoleCodesFor` / `HasAdminRole` / `OperatorHoldsPermissions`。
     **偏离原计划的一处**：原计划签名是 `OperatorCanGrant(c *gin.Context, ...)`，
     但 Service 层不允许触碰 `gin.Context`（AGENTS.md 规则 2），而该判定在
     Controller 与 Service 两侧都要用，故改为只依赖 `(tenantID, userID)` 两个标量，
     由调用方各自从上下文取出。顺带把 `resolveRoleCodes` 重构到它之上，
     两侧共用同一份角色缓存，不增加查库开销。
  2. `common.NewForbiddenError` 让授权失败以 403 语义返回（而非 400）。
  3. 校验一律放在**落库之前**：`role_service.Create/Update` 校验菜单权限码是操作者
     权限的子集；`user_service.normalizeRoleIDs` 覆盖"建用户绑角色/改用户换角色/
     更新用户角色"三条路径。`user_service.Update` 的角色校验也从"落库之后"提到之前，
     避免「资料已改、角色被拒」的半成品状态。
  4. 额外补上原计划未列出的**保留编码护栏**：admin 的通配策略只依赖角色编码、
     与名下菜单无关，因此非 admin 操作者不得新建 code=admin 的角色、不得把角色改名
     为 admin、也不得改动 admin 角色本身（改名等于把通配权限交给新编码）。
     同理，授予 admin 角色按**编码**拦截而非比对菜单权限集。
  5. 空 `MenuIds` / 仅目录型菜单（`permission == ""`）不拦截。
- **验收**：`internal/module/system/service/role_grant_test.go`（9 个用例）覆盖
  「子集放行 / 超集拒绝且不落库 / 目录型菜单放行 / admin 操作者放行 /
  保留编码三条染指路径 / 给用户绑 admin 角色被拒」。提交 `791c85c`。

### P0-2 登录限频可被伪造 IP 绕过 [High] ✅ 已完成
- **问题**：`internal/module/system/controller/auth_controller.go:41` 用 `c.ClientIP()`，
  全仓无 `SetTrustedProxies`（gin 默认信任所有代理头）→ 每次伪造 `X-Forwarded-For`
  即绕过 IP 维度锁定；`login_guard.go:51-57` 在 Redis 故障时 fail-open，
  缓存抖动期暴力破解窗口完全敞开。
- **实施结果**：
  1. `config.ServerConfig` 新增 `trusted_proxies`（默认空 = 不信任任何代理头），
     `router.Setup` 在注册路由前调用 `r.SetTrustedProxies`；
     `config.Validate` 增加白名单校验并显式拒绝 `0.0.0.0/0`、`::/0`
     （gin 解析失败会保留上一份配置，"配错了比不配更危险"，故必须启动期拦下）。
  2. 新增 `security.login_fail_closed`（`*bool`，默认 true）：Redis 不可用时拒绝登录。
     用指针是因为 Go 的 bool 零值恰好等于不安全的那一侧，漏配必须落到安全侧。
     失败计数被写成非整数时按"已达上限"处理。
  3. 核实后 `auth_controller` 的业务逻辑早已下沉到 Service（本次只需传参改造），
     故原计划"Controller 承载业务逻辑"这部分不在本项范围内。
- **验收**：`router/router_test.go`（3 个用例，router 包此前 0 测试）用真实路由验证
  「默认忽略代理头 / 可信代理的头被采信 / 非可信对端的头被忽略」；
  已验证移除 `SetTrustedProxies` 后前两个用例立刻转红。
  另加 `config` 白名单用例与 `service` 的 fail-closed / fail-open 用例。提交 `bc0676a`。

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


### P0-4 默认密钥护栏补强 [Low] ✅ 已完成
- 现状：`config.ValidateSecurity` 仅在 `mode=release` 拦截 —— 正确，保持不变。
- **实施结果**：
  1. 新增 `config.DevelopmentWarnings()`：开发模式逐条打印风险点（调试入口开放、
     监听所有网卡、JWT 密钥仍为默认值、库密码仍为默认值），
     密钥那条同时给出生成方式（`openssl rand -base64 48`）—— 只报风险不给出路的
     告警等于没有告警。生产环境返回空（由 `ValidateSecurity` 直接拒绝启动）。
  2. 新增 `server.host`（留空 = 所有网卡）：让"开发环境只在本机可达"成为可配置项，
     否则第 1 条的告警没有可执行的动作。同时新增 `ServerConfig.ListenAddr()` /
     `ListensOnAllInterfaces()`，`cmd/server` 改用前者拼监听地址。
  3. 启动日志明确打印 IP 计数依据（对端地址 / 可信代理转发），
     用于确认 `trusted_proxies` 是否按预期生效 —— 这是 P0-2/P0-3 唯一的部署陷阱。
  4. `.env.example` 补 `openssl rand -base64 48` 生成方式，并说明 debug 模式下
     默认密钥只会告警不会拒绝启动。
  5. `deploy/config.docker.yaml` 预置 `trusted_proxies: 172.16.0.0/12`：
     该部署**确定**在 nginx 之后（`proxy_pass http://app:8080`），
     漏配会让限流退化为全站共用额度。
- **验收**：`config/config_test.go` 覆盖监听地址两种写法、生产环境不产生开发告警、
  默认密钥/默认库密码必须被点名、生成方式必须出现。`deploy/validate.py` 可复验配置。

---

## 阶段 P1 · 多租户补全（3–4 人天，需要迁移脚本）

### P1-1 `sys_dept` 租户隔离 [Medium] ✅ 已完成
- **问题**：`model/dept.go` 用 `common.BaseModel`（无 `tenant_id`），
  `dept_repository.go:42` 全表查询 → 跨租户部门可枚举/改名/删除。
- **实施结果**：
  1. `SysDept` 改继承 `common.TenantBaseModel`；`DeptRepository` 全部方法加
     `tenantID` 参数并施加 `TenantScope`；`DeptService` / `DeptController` 逐层透传。
     `Update`/`Delete` 用条件更新（`RowsAffected == 0` → `ErrRecordNotFound`），
     仓储层不再假设调用方一定先查过。
  2. 迁移 `sql/migrations/2026-09-25-dept-tenant.sql`：加列 + 加索引 + **两步回填**。
     回填分两步是必需的，不是保险起见：
     · 第一步按用户归属推断（该部门下的在职用户全部属于同一个非 0 租户才推断，
       多租户混用或只有平台账号的部门不推断）；
     · 第二步沿部门树**向下**传播（父部门已定租户的，子部门一并归入）。
     只做第一步会让子部门的父节点不在本租户结果集里 —— 整棵子树不可达。
     刻意不向上推断：父部门若仍是平台级，说明它可能是多租户共用的上级，不该划给某个租户。
  3. `init.sql` 同步加 `tenant_id` 与 `idx_tenant_id`（否则新装库与迁移库结构不一致）。
  4. **计划外补充 A**：新增 `common.BuildTreeForest`，部门树改用它。
     按租户过滤后「父节点不在结果集里」是**合法状态**（历史父部门仍留在平台级），
     沿用 `BuildTree` 会返回**空数组** → 部门管理页一片空白、新建用户选不到部门，
     而数据完好。把这类节点提升为顶层节点，层级关系不丢。菜单仍用 `BuildTree`
     （全局表，不存在这种合法孤儿）。
  5. **计划外补充 B**：`sys_user.dept_id` 的租户归属校验（`userService.normalizeDeptID`），
     覆盖「建用户 / 改用户 / 调整部门」三条路径。不校验时租户 A 可把用户的部门指向
     租户 B 的部门 ID，而后果比跨租户角色更隐蔽：用户列表按租户过滤、部门树也按租户过滤，
     该用户在界面上表现为「没有部门」，不报任何错。
  6. **未做（有意）**：不加 `(tenant_id, name)` 唯一索引。`sys_dept` 原本在 `name` 上
     就没有唯一约束，业务允许同名部门（集团下多个子公司都有「市场部」）；
     加约束会让现有数据的插入突然失败，属于超出本次修复范围的行为变更。
- **验收**：迁移在全新空库上跑通 `init.sql` + 全部 migrations，并**重复执行 3 次**
  确认幂等；用双租户数据验证回填（dept2→租户1、dept3→租户2、无用户的子部门跟随父部门→租户1、
  只有平台账号的 dept1 保持 0），各租户视角互不可见。
  开发库已备份 `runtime/backup-sys_dept-before-p1.sql` 后执行迁移，admin 部门树正常（3 个、层级完整）。
  测试：仓储层 5 组跨租户用例、Service 层租户透传 + 部门树集成用例、
  Controller 层 4 个 httptest 用例（含「新建部门落在操作者租户下」）。
  三处均已做「移除修复即转红」的验证。

### P1-2 全局表语义显式化 [Medium] ✅ 已完成
- **现状**：`config`（含 OSS 密钥）、`dict`、`agreement` 三表无 `tenant_id`，
  任一租户管理员可经 `config_controller.go:123-148 BatchSave` 改写全平台 OSS 凭据。
- **实施结果**（按表分级，不一刀切加租户字段）：
  - `dict`：**保持全局**，在 `model/dict.go` 写明「有意的平台级数据」及理由
    （字典类型编码是代码里的字面量，按租户各存一份会让字面量失去确定性），
    并记录残留风险（持有 dict:add/edit 的角色改的是所有租户共用的文案；
    默认只有 admin 持有，若某部署要下放需另行评估）。
  - `config`：保持全局（承载平台级凭据，按租户复制一份在业务上不成立），
    **写权限收窄为 admin**：新增 `middleware.RequireAdminRole`，路由侧新增
    `protectedAdmin`，应用于 POST/PUT/DELETE `/config`、`PUT /config/batch`、
    `POST /config/upload` 五个写接口。读接口不限制（敏感项的值已在 Service 层打码）。
    **偏离原计划一处**：原计划写的是「站点展示类 key 另开租户级表或按 key 前缀白名单
    放行租户编辑」—— 那是新增功能而非修复，且需要产品决策（哪些 key 算展示类），
    本次不做；已在 `model/config.go` 注明正确做法是另开租户级表，而不是给本表加 tenant_id。
  - `agreement`：加 `tenant_id`（协议本就是「每个租户各有版本」的数据），
    迁移 `sql/migrations/2026-09-25-agreement-tenant.sql`。
    **与 dept 不同，协议不做回填**：表里没有任何归属线索（不指向用户，内容也推断不出
    租户），历史协议一律保留 0 = 平台级，由人工按脚本末尾的清单确认。
- **验收**（真实 HTTP，端到端）：
  - 租户管理员（tenant=1，持有 `config:list` + `config:add`）：读配置 200、
    写配置 **403「该操作仅限超级管理员」**，且越权写入未落库（`sys_config` 0 行）；
  - 平台 admin：读 200、写 200（合法路径未被破坏）；
  - 字典下拉 `GET /dict/data/type/sys_user_status` 对租户管理员 200（全租户可用）。
  - 迁移在全新空库上跑通 `init.sql` + 全部 migrations 并重复执行 2 轮确认幂等；
    开发库已备份 `runtime/backup-sys_agreement-before-p1.sql` 后执行。
  - 测试：`middleware` 新增 5 组 `RequireAdminRole` 用例（含「角色解析失败必须拒绝」）、
    仓储层 4 组协议跨租户用例（已验证「移除 TenantScope 即转红」）。

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

### P2-1 测试补齐（当前最大短板）🚧 进行中
- 起点：controller 层 3.1%、upload 14.3%、payment 24.6%、middleware 21.1%。
- **进度**：全仓平均包覆盖率 49.2% → **54.9%**（18 个包，口径为各包覆盖率求平均）。
  已完成两批（提交 `4c18f82`、`6fcfa8e`）：
  - `payment/service` 24.6% → 47.7%：回调验签全链路（测试期生成 RSA 密钥对自签自验）、
    returnURL 开放重定向防护、doRequest 对非 2xx 的处理、pay.* 配置键名映射
  - `pkg/upload` 14.3% → 52.4%：本地存储的**写盘路径**（此前一次都没跑过，
    而它又是默认后端）、Init 的回退逻辑、调度器校验顺序
  - `middleware` 25.7% → 61.8%：Casbin 适配器与策略生成、CasbinAuth 六组判定、
    OperatorHoldsPermissions 直接判定
  - `system/controller` 6.2% → 10.9%：user/role 控制器的上下文透传
- **顺带修掉一个真 bug**：支付宝 `gmt_payment` 是 GMT+8，代码用 `time.Parse`
  按 UTC 解析 → 支付时间差 8 小时（界面显示「支付时间在未来」）。
  改用 `time.ParseInLocation` + 固定 GMT+8。微信侧无此问题（RFC3339 自带偏移）。
- **有意划的边界**：云存储后端（aliyun/tencent/minio）与真实出网的网关方法
  （Prepay/Refund/QueryTrade）需要真实凭据与网络，不进单测 ——
  不为覆盖率数字去 mock 掉整个网络层。
- **剩余待补**（按缺口排序）：`system/controller` 10.9%、`system/repository` 34.3%、
  `system/service` 38.8%、`member/repository` 38.1%、`pkg/task` 31.2%、
  `internal/database` 28.6%、`cmd/migrate` 50%。
  其中 controller 层缺口最大（约 12 个控制器只测了 3 个），是达到 60% 的关键。

### P2-2 golangci-lint 转阻断 ✅ 已完成
- **先发现了一个被掩盖的问题**：CI 用 `golangci-lint-action@v6` + `version: latest`，
  而 latest 解析到 **v1.64.8**（用 go1.24 构建）；本仓库 go.mod 要求 go 1.25，
  v1 会在**加载阶段**直接失败。也就是说这个任务一直没在检查代码，
  而 `continue-on-error: true` 让它连失败都不显眼 ——
  **观察项在观察项失效时是最危险的组合**。
- **实施结果**：本地改用 v2（v2.13.2）跑通，真实基线 **29 项**
  （errcheck 24 / staticcheck 4 / unused 1）→ 逐项修掉或豁免 → 现在 **0 项**。
  - 新增 `.golangci.yml`：显式 `default: standard`，并写明为什么**不开** gofmt
    （Windows 的 core.autocrlf 会检出 CRLF，而 gofmt 把 CRLF 视为格式错误，
    开了之后每个 Windows 开发者本地 lint 都会红；实测确认转 LF 后零抱怨）。
  - `ci.yml`：删掉 `continue-on-error`，action 升到 v8（v7 起支持 lint v2），
    版本从 `latest` 改为**钉死 v2.13.2**（版本漂移正是那个坑的成因）。
  - `Makefile` 的 `lint` 目标补上 golangci-lint 并给出缺失时的安装指引，
    否则「make check 等价于 CI」会在本地悄悄失效。
- **修的不只是 `_ =`**：其中 8 处是真的吞掉了有后果的错误，已改为正确处理，
  详见提交 `e15142b` 的信息（member 查重把 DB 故障当成「未注册」、
  验证码作废失败导致 token 可重复提交、`png.Encode` 失败返回空白图、
  `payCtx()` 写好了却从未被调用等）。

### P2-3 消除样板与忽略错误 🚧 后端已完成
- ✅ `_ =` 忽略错误：逐处评估完毕。其中 8 处是真的吞掉了有后果的错误（已改为正确处理，
  见 P2-2 的说明），其余是 `defer x.Close()` / `logger.Sync()` 这类无处上报的，
  显式写成 `_ =` 表明是有意忽略。
- ✅ 后端分页收口：**核实后发现「3 种写法」早已统一**（都走 `NormalizePageParams`，
  旧的 `NormalizePageSize`/`GetPageParams` 已不存在），真正的缺口是别的东西：
  14 处 `ShouldBindQuery` 里有 **7 处直接丢掉绑定错误**（`?page=abc` 不报错，
  被当成「没传」按默认分页返回），另有一处曾把 page/pageSize 硬编码成 1/10。
  已新增 `common.PageQuery` + `common.Paged` + `common.BindPage(c, req)`，
  14 处全部收口（提交 `658a41d`）。
- ⏳ 前端 13 份 CRUD 样板 → 抽 `useCrud` hook（`loadData/handleAdd/handleEdit/handleDelete/pagination`）。
- ⏳ 前端 `Result`/`PageResult` 在 `api/index.ts` 与 `types/api.ts` 双份定义 → 删一处。

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
