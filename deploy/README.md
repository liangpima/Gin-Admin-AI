# 部署指南

面向 Linux 服务器。Windows 本地开发见根目录 `README.md`。

> 本目录下 `config.docker.yaml` 是**容器专用**配置（`mode: release` + 容器内服务名）。
> 本地开发仍使用 `config/config.yaml`。

---

## 1. 服务拓扑

```
                      ┌─────────────────────────────┐
   浏览器 ──────────► │  web (nginx :80 → 宿主端口)  │
                      │  静态资源 / SPA history 回退 │
                      └──────┬──────────────┬───────┘
                             │ /api/        │ /uploads/
                             ▼              ▼
                      ┌─────────────────────────────┐
                      │      app (Go :8080)         │
                      └──────┬──────────────┬───────┘
                             ▼              ▼
                      ┌──────────┐   ┌──────────┐
                      │  mysql   │   │  redis   │
                      └──────────┘   └──────────┘
```

**为什么前端要单独用 nginx，不直接让 Go 服务托管？**

Go 服务只注册了 `/api/v1`、`/uploads`、`/swagger`、`/health` 等路由，本身不提供 SPA 页面。
前端 `baseURL` 是 `/api/v1`（同源相对路径），因此由 nginx 托管静态资源并反代 `/api`
即构成完整站点 —— 同源部署下**不需要 CORS**，也没有预检开销。

**为什么 `/uploads/` 必须反代到后端，不能由 nginx 直接映射磁盘目录？**

后端在该路径上挂了 `middleware.UploadSecurity()`，会补 `nosniff` + `CSP sandbox` 响应头。
上传白名单里含 `.svg`，而 SVG 可内嵌 `<script>`；若 nginx 直接映射本地目录，
这些响应头就没了，同源访问 `.svg` 等同于**存储型 XSS**。

---

## 2. 快速开始

```bash
git clone <repo> && cd <repo>

# 1) 准备环境变量（含密钥，全部必填，没有默认值）
cp .env.example .env
vi .env
#   至少填写：MYSQL_PASSWORD / MYSQL_ROOT_PASSWORD / REDIS_PASSWORD / JWT_SECRET
#   JWT_SECRET 用 `openssl rand -hex 32` 生成 —— 用它可伪造任意用户身份

# 2) 构建并启动
docker compose up -d --build

# 3) 查看状态（等 mysql/app 变成 healthy）
docker compose ps
docker compose logs -f app
```

访问 `http://<服务器IP>:8080`（端口由 `.env` 的 `WEB_PORT` 控制）。

### 端口与暴露面

| 服务 | 端口 | 是否对外 |
|------|------|---------|
| web | `WEB_PORT`（默认 8080）| ✅ 唯一入口 |
| app | 8080 | ❌ 仅 compose 内网 |
| mysql / redis | 3306 / 6379 | ❌ 仅 compose 内网 |

需要直连 API / Swagger / 数据库调试时，在 `docker-compose.yml` 里临时放开对应
`ports:`（文件中已注释标注位置），**排查完务必关掉**。

---

## 3. 数据库初始化与升级

### 首次启动

`sql/init.sql` 被挂载到 `/docker-entrypoint-initdb.d/`，MySQL 在**数据目录为空时**
自动执行它（建表 + 种子数据，全部为 `INSERT IGNORE`，可重复执行）。

### 升级已有数据库

⚠️ **`sql/migrations/` 下的迁移脚本不会自动执行。** MySQL 的 initdb 机制只在
首次初始化时跑一遍，后续启动不会重放。

用内置的迁移执行器（镜像里已包含 `/app/migrate`）：

```bash
# 1) 备份（务必先做）
docker compose exec mysql mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" gin > backup-$(date +%F).sql

# 2) 查看待执行的迁移（只读，不执行任何 SQL）
docker compose exec app /app/migrate -status

# 3) 执行所有未应用的迁移
docker compose exec app /app/migrate

# 4) 重跑 init.sql 补齐数据变更（新增权限码、配置项；全部 INSERT IGNORE）
docker compose exec -T mysql mysql -uroot -p"$MYSQL_ROOT_PASSWORD" gin < sql/init.sql

# 5) 重启应用
docker compose restart app web
```

本机开发时用 `make migrate` / `make migrate-status` 即可（等价于
`go run ./cmd/migrate`）。

