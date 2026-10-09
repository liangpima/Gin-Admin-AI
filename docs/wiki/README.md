# Gin-Admin Code Wiki

本套文档基于对仓库源码的全量分析生成，覆盖项目整体架构、模块职责、关键类与函数、依赖关系及运行方式。

## 文档索引

| 文档 | 内容 |
|------|------|
| [01-项目整体架构.md](./01-项目整体架构.md) | 技术栈、目录结构、三层架构、启动链、配置加载、路由注册与权限码机制 |
| [02-后端业务模块.md](./02-后端业务模块.md) | system / payment / member / captcha 四大模块的 model、repository、service、controller 职责与关键函数 |
| [03-中间件与公共层.md](./03-中间件与公共层.md) | 全部中间件执行顺序与实现要点、`internal/common` 公共层、Redis 缓存 key 体系 |
| [04-共享包与工具链.md](./04-共享包与工具链.md) | `pkg/` 共享包、`cmd/gen` 脚手架生成器、`cmd/migrate` 迁移工具 |
| [05-数据库与安全.md](./05-数据库与安全.md) | 表清单与租户分类、迁移脚本、JWT/Casbin/验证码/限频/上传/支付安全 |
| [06-前端架构.md](./06-前端架构.md) | Vue3 前端结构、动态路由、状态管理、401 单飞续期、hooks/components、构建门禁 |
| [07-运行与部署.md](./07-运行与部署.md) | 本地开发命令、启动脚本、Makefile、Docker 部署、CI 流水线 |

## 快速入门

```bash
# 后端（需先启动 MySQL + Redis）
go mod tidy
go build -o server.exe ./cmd/server
./server.exe            # 必须从项目根目录启动

# 前端
cd web && npm install && npm run dev

# 一键启动（Windows）
.\start-all.ps1
```

默认账号 `admin / admin123`（仅限本地开发环境，见 `sql/init.sql`）。

## 阅读建议

- 二次开发前必读 [01-项目整体架构](./01-项目整体架构.md) 的「三层架构铁律」与 [05-数据库与安全](./05-数据库与安全.md) 的「多租户隔离」；
- 新增业务模块参考 [02-后端业务模块](./02-后端业务模块.md) 末尾的接入清单，优先使用 `go run ./cmd/gen` 生成骨架；
- 排查接口 403/404/500 问题时查阅 [03-中间件与公共层](./03-中间件与公共层.md) 的错误语义表与权限登记表机制。
