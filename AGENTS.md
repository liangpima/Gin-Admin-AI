# AGENTS.md — Gin-Admin 项目开发规范

## 项目概述

Gin-Admin 是一套基于 Go + Gin + GORM + Vue3 + Element Plus 的后台管理框架。
本文件定义了 AI Agent 在本项目中生成代码时必须遵守的规则和规范。

## 核心架构

采用 **Controller → Service → Repository** 三层架构。

```
Controller (接口层)
  ├── 参数接收 (ShouldBindJSON/ShouldBindQuery)
  ├── 参数校验 (binding tag)
  └── 返回统一 Response

Service (业务层)
  ├── 业务逻辑
  ├── 事务控制 (db.Transaction)
  └── 调用本模块 Repository

Repository (数据层)
  ├── 数据库操作 (GORM)
  └── 只做数据访问，不含业务逻辑
```

## 项目技术栈

- **后端**: Go 1.25, Gin v1.10, GORM v1.25, MySQL 5.7+, Redis 3.0+
- **认证**: JWT (golang-jwt/v5) + Casbin RBAC
- **前端**: Vue 3.5, Element Plus 2.9, Vite 6, TypeScript 5.7, Pinia 2.3
- **其他**: Zap 日志, Viper 配置, Swagger 文档, 1Password/robfig/cron

## 目录结构

```
go-admin/
├── cmd/server/main.go              # 唯一入口
├── config/
│   ├── config.yaml                 # 应用配置
│   ├── config.go                   # 配置加载
│   └── casbin/model.conf           # RBAC 模型
├── internal/
│   ├── cache/redis.go              # Redis 封装（可选）
│   ├── common/                     # 统一响应/错误码/业务错误语义/模型/分页/软删除唯一值释放
│   ├── database/mysql.go           # MySQL 连接
│   ├── logger/zap.go               # Zap 日志
│   ├── middleware/                 # 中间件
│   │   ├── casbin.go               # RBAC 鉴权 + 角色菜单策略同步
│   │   ├── casbin_adapter.go       # Casbin 策略的 GORM 适配器
│   │   └── permission.go           # 路由权限登记表（见规则13）
│   └── module/
│       ├── system/                 # 系统管理（用户/角色/菜单/部门/岗位/配置/字典/日志/文件/协议）
│       ├── payment/                # 支付模块
│       ├── member/                 # 会员模块（会员/等级/标签/积分）
│       ├── captcha/                # 验证码
│       └── monitor/                # 监控（占位，未实现）
├── pkg/
│   ├── auth/jwt.go                 # JWT 工具
│   ├── upload/                     # 多端文件上传（本地/OSS/COS/MinIO）
│   │   ├── upload.go               # 上传入口（自动选择存储方式 + 扩展名校验）
│   │   ├── local.go                # 本地存储
│   │   ├── aliyun_oss.go           # 阿里云 OSS
│   │   ├── tencent_cos.go          # 腾讯云 COS
│   │   └── minio.go                # MinIO
│   ├── excel/excel.go              # Excel 导入导出
│   ├── task/cron.go                # 定时任务调度
│   └── utils/                      # Hash/Snowflake/字符串工具
├── router/router.go               # 路由注册（含 /uploads 静态服务）
├── sql/                            # 数据库脚本
├── docs/                           # Swagger 文档
├── web/                            # 前端 (Vue3)
│   └── src/
│       ├── api/                    # API 接口定义（16个）
│       ├── components/             # 公共组件（10个）
│       ├── hooks/                  # useResponsive, useTheme
│       ├── layout/                 # 布局组件
│       ├── router/                 # 路由配置
│       ├── store/modules/          # app/permission/tagsView/user
│       ├── utils/                  # auth/format/request
│       └── views/                  # 页面（7个目录）
├── Makefile
├── start-all.ps1                   # 一键启动脚本
└── AGENTS.md
```

## 开发规则（13条铁律）

### 规则1: Controller 只负责参数接收与返回

- **允许**: 参数接收 (`ShouldBindJSON`/`ShouldBindQuery`)、参数校验 (`binding` tag)、返回结果 (`common.Success`/`common.Error`)
- **禁止**: 在 Controller 中编写业务逻辑、直接操作数据库、调用 Repository

### 规则2: Service 负责业务逻辑与事务控制