**执行器的行为约定：**

- 已应用的版本记在 `schema_migrations` 表，重复执行安全（会跳过）
- **失败即停**：出错的文件不会被标记为已应用，修正脚本后重新执行即可，
  之前成功的不会重跑
- **不做事务回滚**：MySQL 的 DDL 会隐式提交，事务包不住它，加上只会给人
  虚假的安全感。因此迁移脚本**必须幂等**（既有约定：先用 `information_schema`
  判断再操作）
- 文件名必须是 `YYYY-MM-DD-描述.sql`，否则拒绝执行 —— 排序错了会静默跑乱顺序

> 手工方式仍然可用（例如没有 Go 环境、或只想单独跑某一个脚本），
> 但需要自己保证顺序与不重复执行：
>
> ```bash
> docker compose exec -T mysql mysql -uroot -p"$MYSQL_ROOT_PASSWORD" gin \
>     < sql/migrations/2026-09-15-schema.sql
> # 其余脚本按日期顺序依次执行
> ```

迁移脚本都是幂等的（通过 `information_schema` 判断是否已应用），重复执行安全。

> **`2026-09-16-post-tenant.sql` 有一个行为变化需注意**：
> 岗位表由全局表改为租户内表，历史岗位的 `tenant_id` 会被置为 0（平台级）。
> 平台账号仍可见全部岗位，但**各租户账号从此看不到这批历史岗位**。
> 若要让某租户沿用，执行：
> `UPDATE sys_post SET tenant_id = <租户ID> WHERE tenant_id = 0;`

---

## 4. 必须检查的配置

`deploy/config.docker.yaml` 已按容器环境设定，以下几处部署时需确认：

| 配置项 | 容器内取值 | 说明 |
|--------|-----------|------|
| `server.mode` | `release` | **不要改回 debug**：debug 会暴露 Swagger 与 gin 调试输出 |
| `database.host` | `mysql` | compose 服务名 |
| `redis.addr` | `redis:6379` | 同上 |
| `cors.allow_origins` | `[]`（空）| 同源部署不需要跨域。**绝不要填 `*`** —— 配合 `allow_credentials` 等价于对任意站点开放带凭证访问 |
| `log.db_retention_days` | `90` | 操作/登录日志保留天数，`<=0` 表示不清理 |
| `upload.max_size` | `10` | 单位 MB。nginx 侧 `client_max_body_size` 已放到 20m 留余量，真正的闸门在这里 |

密钥不写在配置文件里，由 `.env` → 环境变量注入（`DB_PASSWORD` / `JWT_SECRET` / `REDIS_PASSWORD`）。
`mode=release` 下若 `JWT_SECRET` 仍是默认值或为空，服务会**拒绝启动**（见 `config.ValidateSecurity`）。

### 数据库连接问题排查

`app` 使用 `.env` 里的 `MYSQL_PASSWORD` 连接。若 MySQL 数据卷是**之前用别的密码**
初始化过的，改 `.env` 不会生效（密码只在 initdb 时设置一次），需要：

```bash
# 方案一：改回原密码
# 方案二：重置密码
docker compose exec mysql mysql -uroot -p"<原root密码>" \
    -e "ALTER USER 'gin'@'%' IDENTIFIED BY '<新密码>'; FLUSH PRIVILEGES;"
```

---

## 5. 常用操作

```bash
docker compose ps                      # 状态（关注 healthy）
docker compose logs -f app             # 应用日志
docker compose logs -f web             # nginx 访问日志
docker compose restart app             # 重启后端
docker compose up -d --build app web   # 重新构建并滚动更新
docker compose down                    # 停止（保留数据卷）
docker compose down -v                 # ⚠️ 连同数据卷一起删除，数据不可恢复
```

应用日志同时写入 `app-logs` 卷（`logs/app.log`），可直接查看：

```bash
docker compose exec app tail -f /app/logs/app.log
```

### 备份

```bash
# 数据库
docker compose exec mysql mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" gin > backup.sql
# 上传文件（用户上传的图片/文档）
docker run --rm -v "$(basename $PWD)_app-uploads:/data" -v "$PWD:/backup" alpine \
    tar czf /backup/uploads.tar.gz -C /data .
```

---

## 6. 健康检查

