# Gin-Admin

[中文](README.md) | [English](README.en.md)

# Gin-Admin 后台管理框架

基于 **Go + Gin + GORM + Vue3 + Element Plus** 的中大型业务系统后台管理框架。

适用于活动管理、CRM、Agent管理平台、企业内部管理系统等场景。

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Vue](https://img.shields.io/badge/Vue-3.5-42b883?style=flat&logo=vue.js)](https://vuejs.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

## 功能特性

- **完整的 RBAC 权限体系** - 用户 → 角色 → 菜单/按钮权限，支持多租户
- **多端支付集成** - 微信支付（JSAPI/Native/退款）+ 支付宝（H5/Page/退款）
- **多存储方案** - 本地存储 / 阿里云 OSS / 腾讯云 COS / MinIO
- **会员系统** - 会员管理、等级体系、标签系统、积分日志
- **响应式前端** - 桌面端 + 移动端自适应布局
- **暗黑模式** - 一键切换亮色/暗色主题
- **点击验证码** - 防暴力破解登录
- **操作日志** - 完整的操作审计追踪

## 技术栈

### 后端 Backend

| 技术 | 版本 | 说明 |
|------|------|------|
| ![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat&logo=go&logoColor=white) | 1.25 | 编程语言 |
| ![Gin](https://img.shields.io/badge/Gin-v1.10.0-008ECF?style=flat&logo=gin&logoColor=white) | v1.10.0 | Web 框架 |
| ![GORM](https://img.shields.io/badge/GORM-v1.25.12-0092D2?style=flat&logo=database&logoColor=white) | v1.25.12 | ORM 框架 |
| ![MySQL](https://img.shields.io/badge/MySQL-5.7%2B-4479A1?style=flat&logo=mysql&logoColor=white) | 5.7+ | 关系型数据库 |
| ![Redis](https://img.shields.io/badge/Redis-3.0%2B-DC382D?style=flat&logo=redis&logoColor=white) | 3.0+ | 缓存/Token存储 |
| ![JWT](https://img.shields.io/badge/JWT-v5.2.3-000000?style=flat&logo=json-web-tokens&logoColor=white) | v5.2.3 | 认证授权 |
| ![Casbin](https://img.shields.io/badge/Casbin-v2.103.0-3D9B8F?style=flat&logo=casbin&logoColor=white) | v2.103.0 | RBAC 权限控制 |
| ![Viper](https://img.shields.io/badge/Viper-v1.19.0-BD3FEB?style=flat&logo=viper.js&logoColor=white) | v1.19.0 | 配置管理 |
| ![Zap](https://img.shields.io/badge/Zap-v1.27.0-ECBA52?style=flat&logo=go&logoColor=white) | v1.27.0 | 结构化日志 |
| ![Swagger](https://img.shields.io/badge/Swagger-v1.6.0-85EA2D?style=flat&logo=swagger&logoColor=white) | v1.6.0 | API 文档 |
| ![OSS](https://img.shields.io/badge/Aliyun_OSS-v3.0.2-FF6A00?style=flat&logo=alibabacloud&logoColor=white) | v3.0.2 | 阿里云对象存储 |
| ![COS](https://img.shields.io/badge/Tencent_COS-v0.7.74-006EFF?style=flat&logo=tencentcloud&logoColor=white) | v0.7.74 | 腾讯云对象存储 |
| ![MinIO](https://img.shields.io/badge/MinIO-v7.2.1-C72C48?style=flat&logo=minio&logoColor=white) | v7.2.1 | S3 兼容存储 |

### 前端 Frontend

| 技术 | 版本 | 说明 |
|------|------|------|
| ![Vue](https://img.shields.io/badge/Vue-3.5.13-42b883?style=flat&logo=vuedotjs&logoColor=white) | 3.5.13 | 渐进式框架 |
| ![Vue Router](https://img.shields.io/badge/Vue_Router-4.5.0-42b883?style=flat&logo=vuedotjs&logoColor=white) | 4.5.0 | 路由管理 |
| ![Pinia](https://img.shields.io/badge/Pinia-2.3.0-FCCD2B?style=flat&logo=pinia&logoColor=white) | 2.3.0 | 状态管理 |
| ![Element Plus](https://img.shields.io/badge/Element_Plus-2.9.1-409EFF?style=flat&logo=element&logoColor=white) | 2.9.1 | UI 组件库 |
| ![Axios](https://img.shields.io/badge/Axios-1.7.9-5A29E4?style=flat&logo=axios&logoColor=white) | 1.7.9 | HTTP 客户端 |
| ![Vite](https://img.shields.io/badge/Vite-6.0.5-646CFF?style=flat&logo=vite&logoColor=white) | 6.0.5 | 构建工具 |
| ![TypeScript](https://img.shields.io/badge/TypeScript-5.7.2-3178C6?style=flat&logo=typescript&logoColor=white) | 5.7.2 | 类型系统 |

## 项目结构

```
go-admin/
├── cmd/server/main.go              # 后端入口
├── config/
│   ├── config.yaml                 # 应用配置
│   ├── config.go                   # 配置加载
│   └── casbin/model.conf           # RBAC 模型
├── internal/
│   ├── cache/redis.go              # Redis 封装（可选）
│   ├── common/                     # 统一响应/错误码/模型/分页
│   ├── database/mysql.go           # MySQL 连接
│   ├── logger/zap.go               # Zap 日志
│   ├── middleware/                  # 7个中间件
│   │   ├── auth.go                 # JWT 认证
│   │   ├── casbin.go               # RBAC 鉴权
│   │   ├── cors.go                 # 跨域处理
│   │   ├── operation_log.go        # 操作日志
│   │   ├── recovery.go             # 异常恢复
│   │   ├── logger.go               # 请求日志
│   │   └── tenant.go               # 多租户
│   └── module/
│       ├── system/                 # 系统管理（用户/角色/菜单/部门/岗位/配置/字典/日志/文件/协议）
│       │   ├── controller/
│       │   ├── service/
│       │   ├── repository/
│       │   ├── model/
│       │   ├── dto/
│       │   └── vo/
│       ├── payment/                # 支付模块（微信/支付宝）
│       │   ├── controller/
│       │   ├── service/
│       │   ├── repository/
│       │   └── model/
│       ├── member/                 # 会员模块（会员/等级/标签/积分）
│       │   ├── controller/
│       │   ├── service/
│       │   ├── repository/
│       │   └── model/
│       └── captcha/                # 验证码（无 Repository 层）
├── pkg/
│   ├── auth/jwt.go                 # JWT Token 创建与验证
│   ├── upload/                     # 多端文件上传
│   │   ├── upload.go               # 上传入口（自动选择存储方式）
│   │   ├── local.go                # 本地存储
│   │   ├── aliyun_oss.go           # 阿里云 OSS
│   │   ├── tencent_cos.go          # 腾讯云 COS
│   │   └── minio.go                # MinIO
│   ├── excel/excel.go              # Excel 导入导出（excelize）
│   ├── task/cron.go                # 定时任务调度（robfig/cron）
│   └── utils/                      # 工具函数（Hash/Snowflake/字符串）
├── router/router.go                # 路由注册（含 /uploads 静态服务）
├── sql/
│   └── init.sql                    # 数据库初始化脚本
├── docs/                           # Swagger 文档
├── web/                            # 前端 (Vue3 + Element Plus)
│   └── src/
│       ├── api/                    # API 接口定义（16个模块）
│       ├── components/             # 公共组件（7个）
│       │   ├── ClickCaptcha/       # 点击验证码
│       │   ├── DictTag/            # 字典标签
│       │   ├── FormDialog/         # 表单弹窗
│       │   ├── ImagePicker/        # 图片选择器
│       │   ├── MobileAction/       # 移动端操作按钮
│       │   ├── Pagination/         # 分页组件
│       │   └── WangEditor/         # 富文本编辑器
│       ├── hooks/                  # 组合式函数
│       │   ├── useResponsive.ts    # 响应式断点
│       │   └── useTheme.ts         # 主题切换
│       ├── layout/                 # 布局组件
│       │   ├── index.vue           # 主布局
│       │   └── components/
│       │       ├── Sidebar.vue     # 侧边栏
│       │       ├── Navbar.vue      # 顶栏
│       │       ├── TagsView.vue    # 标签页导航
│       │       ├── AppMain.vue     # 主内容区
│       │       └── Breadcrumb.vue  # 面包屑
│       ├── router/                 # 路由配置（动态菜单）
│       ├── store/modules/          # Pinia 状态
│       │   ├── app.ts              # 应用状态
│       │   ├── user.ts             # 用户状态
│       │   ├── permission.ts       # 权限状态
│       │   └── tagsView.ts         # 标签页状态
│       ├── utils/                  # 工具函数
│       │   ├── auth.ts             # Token 管理
│       │   ├── request.ts          # Axios 封装
│       │   └── format.ts           # 格式化工具
│       └── views/                  # 页面视图
│           ├── login/              # 登录页
│           ├── dashboard/          # 仪表盘
│           ├── error/              # 错误页
│           ├── system/             # 系统管理页面
│           ├── settings/           # 设置页面
│           ├── member/             # 会员页面
│           └── payment/            # 支付页面
├── AGENTS.md                       # AI Agent 开发规范
├── Makefile                        # 构建脚本
├── start-all.ps1                   # 一键启动（Windows）
├── start-backend.ps1               # 启动后端
├── start-frontend.ps1              # 启动前端
└── run.bat                         # 后端运行脚本
```

## 架构设计

### 后端三层架构

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

### 中间件顺序

```
全局: Recovery → Logger → Cors → Tenant
鉴权: Auth → CasbinAuth → OperationLog（仅受保护路由组）
```

### 前端架构

```
Vue3 + Element Plus + Pinia + Vue Router

API 层: Axios 拦截器 + JWT Token 自动注入
路由层: 动态路由（后端菜单驱动）+ 前端路由守卫
状态层: Pinia (app/user/permission/tagsView)
视图层: 组件化开发 + 响应式布局
```

### 安全特性

| 特性 | 实现 |
|------|------|
| 多租户隔离 | `TenantScope` 自动过滤，仅从 JWT 获取 tenant_id，关联表操作同步过滤 |
| 登录限频 | Redis IP 计数，5分钟/5次，超限锁定15分钟 |
| Token 黑名单 | 退出登录时 Access Token 加入 Redis 黑名单，Auth 中间件实时检查 |
| Token 吊销 | 密码修改/用户禁用后自动吊销所有 Token |
| 密码安全 | bcrypt 哈希 + 强度校验（大写/小写/数字至少两种，禁止空格） |
| RBAC 权限 | Casbin 策略控制（空策略时跳过） |
| CORS 白名单 | 生产环境必须配置 `cors.allow_origins` |
| 文件上传校验 | 扩展名白名单 + 危险扩展名拦截，大小限制从配置读取 |
| 支付回调验签 | 微信平台证书 RSA + 支付宝签名验证 |
| 支付金额校验 | 回调时比对订单金额，防止篡改 |
| 开放重定向防护 | returnURL 协议和主机名校验 |

## 功能模块

### 系统管理

| 模块 | 说明 | API |
|------|------|-----|
| 用户管理 | 增删改查、状态管理、密码重置 | `/api/v1/system/user/*` |
| 角色管理 | 增删改查、菜单权限分配 | `/api/v1/system/role/*` |
| 菜单管理 | 目录/菜单/按钮三级管理 | `/api/v1/system/menu/*` |
| 部门管理 | 树形部门结构 | `/api/v1/system/dept/*` |
| 岗位管理 | 岗位增删改查 | `/api/v1/system/post/*` |
| 系统配置 | 参数键值配置 | `/api/v1/system/config/*` |
| 数据字典 | 字典类型 + 字典数据 | `/api/v1/system/dict/*` |
| 日志管理 | 操作日志 + 登录日志 | `/api/v1/system/log/*` |
| 附件管理 | 图片上传、预览、删除 | `/api/v1/system/file/*` |
| 协议管理 | 用户协议/隐私政策 | `/api/v1/system/agreement/*` |
| 仪表盘 | 数据统计概览 | `/api/v1/dashboard/stats` |

### 支付管理

| 模块 | 说明 | API |
|------|------|-----|
| 创建订单 | 微信JSAPI/Native、支付宝H5/Page | `POST /api/v1/system/pay/order` |
| 订单查询 | 按订单号查询 | `GET /api/v1/system/pay/order` |
| 关闭订单 | 关闭待支付订单 | `POST /api/v1/system/pay/order/close` |
| 申请退款 | 微信/支付宝退款 | `POST /api/v1/system/pay/order/refund` |
| 支付回调 | 微信/支付宝异步通知 | `POST /api/v1/pay/notify/*` |

**微信支付支持**：
- JSAPI 支付（公众号/小程序）
- Native 支付（扫码支付）
- 退款（部分/全额）
- 订单查询

**支付宝支持**：
- H5 支付（手机网页）
- App 支付
- Page 支付（电脑网站）
- 退款
- 订单查询

### 会员管理

| 模块 | 说明 | API |
|------|------|-----|
| 会员管理 | 会员增删改查、状态管理 | `/api/v1/member/*` |
| 会员等级 | 等级配置、升级规则 | `/api/v1/member/level/*` |
| 会员标签 | 标签管理、批量打标 | `/api/v1/member/tag/*` |
| 积分日志 | 积分变动记录 | `/api/v1/member/points/*` |

### 认证授权

- JWT 登录 + Refresh Token（Access 2h / Refresh 7d）
- Casbin RBAC 权限控制
- 用户 → 角色 → 菜单/按钮权限
- 点击验证码（防暴力破解）
- 登录限频（5分钟/5次，Redis 计数）
- Token 黑名单（退出登录即时失效）
- Token 吊销（密码修改/禁用后立即失效）
- 密码强度校验（大小写+数字至少两种，禁止空格）

### 文件上传

- 本地存储（默认）
- 阿里云 OSS
- 腾讯云 COS
- MinIO（S3 兼容）
- 启动时自动读取 `sys_config` 表 `oss.*` 配置选择存储方式
- 支持格式：图片（jpg/png/gif/bmp/svg/webp）、视频（mp4/mov/avi）、音频（mp3/wav）、文档（pdf/doc/xls/ppt）、压缩包（zip/rar/7z）

### 前端特性

- **响应式布局** - 桌面端/平板/手机自适应
- **暗黑模式** - 一键切换亮色/暗色主题
- **动态菜单** - 后端菜单驱动，支持多级目录
- **标签页导航** - 多标签页快速切换
- **富文本编辑器** - 集成 WangEditor
- **图片选择器** - 从已上传文件中选择
- **表格骨架屏** - 加载状态优化

## 快速开始

### 环境要求

- Go 1.25+
- MySQL 5.7+
- Redis 3.0+
- Node.js 18+

### 1. 数据库初始化

```bash
mysql -u root -p < sql/init.sql
```

### 2. 启动后端

```bash
# 修改配置
vim config/config.yaml

# 编译运行
go mod tidy
go build -o server.exe ./cmd/server
./server.exe
```

### 3. 启动前端

```bash
cd web
npm install
npm run dev
```

### 4. 一键启动（Windows）

```powershell
.\start-all.ps1           # 同时启动前后端
.\start-backend.ps1       # 仅启动后端
.\start-frontend.ps1      # 仅启动前端
```

### 5. 访问

| 地址 | 说明 |
|------|------|
| http://localhost:3000 | 前端页面 |
| http://localhost:8080 | 后端 API |
| http://localhost:8080/swagger/index.html | API 文档 |

### 默认账号

- 用户名：`admin`
- 密码：`admin123`

> ⚠️ **首次登录后请立即修改密码。** 这组凭据由 `sql/init.sql` 预置，任何了解本项目的人都知道。

## 部署（生产环境）

### 容器化部署（推荐）

Linux 服务器可用，不依赖 Windows 脚本：

```bash
cp .env.example .env            # 填写 MYSQL_PASSWORD / MYSQL_ROOT_PASSWORD / REDIS_PASSWORD / JWT_SECRET
vi .env                         # 这些变量**没有默认值**，缺任何一个 compose 会直接报错退出
docker compose up -d --build
docker compose ps               # 等 mysql / app 变成 healthy
# 访问 http://<服务器IP>:8080（端口由 .env 的 WEB_PORT 控制）
```

### 数据库升级

⚠️ **不要手工逐条执行 `sql/migrations/` 下的脚本** —— 漏掉一个不会有任何提示，
直到某个接口报 `Unknown column` 才被发现（例如岗位表缺 `tenant_id`，岗位页直接报错）。
用内置的迁移执行器：

```bash
make migrate-status   # 查看待执行的迁移（只读，不执行 SQL）
make migrate          # 执行所有未应用的迁移
```

容器部署时用镜像内的执行器：

```bash
docker compose exec app /app/migrate -status
docker compose exec app /app/migrate
```

执行器把已应用的版本记在 `schema_migrations` 表，重复执行是安全的。
**注意它不做事务回滚** —— MySQL 的 DDL 会隐式提交，事务包不住；
因此迁移脚本必须幂等，这也是本项目既有约定（先用 `information_schema` 判断再操作）。

完整说明见 **[deploy/README.md](deploy/README.md)**，覆盖服务拓扑与端口暴露面、
数据库初始化与**升级迁移**、必须检查的配置项、备份、健康检查、排障，
以及不用容器时的 systemd 部署方式。

> `deploy/validate.py` 是一个离线校验脚本（无需 Docker），用于检查
> compose 引用完整性、`.dockerignore` 是否误排了构建必需目录、nginx 关键行为等。
> 改动部署文件后建议跑一次：
>
> ```bash
> pip install pyyaml          # 仅此一个依赖
> python deploy/validate.py
> ```

### 不使用容器

```bash
# 交叉编译（Go 支持跨平台编译，目标机无需安装 Go）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o go-admin ./cmd/server

# 前端产物（交给已有 nginx）
cd web && npm ci && npm run build      # 产物在 web/dist
```

⚠️ 路径类配置（`log.filename` / `upload.save_path` / `casbin.model_path`）都是
**相对工作目录**的，必须从应用根目录启动。用 systemd / supervisor 部署时要显式设置
`WorkingDirectory`，否则 casbin 加载失败会导致服务**拒绝启动**（这是有意的：
避免在"无鉴权"状态下对外提供服务）。

注意：Go 服务**不托管前端页面**，只提供 `/api/v1`、`/uploads`、`/swagger`、`/health`。
前端需由 nginx 托管并反代 `/api` 与 `/uploads`，配置可参考 `deploy/nginx/default.conf`。
其中 `/uploads` 必须反代到后端而非直接映射磁盘目录 —— 后端在该路径上挂了
`middleware.UploadSecurity()` 补 CSP 沙箱响应头，直接映射会让白名单里的 `.svg`
变成同源存储型 XSS 落点。

### 健康检查

| 端点 | 用途 |
|------|------|
| `/health` | liveness，只判断进程存活，**不探测依赖**（依赖抖动不应触发容器重启） |
| `/health/ready` | readiness，探测 MySQL 与 Redis，任一不可用返回 **503** |

两者都不需要认证，且只返回 `ok` / `down`，不返回具体错误（避免暴露内网拓扑）。

## 配置说明

### 环境变量

敏感配置支持环境变量覆盖（优先级高于 config.yaml）：

```bash
export DB_PASSWORD="your_db_password"
export JWT_SECRET="your_jwt_secret"
export REDIS_PASSWORD="your_redis_password"
```

### CORS 配置

```yaml
cors:
  allow_origins:
    - "http://localhost:3000"
    - "http://localhost:5173"
  allow_methods: ["GET","POST","PUT","DELETE","OPTIONS","PATCH"]
  allow_headers: ["Origin","Content-Type","Accept","Authorization","X-Tenant-Id"]
  allow_credentials: true
```

### 支付配置（数据库 sys_config 表）

| Key | 说明 |
|-----|------|
| pay.wechat_app_id | 微信AppID |
| pay.wechat_mch_id | 微信商户号 |
| pay.wechat_key | 微信API密钥 |
| pay.alipay_app_id | 支付宝AppID |
| pay.alipay_key | 支付宝应用私钥 |
| pay.alipay_public_key | 支付宝公钥 |
| pay.notify_url | 统一回调地址 |

### OSS 配置（数据库 sys_config 表）

| Key | 说明 | 可选值 |
|-----|------|--------|
| oss.type | 存储类型 | local / aliyun / tencent / minio |
| oss.endpoint | Endpoint | |
| oss.bucket | Bucket名称 | |
| oss.access_key | AccessKey | |
| oss.secret_key | SecretKey | |
| oss.domain | 自定义域名 | |

## 开发指南

### 后端开发

```bash
# 编译
make build

# 运行
make run

# 测试
make test

# 静态检查
make lint

# 生成 Swagger 文档
make swagger

# 整理依赖
make deps

# 清理编译产物
make clean
```

### 前端开发

```bash
cd web

# 安装依赖
npm install

# 开发服务器（热重载）
npm run dev

# 构建生产版本
npm run build

# 类型检查
npm run build  # 包含 vue-tsc 类型检查
```

### 代码规范

- Go 代码遵循 `gofmt` 标准格式
- 错误处理必须显式检查，禁止 `_` 忽略关键错误
- 所有公开函数必须有注释（Swagger 格式）
- DTO 使用 `binding` tag 进行参数校验
- Model 使用 `gorm` tag 定义数据库字段
- JSON 字段使用小驼峰命名
- 日志统一使用 `internal/logger` 的 Zap 实例，禁止使用 `log.Printf`
- 前端 API 文件与后端路由模块一一对应
- 前端组件优先使用 Element Plus 内置组件

## 测试

```bash
# 后端测试
go test ./...

# 模块测试
go test ./internal/module/payment/... -v

# 前端构建测试
cd web && npm run build
```

## 常见问题

### Q: 启动后端报错 "connection refused"

A: 确保 MySQL 和 Redis 服务已启动，并检查 `config/config.yaml` 中的连接配置。

### Q: 前端页面空白

A: 检查后端是否已启动（前端通过代理访问 `/api`），以及浏览器控制台是否有错误。

### Q: 文件上传失败

A: 检查 `sys_config` 表中的 OSS 配置，确保 `oss.type` 正确设置。

### Q: 支付回调收不到

A: 确保支付回调地址 `pay.notify_url` 可公网访问，且已在微信/支付宝后台配置。

## 打赏

如果这个项目对你有帮助，欢迎请作者喝杯咖啡~

| 微信 | 支付宝 |
|:---:|:---:|
| <img src="docs/images/weixin.jpg" width="200"> | <img src="docs/images/zhifubao.jpg" width="200"> |

## 许可证

[MIT License](LICENSE)