- **允许**: 业务判断、数据组装、事务管理 (`db.Transaction`)、调用本模块 Repository
- **禁止**: 直接返回 HTTP 响应、操作 `gin.Context`、引入 `net/http` 相关依赖

### 规则3: Repository 只负责数据库操作

- **允许**: GORM 查询、CRUD 操作、SQL 构建
- **禁止**: 业务逻辑判断、跨表关联查询（应通过 Service 组合）、返回 HTTP 响应

### 规则4: 禁止跨模块访问 Repository

每个 Service 只能访问自己模块的 Repository。

```
✅ 允许:
UserService  → UserRepository
RoleService  → RoleRepository

❌ 禁止:
PaymentService → UserRepository   (跨模块)
OrderService   → UserRepository   (跨模块)
```

如需跨模块数据，通过调用对应 Service 实现。

### 规则5: 所有接口必须返回统一 Response 结构

```go
// 成功
common.Success(c, data)
common.SuccessWithPage(c, list, total, page, pageSize)

// 失败 —— Service 返回的 error 一律用 FailWith，不要手写业务码
common.FailWith(c, err)

// 参数绑定失败等 Controller 自己产生的错误，直接用 Error
common.Error(c, common.CodeBadRequest, err.Error())
common.Unauthorized(c, "未登录")
common.Forbidden(c, "无权限")
```

禁止直接使用 `c.JSON()` 返回业务数据。

#### 错误语义：业务错误 vs 系统错误（必须遵守）

**`common.FailWith(c, err)` 是 Service 错误的唯一出口**，它按错误类型自动选择业务码：

| 错误来源 | 构造方式 | 对外业务码 | 文案 |
|----------|----------|-----------|------|
| 业务校验失败（重名、状态冲突、密码太弱…） | `common.NewBizError("...")` | 400 | 原文透出 |
| 操作目标不存在 | `common.NewNotFoundError("...")` | 404 | 原文透出 |
| DB / IO / 第三方失败 | 原样 `return err` | 500 | **通用文案**，真实错误只进日志 |

```go
// ✅ Service：业务错误显式标记，系统错误原样返回
if s.repo.CountByCode(tenantID, req.Code, 0) > 0 {
    return common.NewBizError("角色编码已存在")
}
user, err := s.repo.FindByID(tenantID, id)
if err != nil {
    return common.NotFoundOrErr(err, "用户不存在")   // gorm.ErrRecordNotFound → 404
}
return err                                        // 其余 → 500

// ✅ Controller：一行收口
if err := ctl.userService.Create(tenantID, &req, operatorID); err != nil {
    common.FailWith(c, err)
    return
}

// ❌ 禁止：把所有错误都写成同一个码
common.Error(c, common.CodeInternalError, err.Error())   // DB 故障与重名混为一谈
common.Error(c, common.CodeBadRequest, err.Error())      // DB 故障被说成「参数错误」
```

要点：

- **单条查询必须用 `common.NotFoundOrErr(err, "XX不存在")`**。GORM 查不到记录返回的是
  `gorm.ErrRecordNotFound`，不转换就会变成 500，用户完全不知道是自己传的 ID 不对
- Service 内部**复用已包装的方法**，不要绕过它直连 repository。例如 `CloseOrder` 应调
  `s.GetOrder()` 而非 `s.orderRepo.FindByOrderNo()`，否则 404 语义会丢
- 系统错误对外统一返回「服务器内部错误」，**不要把原始 error 回给调用方** ——
  里面常带 SQL、表名字段、内部路径，属于实现细节泄漏；排查所需信息日志里已有
- 新增业务错误时，先判断它属于上表哪一行，不要图省事直接 `errors.New`

### 规则6: 所有表必须包含基础字段

```go
type BaseModel struct {
    ID        uint           `gorm:"primarykey" json:"id"`
    CreateBy  uint           `gorm:"comment:创建者ID" json:"createBy"`
    UpdateBy  uint           `gorm:"comment:更新者ID" json:"updateBy"`
    CreatedAt time.Time      `gorm:"comment:创建时间" json:"createdAt"`
    UpdatedAt time.Time      `gorm:"comment:更新时间" json:"updatedAt"`
    DeletedAt gorm.DeletedAt `gorm:"index;comment:删除时间" json:"-"`
    Remark    string         `gorm:"type:varchar(500);comment:备注" json:"remark"`
}
```

