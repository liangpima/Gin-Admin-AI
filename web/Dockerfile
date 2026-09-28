# syntax=docker/dockerfile:1

# ============================================================
# 前端镜像（Vue3 构建产物 + nginx）
#
# 构建上下文必须是**仓库根目录**（本文件需要引用 deploy/nginx/default.conf）：
#   docker build -f web/Dockerfile -t gin-admin-web:latest .
#
# 为什么用 nginx 而不是让 Go 服务托管前端：
#   Go 服务只注册了 /api/v1、/uploads、/swagger、/health 等路由，
#   本身不提供 SPA 页面。前端 baseURL 是 /api/v1（同源相对路径），
#   因此由 nginx 托管静态资源并反代 /api 与 /uploads，即构成完整站点。
# ============================================================

# ---------- 构建阶段 ----------
FROM node:22-alpine AS builder

WORKDIR /src

# package-lock.json 存在，用 npm ci 以获得可复现的依赖树（严格按 lock 安装）
COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./

# npm run build = vue-tsc && vite build
# 其中 vue-tsc 是类型检查：类型错误会让构建失败，这是前端唯一的验证途径
RUN npm run build

# ---------- 运行阶段 ----------
FROM nginx:1.27-alpine

# 覆盖默认站点配置：SPA history 回退 + /api 与 /uploads 反代
COPY deploy/nginx/default.conf /etc/nginx/conf.d/default.conf

# 安全响应头片段。default.conf 里凡是自带 add_header 的 location 都会
# `include /etc/nginx/snippets/security-headers.conf;`（nginx 的 add_header
# 不继承上层，逐处 include 才能保住 X-Frame-Options 等头）。
# **漏掉这一行会让 nginx 启动直接失败**（include 找不到文件），
# 好在是启动期就炸而不是静默失效。
COPY deploy/nginx/snippets/security-headers.conf /etc/nginx/snippets/security-headers.conf

COPY --from=builder /src/dist /usr/share/nginx/html

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1/ >/dev/null || exit 1
