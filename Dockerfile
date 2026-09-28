# syntax=docker/dockerfile:1

# ============================================================
# 后端镜像（多阶段构建）
#
# 构建： docker build -t gin-admin:latest .
# 运行： 见 docker-compose.yml（推荐）或 deploy/README.md
#
# 注意：本镜像**只包含后端**。Go 服务不提供前端页面，
#      前端由 web/Dockerfile 产出的 nginx 镜像承载，并反代 /api 与 /uploads。
# ============================================================

# ---------- 构建阶段 ----------
FROM golang:1.25-alpine AS builder

WORKDIR /src

# 先只拷贝依赖清单：依赖没变时这一层能命中缓存，避免每次改代码都重新拉依赖
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 产出静态二进制，才能跑在无 glibc 的 alpine 上。
# -trimpath 去掉构建机绝对路径；-s -w 去掉符号表与调试信息，显著减小体积。
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/go-admin ./cmd/server

# 同时构建迁移执行器。放进镜像是为了让**容器部署场景**也能用它升级数据库 ——
# 否则升级只能退回「手工逐条 mysql < 文件」，漏执行不会有任何提示。
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/migrate ./cmd/migrate

# ---------- 运行阶段 ----------
FROM alpine:3.20

# ca-certificates：调用支付宝/微信/OSS/COS 等 HTTPS 接口必需，缺了会报 x509 错误。
# tzdata：DSN 使用 loc=Local、日志用本地时间，没有时区库会全部退化成 UTC。
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -u 10001 -G app app

WORKDIR /app

COPY --from=builder /out/go-admin /app/go-admin
COPY --from=builder /out/migrate /app/migrate

# 只分发 casbin 模型，**刻意不 COPY config/config.yaml**。
#
# 原因：config/config.yaml 是本地开发配置，里面带 `mode: debug`、
# 默认 JWT 密钥（change-me-in-production）与默认库密码（123456）。
# 把它烘进镜像的后果是 `docker run -p 8080:8080 gin-admin:latest`
# （不挂 compose 的配置卷）会以 debug 模式启动，而
# `config.ValidateSecurity()` 在非 release 模式下直接 return nil，
# 于是默认密钥被放行 —— 该密钥是公开常量，任何人可据此伪造
# 任意用户（含 admin）的 token。
#
# 现在镜像内**没有**配置文件，启动必须由 volume 挂载注入
# （compose 已挂 deploy/config.docker.yaml）。缺失时进程启动即失败，
# 而不是带病运行。casbin 模型则必须随镜像分发：加载失败会拒绝启动。
COPY config/casbin/ /app/config/casbin/

# sql/ 一并放入，便于在容器内执行初始化与迁移脚本
COPY sql/ /app/sql/

# 路径类配置（log.filename / upload.save_path / casbin.model_path）都是
# **相对工作目录**的（见 AGENTS.md「路径」一节），所以必须从 /app 启动。
# 这几个目录要预先建好并授权，否则写日志、上传文件会失败。
RUN mkdir -p /app/logs /app/uploads /app/runtime/certs \
    && chown -R app:app /app

USER app

EXPOSE 8080

# liveness：只判断进程与 HTTP 服务是否存活（不探测依赖，避免依赖抖动导致容器被反复重启）
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/health || exit 1

# main.go 从 os.Args[1] 读取配置路径，显式传入便于用 volume 覆盖。
#
# ⚠️ 镜像内**不含** config/config.yaml（见上方 COPY 处的说明），
# 因此直接 `docker run` 会以「配置文件不存在」启动失败 —— 这是有意的：
# 必须挂载配置（compose 已挂 deploy/config.docker.yaml），
# 才能避免用开发默认密钥在生产跑起来。
ENTRYPOINT ["/app/go-admin"]
CMD ["config/config.yaml"]