多租户表继承 `TenantBaseModel`（额外包含 `TenantID`）。

### 规则7: 多租户必须自动附加 tenant_id

- 使用 `common.TenantScope(db, tenantID)` 在 Repository 层过滤租户数据
- 禁止无条件查询全表
- **Repository 方法必须接收 `tenantID uint` 参数**（从签名层面强制，避免漏传）
- Controller 从 JWT claims 中提取 tenantID，透传给 Service → Repository
- 支付回调等外部接口可使用 `FindByXxxForNotify()`（无 tenant 过滤）

**必须按租户过滤的表**（表含 `tenant_id` 列；多数继承 `TenantBaseModel`，
`pay_order` / `sys_operation_log` / `sys_login_log` 为手动声明 `TenantID` 字段）：

`sys_user`、`sys_role`、`sys_post`、`sys_dept`、`sys_agreement`、`sys_file`、`sys_operation_log`、`sys_login_log`、`pay_order`、`pay_member`、`pay_member_level`、`pay_member_tag`、`pay_points_log`

**全局表**（继承 `BaseModel`，**不要**加租户过滤，否则会因无 `tenant_id` 列而 SQL 报错）：

`sys_menu`、`sys_config`、`sys_dict_type`、`sys_dict_data`、`sys_tenant`

> ⚠️ **`sys_dept` 与 `sys_agreement` 已改为租户内表**（见
> `sql/migrations/2026-09-25-dept-tenant.sql` 与 `2026-09-25-agreement-tenant.sql`）。
> 它们此前被列在「全局表」清单里，而这份过期描述已经造成过一处真实缺陷：
> 仪表盘 `dashboardRepository.GetStats` 按全局口径统计部门数，导致**每个租户
> 看到的都是全平台部门数量**（既不准确，也泄漏平台规模）。
> 新增/修改涉及租户的查询前，请**核对模型是否继承 `TenantBaseModel`
> 以及表里是否真有 `tenant_id` 列**，不要照抄清单。

**纯关联表**（**没有** `tenant_id` 列，同样不能套 `TenantScope`）：

`sys_user_role`、`sys_user_post`、`sys_role_menu`、`pay_member_tag_rel`

关联表的租户隔离必须在 Service 层自己校验：写入前用租户维度查出被引用方
（角色/岗位/会员），比对数量是否一致，不一致即拒绝。
只查关联表本身无法判断归属 —— 它的 user_id 与 role_id 可能分属不同租户。

```go
// ✅ Service 层校验归属后再写关联
roleIDs, err := s.normalizeRoleIDs(tenantID, req.RoleIds)   // 按租户过滤 + 比对数量
if err != nil {
    return err
}
return s.userRepo.ReplaceRoles(user.ID, roleIDs)

// ❌ 直接把前端传来的 ID 写进关联表 → 跨租户权限提升
return s.userRepo.ReplaceRoles(user.ID, req.RoleIds)
```

⚠️ **`TenantScope(db, 0)` 表示「不过滤」**（用于 `tenant_id=0` 的平台级账号，如默认 admin）。
因此**漏传 tenantID 会静默退化为全表查询**，造成跨租户数据泄漏 —— 这是该类问题最典型的成因，
务必让 tenantID 贯穿 Controller → Service → Repository 全链路。

```go
// ✅ 允许:
func (r *memberRepository) FindByID(tenantID, id uint) (*model.Member, error) {
    return common.TenantScope(database.DB, tenantID).First(&member, id).Error
}

// ❌ 禁止:
func (r *memberRepository) FindByID(id uint) (*model.Member, error) {
    return database.DB.First(&member, id).Error  // 缺少 tenant 过滤
}
```

### 规则8: 禁止直接使用 db.Where()

所有数据库查询必须通过 Repository 封装。

```
❌ 禁止:
db.Where("username = ?", username).First(&user)

✅ 允许:
userRepository.FindByUsername(username)
```

### 规则9: 新增功能优先复用已有 Service

- 优先调用已有 Service 获取数据，禁止重复实现
- 新模块应依赖现有 Service，而非直接访问其 Repository
- Service 间互相调用通过接口，禁止直接 `NewXxxRepository()` 绕过 Service 层

### 规则10: 生成代码前先分析项目现有结构