| 端点 | 用途 | 行为 |
|------|------|------|
| `/health` | liveness | 只表示进程存活，**不探测依赖**。依赖抖动时不会触发容器重启 |
| `/health/ready` | readiness | 探测 MySQL 与 Redis，任一不可用返回 **503** |

两者都不需要认证，且**只返回 `ok` / `down`**，不返回具体错误 —— 原始错误会带出
内网地址、库名等拓扑信息。细节在 `app` 日志里。

`docker-compose.yml` 中 `app` 的 healthcheck 用的是 `/health/ready`（deep），
而镜像自带的 `HEALTHCHECK` 用的是 `/health`（liveness），这个区分是刻意的。

---

## 7. 排障

**启动日志里出现 CORS 的 Error**
```
[cors] 生产环境未配置 cors.allow_origins，将拒绝全部跨域请求
```
这是**预期行为**，不是故障：同源部署本就不需要跨域，未配置白名单时
fail-closed 拒绝全部跨域请求。仅当确实要从其他域名直接调 API 时才配置 `allow_origins`。

**`app` 一直不 healthy，日志报 casbin 加载失败**
路径类配置（`casbin.model_path` / `log.filename` / `upload.save_path`）都是
**相对工作目录**的，必须从应用根目录启动。镜像已设 `WORKDIR /app` 并预置
`config/casbin/model.conf`。若自定义了启动方式，务必保证工作目录正确 ——
casbin 加载失败会导致服务**拒绝启动**（这是有意的，避免以"无鉴权"状态对外服务）。

**`app` 报数据库连接失败**
先确认 `mysql` 已 healthy；再确认 `.env` 的 `MYSQL_PASSWORD` 与数据卷初始化时一致（见第 4 节）。

**上传大文件返回 413**
两处限制都要放宽：nginx 的 `client_max_body_size`（`deploy/nginx/default.conf`）
与后端 `upload.max_size`（`deploy/config.docker.yaml`，单位 MB）。

**页面刷新子路由 404**
nginx 的 SPA 回退（`try_files $uri $uri/ /index.html`）被改坏了。
前端用 `createWebHistory()`，没有回退就无法直接访问子路径。

---

## 8. 不用 Docker 部署（systemd 参考）

```ini
# /etc/systemd/system/gin-admin.service
[Unit]
Description=Gin-Admin
After=network.target mysql.service redis.service

[Service]
Type=simple
User=gin-admin
# ⚠️ 必须设置：路径类配置相对工作目录，工作目录错了会拒绝启动
WorkingDirectory=/opt/gin-admin
ExecStart=/opt/gin-admin/go-admin config/config.yaml
Restart=on-failure
RestartSec=5

# 密钥通过环境变量注入，不要写进配置文件
Environment=JWT_SECRET=<强随机值>
Environment=DB_PASSWORD=<数据库密码>
Environment=REDIS_PASSWORD=<Redis密码>

[Install]
WantedBy=multi-user.target
```

编译（任选平台，Go 交叉编译无需目标机装 Go）：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
    -o go-admin ./cmd/server