在编写新代码前，必须：
1. 查看现有模块结构（model/repository/service/controller）
2. 遵循现有命名规范和代码风格
3. 复用已有的公共组件和工具函数（如 `pkg/utils/`、`internal/common/`）
4. 确保与现有架构一致

### 规则11: 前端页面必须支持响应式

所有前端页面必须适配多端显示：

- **搜索栏**: 使用 `el-row` + `el-col` 响应式断点（`:xs="24" :sm="12" :md="8" :lg="6"`）
- **表格**: 小屏启用横向滚动
- **弹窗**: 小屏（<768px）自动切换为 92% 宽度
- **统计卡片**: 使用响应式列数（`:xs="24" :sm="12" :md="6"`）
- **操作列**: 小屏使用 `MobileAction` 组件折叠操作按钮
- **侧边栏**: 移动端自动折叠为图标模式
- **断点规范**: mobile(<768px) / tablet(768-1024px) / desktop(>1024px)

使用 `src/hooks/useResponsive.ts` 获取设备状态，使用 `src/assets/styles/responsive.scss` 的 mixin。

### 规则12: 支付回调接口不做鉴权

支付回调接口（微信/支付宝通知）不经过 Auth 中间件，直接在 `router/router.go` 顶层注册：

```go
r.POST("/api/v1/pay/notify/wechat", payController.WechatNotify)
r.POST("/api/v1/pay/notify/alipay", payController.AlipayNotify)
```

回调接口必须自行验签，防止伪造请求。

### 规则13: 新增路由必须登记权限码

受保护路由**必须**用 `protected()` 注册并声明权限码，权限码与 `sys_menu.permission` 保持一致：

```go
protected(system, http.MethodGet, "/user/list", permUserList, userController.FindList)
```

- 权限码常量集中在 `router/router.go` 顶部定义（`permXxx`）
- 需要新权限码时，**同时**在 `sql/init.sql` 的 `sys_menu` 补一条 `type=2` 的按钮权限记录，
  否则该权限无法被分配给角色
- **仅要求登录态**的自助接口（userInfo / changePwd / dashboard / logout）权限码传空串 `""`
- 鉴权中间件按登记表校验，**未登记的路由默认拒绝（403）**。漏配不会静默开放，
  但会导致接口不可用，新增路由后务必实测

授权关系来自 `sys_role_menu`（角色-菜单），中间件启动时及角色变更后自动同步为 Casbin 策略，
因此**给角色勾选菜单即等于分配权限**，无需手工维护 `casbin_rule` 表。

## 中间件使用顺序

路由组注册时的中间件应用顺序（`router/router.go`）：

```
全局中间件: Recovery → Logger → Cors → Tenant
鉴权中间件: Auth → CasbinAuth → OperationLog（仅受保护路由组）
```

权限校验流程：路由注册阶段由 `protected()` 把「方法 + 完整路径」与权限码登记到中间件的
路由权限表 → 请求到达时 `CasbinAuth` 用 `c.FullPath()` 查表 → 按当前用户的角色逐个 `Enforce`。

⚠️ **不要把权限码做成路由级中间件去 `c.Set()`**：gin 的**组中间件先于路由级中间件执行**，
组级 `CasbinAuth` 运行时该值尚未写入，会导致校验被整体跳过（等于全部放行）。
这是本项目中已经踩过一次的坑。

## 后端目录规范

```
internal/module/<模块名>/
├── controller/    # 接口层（1个文件 = 1个功能域）
├── service/       # 业务层（与 controller 一一对应）
├── repository/    # 数据层（与 controller 一一对应）
├── model/         # 数据模型（继承 BaseModel 或 TenantBaseModel）
├── dto/           # 请求 DTO（binding tag 校验）
└── vo/            # 响应 VO
```

## 前端目录规范

```
web/src/
├── api/<模块名>.ts      # API 接口定义（与后端路由一一对应）
├── views/<模块名>/      # 页面组件（支持响应式）
├── components/          # 公共组件（11个：ClickCaptcha, FormDialog, ImagePicker, MobileAction, PageHeader, Pagination, RightPanel, SvgIcon, TableSkeleton, Upload, WangEditor）
├── hooks/               # useResponsive, useTheme
├── store/modules/       # app/permission/tagsView/user
├── utils/               # auth.ts, format.ts, request.ts
└── layout/              # 布局组件
```

## 已实现模块

### system 模块（系统管理）

| 功能 | API 路径 |
|------|----------|
| 用户管理 | `/api/v1/system/user/*` |
| 角色管理 | `/api/v1/system/role/*` |
| 菜单管理 | `/api/v1/system/menu/*` |
| 部门管理 | `/api/v1/system/dept/*` |
| 岗位管理 | `/api/v1/system/post/*` |
| 系统配置 | `/api/v1/system/config/*` |
| 数据字典 | `/api/v1/system/dict/*` |
| 日志管理 | `/api/v1/system/log/*` |
| 文件管理 | `/api/v1/system/file/*` |
| 协议管理 | `/api/v1/system/agreement/*` |
| 仪表盘 | `/api/v1/system/dashboard/*` |

### payment 模块（支付管理）

- 支付订单 CRUD
- 微信支付 V3（JSAPI/Native/退款/查询）
- 支付宝开放平台（H5/App/Page/退款/查询）
- 微信/支付宝回调处理（独立验签，无鉴权）

### member 模块（会员管理）

- 会员管理、会员等级、会员标签、积分日志

### captcha 模块（验证码）

- 验证码生成与验证（无 Repository 层）

## 共享包（pkg/）

| 包 | 用途 |
|----|------|
| `pkg/auth` | JWT Token 创建与验证 |
| `pkg/upload` | 多端文件上传（本地/OSS/COS/MinIO），启动时从 sys_config 读取 oss.* 配置自动选择 |
| `pkg/excel` | Excel 导入导出（excelize） |
| `pkg/task` | 定时任务调度（robfig/cron） |
| `pkg/utils` | Hash/Snowflake ID/字符串工具 |

## 代码风格

- Go 代码遵循 `gofmt` 标准格式
- 错误处理必须显式检查，禁止 `_` 忽略关键错误
- 所有公开函数必须有注释（Swagger 格式）
- DTO 使用 `binding` tag 进行参数校验
- Model 使用 `gorm` tag 定义数据库字段
- JSON 字段使用小驼峰命名
- 日志统一使用 `internal/logger` 的 Zap 实例，禁止使用 `log.Printf`
- 前端 API 文件与后端路由模块一一对应
- 前端组件优先使用 Element Plus 内置组件

## 常用命令

```bash
# 后端
go mod tidy                    # 整理依赖
go build -o server.exe ./cmd/server  # 编译
go test ./...                  # 运行测试
go test ./internal/module/payment/... -v  # 模块测试
swag init -g cmd/server/main.go -o docs  # 生成 Swagger 文档
go vet ./...                   # 静态检查

# 前端
cd web && npm install          # 安装依赖
npm run dev                    # 开发服务器
npm run build                  # 构建（含类型检查）

# Makefile
make build                     # 编译
make run                       # 运行
make test                      # 测试
make lint                      # 静态检查
make swagger                   # 生成文档
make deps                      # 整理依赖

# 一键启动（Windows）
.\run.bat                     # 杀旧进程 + 编译 + 启动后端
.\start-all.ps1               # 同时启动前后端
.\start-backend.ps1           # 仅启动后端（不杀旧进程）
.\start-frontend.ps1          # 仅启动前端
```

## 安全规范

### 认证与授权

- JWT Secret 通过环境变量 `JWT_SECRET` 注入，禁止硬编码；**生产环境（mode=release）若仍为默认值将拒绝启动**
- Access Token 有效期 2 小时，Refresh Token 7 天；两者 Issuer 不同，`ParseToken` 只接受 Access Token，`ParseRefreshToken` 只接受 Refresh Token
- 登录限频：**IP 与账号双维度**各 5 次失败后锁定 15 分钟（仅在失败时计数，登录成功则清零）
- 限频计数**必须用 `SETNX` 带 TTL 建键**，不要写「先 `INCR` 再 `EXPIRE`」：
  后者是两次往返，若 `INCR` 成功而 `EXPIRE` 失败，该 key 永不过期，
  会把对应的 IP/账号**永久锁死**，只能人工清 Redis