```

前端单独构建后交给已有 nginx：

```bash
cd web && npm ci && npm run build     # 产物在 web/dist
```

nginx 配置可直接参考 `deploy/nginx/default.conf`（注意把 `app:8080`
改成后端实际地址，如 `127.0.0.1:8080`）。

## 部署文件离线校验

没有 Docker 的机器上也能做静态校验（本仓库 CI 与 `make check` 都会跑这一步）：

```bash
pip install pyyaml          # 仅在本地首次需要
make check-deploy           # 等价于 python3 deploy/validate.py
```

`deploy/validate.py` 覆盖：compose 的 YAML 与引用完整性、build 上下文与 Dockerfile
是否真实存在、Dockerfile 的关键 COPY 源有没有被 `.dockerignore` 排除、
应用配置的关键项（mode / host / cors / 密钥留空）、nginx 的关键行为与**安全响应头**。

其中 nginx 那条断言针对一个真实踩过的缺陷：**nginx 的 `add_header` 不继承上层** ——
某个 location 一旦自己写了 `add_header Cache-Control`，server 级的
`X-Frame-Options` / `X-Content-Type-Options` / `Referrer-Policy` 就全部失效。
`location = /index.html` 正是后台主文档的响应者（`location /` 的 `try_files`
内部重定向到它），丢掉 `X-Frame-Options` 就等于整个后台可以被第三方站点
iframe 嵌套（点击劫持）。

因此安全响应头集中放在 `deploy/nginx/snippets/security-headers.conf`，
**任何自带 `add_header` 的 location 都必须 `include` 它** —— 校验脚本会强制这一点，
且 `web/Dockerfile` 必须把该片段复制进镜像（否则 nginx 启动即失败）。

## 迁移回滚与大表 DDL 规范（P2-2）

### 回滚脚本约定

- 每个迁移**应**配套 `<同版本名>.down.sql`，内容是逆向操作；
  确实不可回滚的（如删列丢数据）在文件头标注「不可回滚，恢复需从备份」。
- `cmd/migrate` 的 loader **显式跳过** `*.down.sql`（有单测钉住）——
  回滚是人工决策，绝不随升级自动执行。手工执行：
  `mysql -u<user> -p <db> < sql/migrations/<版本>.down.sql`
- 样例见 `2026-09-29-pay-order-member-list-index.down.sql`
  （含幂等写法与「回滚代价提示」——回滚前先确认症状确由该迁移引起）。

### 大表 DDL 硬规则（MySQL 5.7，表超 ~10 万行就按此执行）

1. **加索引**：默认 `ALTER TABLE ... ADD INDEX ...`，InnoDB 5.7 走
   `ALGORITHM=INPLACE`（不锁写）。**先在基准库实测**（见
   internal/benchmark），确认优化器真的会用它。
2. **禁用**在大表上直接 `MODIFY COLUMN` 改列类型 —— 那是
   `ALGORITHM=COPY`，锁全表 + 全量重写。等价改法：
   加新列 → 回填 → 切换读写 → 下个版本删旧列。
3. **先加列、再回填、最后改约束**：NOT NULL/唯一约束永远放在
   数据补齐**之后**；回填用分批 `UPDATE ... LIMIT`（每批 1~5 千行，
   批间 sleep），一条 UPDATE 回填百万行 = 长事务 = 主从延迟爆炸。
4. **DDL 无法事务回滚**（隐式提交）：每个脚本必须幂等
   （information_schema 判断 + PREPARE/EXECUTE，见现存迁移），
   失败后**修正脚本重跑**，而不是"手动补一半"。
5. **上线窗口**：任何大表 DDL 都安排在低峰，且执行前确认当天的
   备份已成功（见下节）。

## 备份与恢复（P2-3）

```bash
# 每日备份（cron 建议：02:30，避开 03:00 的日志清理任务）
crontab: 30 2 * * * /path/to/go-admin/scripts/backup.sh >> /var/log/go-admin-backup.log 2>&1
# 可配置环境变量：BACKUP_DIR（默认 runtime/backups）、RETENTION_DAYS（默认 7）、
# MYSQL 容器/本机自适配；脚本自带「产物非空校验」，失败时退出码非 0
# （正好可以接进监控/告警——见下文最低告警集）。
```

**没有恢复演练的备份不算备份**：`scripts/restore.md` 是演练步骤，
每次改备份参数后必须重跑一遍并把耗时记录在文件末尾。

## 最低告警集（P0-2 收尾，口径 2026-09-29：企业微信群机器人）

指标采集（internal/metrics）+ Prometheus 抓取 `server.metrics_port` 后，
加载 `deploy/prometheus/goadmin-alerts.yml` 即获得 4 条告警：

| 告警 | 条件 | 级别 |
|---|---|---|
| GoAdminPaymentNotifyRejected | 回调被拒 10 分钟 >3 次 | critical |
| GoAdminServerErrorRate | 5xx 占比 >5% 持续 5 分钟 | critical |
| GoAdminDBPoolWaiting | 连接池等待 10 分钟 >100 次 | warning |
| GoAdminBackupMissing | 备份缺失/超 25 小时 | critical |

**通知到企业微信**：Prometheus → Alertmanager（route 到 wechat receiver，
配置 corp_id / api_secret / to_party）→ 群机器人。通知通道是部署侧
基础设施，框架不内置发送 —— 换钉钉/邮件只改 Alertmanager receiver。

backup.sh 支持可选 `PUSHGATEWAY_URL` 环境变量：成功后推送
`goadmin_backup_last_success_timestamp_seconds`，供 GoAdminBackupMissing
判定；未配置时该告警退化为 cron 邮件兜底。