- 密码修改/用户禁用后自动吊销所有 Token（`userService.revokeUserTokens`）
- 退出登录时将 Access Token 加入 Redis 黑名单（`cache.RevokeToken`），Auth 中间件检查 `IsTokenRevoked`
- **Refresh Token 必须登记用户维度索引**：登录/刷新轮换时写入 `cache.RefreshTokenSetKey(userID)`（Redis Set）。
  Refresh Token 本身是随机串，没有这个索引就无法按用户批量吊销 —— 改密/禁用/登出都会静默失效。
  吊销时：`SMembers` 取全部 token → 逐个删 `cache.RefreshTokenKey(token)` → 删集合
- **`AuthService.Logout` 的 userID 来自 JWT claims，不要把 access token 当 refresh token 传**
  （历史 bug：拿 access token 去拼 `refresh_token:<accessToken>`，删了一个不存在的键）

### 登录验证码

- 登录**强制**要求 `captchaToken`（`LoginRequest.CaptchaToken` 为 `binding:"required"`）
- 流程：`GET /api/v1/captcha/generate` → `POST /api/v1/captcha/verify` → 用返回的 token 调 `/auth/login`
- `captchaService.Verify` 成功时写入一次性凭证 `captcha:verified:<token>`（5 分钟）；
  登录用 `captchaService.ConsumeVerifiedToken()` 校验并消费，凭证不可复用
- ⚠️ **禁止在 Controller 里对同一个请求体做两次 `ShouldBindJSON`**：请求体读完即耗尽，
  第二次绑定必然失败。若错误被 `_ =` 丢弃，整个校验分支会变成永不执行的死代码（历史 bug）
- **RBAC 已启用**：主体为角色 code，策略由 `sys_role_menu` + `sys_menu.permission` 自动生成（见规则13）；
  角色 `admin` 始终持有 `*` 通配策略；未登记权限码的路由默认拒绝
- 登录失败统一返回「用户名或密码错误」，避免用户名枚举

### 密码安全

- 密码使用 bcrypt 哈希（`bcrypt.DefaultCost`）
- 密码强度校验：至少包含大写字母、小写字母、数字中的两种，禁止包含空格
- 适用场景：用户创建（`Create`）、密码重置（`ResetPassword`）、密码修改（`ChangePassword`）

### 多租户隔离

- 所有 Repository 查询必须通过 `common.TenantScope(db, tenantID)` 过滤
- tenant_id 仅从 JWT claims 获取，禁止从 Header/Query 参数读取
- 关联表操作（如 `ReplaceTags`、`FindTagIDsByMemberID`）也必须使用 `TenantScope`
- 支付回调等外部接口使用 `FindByXxxForNotify()`（无 tenant 过滤）

### 文件上传

- 扩展名白名单校验（jpg/png/gif/bmp/svg/webp/mp4/mov/mp3/pdf/doc/xls/ppt/zip 等）
- 危险扩展名拦截（php/exe/sh/bat/js/vbs 等）
- 文件大小限制从 `config.yaml` 的 `upload.max_size` 读取（单位 MB），默认 10MB
- `/uploads` 为匿名可读的静态目录，由 `middleware.UploadSecurity()` 补 CSP 沙箱响应头防御 SVG XSS
- **密钥/证书类文件禁止存放在 `uploads/` 下** —— 该目录匿名可读。证书上传写入 `runtime/certs/`
- 拼接上传目录路径做删除时必须做穿越校验（Clean + 拒绝绝对路径/`..` + 拼接后前缀校验）
- 白名单来自配置 `upload.allow_exts`（逗号分隔，启动时由 `upload.SetAllowedExts` 注入）。
  空值表示沿用内置默认；`dangerousExts` 硬编码黑名单**始终生效**，不随配置放宽

### CORS 配置

- 生产环境必须在 `config.yaml` 的 `cors.allow_origins` 配置白名单
- 开发环境（mode: debug）允许所有来源
- `AllowCredentials: true` 必须配合明确的 Origin 白名单

### 支付安全

- 微信支付回调验签已实现（平台证书获取 + RSA 验签，证书带 10 分钟缓存）
- 微信 APIv3 **请求签名串 = `方法\n路径(含query)\n时间戳\n随机串\n报文主体\n`**。
  两个易错点：URL 必须去掉协议与域名（用 `canonicalURL()`）；**报文主体必须参与签名**，
  漏掉 body 会让下单/退款全部 401，且因证书拉取也走同一签名函数，回调验签会连带失效
- 支付宝回调验签已实现
- 回调金额校验（防止金额篡改）
- **金额换算禁止用浮点**：`int64(amt*100)` 会因表示误差少 1 分（19.99 → 1998），
  导致回调判定「支付金额不匹配」。统一用 `yuanToFen()` 按字符串拆分
- returnURL 开放重定向防护（协议和主机名校验）

#### 涉及外部资金的操作必须先抢占状态（重要）

支付渠道调用是**不可逆的资金操作**，绝不能「先查状态 → 调渠道 → 再改状态」——
两个并发请求会同时通过状态检查，于是真实退款两次：数据库最终一致，钱多退一份。
实例级 mutex 也救不了，它只保护改状态那一步，而资金操作在锁外。

统一采用「条件更新抢占 + 影响行数判断」：

```go
// ✅ 正确顺序
if err := validateRefund(order, amt); err != nil { return err }   // 1. 纯校验
claimed, _ := repo.ClaimRefund(orderNo)                            // 2. 条件更新 1→4
if !claimed { return common.NewBizError("退款正在处理中或已退款") }  //    抢不到就拒绝
err := s.refund(order, ...)                                        // 3. 抢到才调渠道
if err != nil { repo.ReleaseRefundClaim(orderNo); return ... }     //    失败回滚
repo.MarkRefunded(orderNo, amt, now)                               // 4. 成功落定
```

订单状态见 `payment/model/pay_order.go` 的常量（**禁止裸数字**）：
`0待支付 1已支付 2已关闭 3已退款 4退款中`。
`4退款中` 就是为抢占而引入的中间态，前端也需要相应展示。

回调幂等（`MarkPaidIfPending`）用的是同一套模式，两者应保持一致。

#### 订单号生成

`genOrderNo(prefix)` = 前缀 + 毫秒时间戳 + 10 位加密随机 hex。
**不要退回「时间戳 + 纳秒末四位」**：同毫秒并发有约 1/10000 概率撞号，
而 `order_no` 有唯一索引；更糟的是撞号时 `CreateOrder` 可能把**别人的订单**返回给调用方。
`CreateOrder` 只在「标题+金额+渠道全一致」时才复用原单，否则报错。

### 依赖

- **Redis 是启动强依赖**，初始化失败直接退出。它承载 refresh token 存储、token 黑名单、
  登录限频、验证码与角色缓存；注释里的「可选」是历史误导
- MySQL 同理，失败即退出
- 日志轮转使用 `gopkg.in/natefinch/lumberjack.v2`，对应 `log.max_size / max_backups /
  max_age / compress` 四个配置项（此前这些配置项声明了却从未被读取）

### 定时任务

- 新增任务**必须**通过 `task.AddJob` 注册。它会自动包一层 recover：
  robfig/cron 默认不 recover panic，任务内一旦 panic 会**直接终止整个进程**，
  而 `middleware.Recovery` 只覆盖 HTTP 请求，救不了后台任务
- `pkg/task` 不依赖 `internal/`（保持 pkg 层依赖方向），panic 处理函数由
  `main.go` 通过 `task.SetPanicHandler` 注入到 zap 日志

### 日志

- `log.filename` 的**父目录会在启动时自动创建**，并做一次可写性探测，失败则拒绝启动。
  不要退回「`os.OpenFile` 失败静默跳过」的写法 —— 那会让人以为在写文件，实际只输出到 stdout
- 日志同时输出到 stdout 与文件

### 路径

- `log.filename` / `upload.save_path` / `casbin.model_path` 均为**相对工作目录**的路径，
  因此必须从项目根目录启动（`./server.exe`）。用 systemd/supervisor 部署时要显式设置
  `WorkingDirectory`，否则 casbin 加载失败会导致服务拒绝启动

### 层级数据（菜单 / 部门）

- 更新 `parent_id` 前必须调用 `hasCycleInHierarchy` 校验成环。
  环本身不会导致递归死循环（每个节点只有一个父节点，遍历中最多出现一次），
  但那棵子树会**从界面上消失**却仍留在库里，成为不可见也不可删的孤儿数据

### 敏感配置

```bash
# 环境变量覆盖（优先级高于 config.yaml）
export DB_PASSWORD="your_db_password"
export JWT_SECRET="your_jwt_secret"
export REDIS_PASSWORD="your_redis_password"
```

- 生产环境若 `jwt.secret` 或 `database.password` 仍为配置文件中的默认值，服务将**拒绝启动**
- `sys_config` 中的敏感项（key 命中 secret / password / key / pem / private / token）经接口返回时
  会打码为 `******`；**服务内部读取真实值必须用 `ConfigService.FindByPrefixRaw()`**
- `BatchSave` 会跳过值为 `******` 的项，因此前端原样回传占位符不会覆盖真实密钥
- 新增支付/存储类密钥时，配置项 key 应包含上述关键词，以便自动纳入打码
- **操作日志会自动脱敏请求体**：`middleware/operation_log.go` 的 `sensitiveNameFragments`
  （匹配 password / secret / token / privatekey / accesskey / apiv3key / pem 等片段），
  并识别 `{"key":"secret_key","value":"真实值"}` 这种「名称 + 取值」分离的结构
  命中 `password / oldpassword / newpassword / secret / privatekey / accesskey / apiv3key / token` 等
  字段名即替换为 `******`，非 JSON 请求体不记录内容，整体按 2000 字截断。
  新增接口若含其他敏感字段，需把字段名加入该 map
- 操作日志/登录日志按 `log.db_retention_days`（默认 90 天）由定时任务每天 03:00 清理；
  定时任务框架为 `pkg/task`，在 `cmd/server/main.go` 中注册并 `task.Start()`

### 软删除与唯一索引

唯一索引**不区分记录是否已软删除**。删除时若不改写唯一字段，被删记录会一直占用该值，
导致同名记录无法再次创建（`Duplicate entry`）。

因此 `Delete` 必须**在事务内**完成两件事：

1. 改写唯一字段释放索引占用 —— 用 `common.FreedUniqueValue(value, id, maxLen)`
2. 清理关联表，避免留下孤儿记录

涉及的表与列：

| 表 | 唯一列 | 列长 | 删除时需清理的关联表 |
|----|--------|------|---------------------|
| `sys_user` | `username` | 64 | `sys_user_role`、`sys_user_post` |
| `sys_role` | `code` | 64 | `sys_user_role`、`sys_role_menu` |
| `sys_post` | `(tenant_id, code)` | 64 | `sys_user_post` |
| `sys_menu` | — | — | `sys_role_menu` |
| `sys_config` | `config_key` | 191 | — |
| `sys_dict_type` | `type` | 128 | — |
| `pay_member` | `phone` / `member_no` | 20 / 32 | `pay_member_tag_rel` |
| `pay_member_tag` | — | — | `pay_member_tag_rel` |

⚠️ **不要用 GORM 的 `BeforeDelete` 钩子做这件事**：`db.Delete(&Model{}, id)` 不会加载模型，
钩子里读到的字段是空的，改写会把值覆盖成 `_del_<id>`，造成数据损坏。必须显式「先查后改」。

### 前端安全

- Token 存储在 Cookie 中，设置 `sameSite: Lax` + `secure`（HTTPS 时）
- 无 `v-html` / `innerHTML` 使用（XSS 安全）
- Swagger 文档仅在非 release 模式暴露

## 新业务模块接入清单

1. 在 `internal/module/<模块名>/model/` 创建数据模型（多租户表继承 `TenantBaseModel`，全局表继承 `BaseModel`）
2. 在 `internal/module/<模块名>/repository/` 创建数据访问层（多租户表的方法**必须接收 `tenantID`**）
3. 在 `internal/module/<模块名>/service/` 创建业务逻辑层（**透传 `tenantID`**）
4. 在 `internal/module/<模块名>/dto/` 创建请求 DTO
5. 在 `internal/module/<模块名>/vo/` 创建响应 VO（如需要）
6. 在 `internal/module/<模块名>/controller/` 创建接口层
7. 在 `router/router.go` 顶部定义权限码常量，并用 `protected()` 注册路由（见规则13）
8. 在 `sql/init.sql` 添加建表语句、初始数据，**以及新权限码对应的 `sys_menu` 按钮记录**（`type=2`）
9. 在 `web/src/api/` 创建前端 API 文件
10. 在 `web/src/views/` 创建前端页面（支持响应式）
11. 在 controller 方法上添加 Swagger 注解
12. 运行 `swag init -g cmd/server/main.go -o docs` 生成文档
13. **实测验证**：分别用有权限和无权限的账号访问新接口，确认返回 200 / 403 符合预期
