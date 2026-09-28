"""离线校验部署文件。

本环境没有 Docker，无法真正构建镜像，因此这里做能做到的最强静态校验：
  1. YAML 语法
  2. compose 的引用完整性（depends_on 指向的服务、声明的 named volume）
  3. build 上下文与 Dockerfile 是否真实存在
  4. Dockerfile 里 COPY 的源路径在构建上下文中是否存在（含 .dockerignore 排除项）
"""

import os
import re
import sys

try:
    import yaml
except ImportError:
    sys.exit(
        "需要 PyYAML 解析 YAML：\n"
        "    pip install pyyaml\n"
        "（或 python -m pip install pyyaml）"
    )

ROOT = os.path.dirname(os.path.abspath(__file__))
os.chdir(os.path.dirname(ROOT))  # 切到仓库根目录

errors: list[str] = []
checks = 0


def fail(msg: str) -> None:
    errors.append(msg)


# ---------- 1. YAML 语法 ----------
compose_path = "docker-compose.yml"
config_path = "deploy/config.docker.yaml"

with open(compose_path, encoding="utf-8") as f:
    compose = yaml.safe_load(f)
checks += 1
print(f"[OK] YAML 解析: {compose_path}")

with open(config_path, encoding="utf-8") as f:
    app_cfg = yaml.safe_load(f)
checks += 1
print(f"[OK] YAML 解析: {config_path}")

# ---------- 2. compose 结构 ----------
services = compose.get("services") or {}
if not services:
    fail("docker-compose.yml 没有 services")
checks += 1

declared_volumes = set((compose.get("volumes") or {}).keys())

for name, svc in services.items():
    # depends_on 引用的服务必须存在
    dep = svc.get("depends_on")
    if isinstance(dep, dict):
        for target in dep:
            if target not in services:
                fail(f"services.{name}.depends_on 指向不存在的服务: {target}")
    elif isinstance(dep, list):
        for target in dep:
            if target not in services:
                fail(f"services.{name}.depends_on 指向不存在的服务: {target}")

    # named volume 必须先声明
    for vol in svc.get("volumes") or []:
        if not isinstance(vol, str):
            continue
        src = vol.split(":")[0]
        if src.startswith((".", "/", "~")):
            if not os.path.exists(src):
                fail(f"services.{name} 挂载的宿主机路径不存在: {src}")
        elif src not in declared_volumes:
            fail(f"services.{name} 使用了未在顶层 volumes 声明的卷: {src}")

    # build 上下文与 Dockerfile
    build = svc.get("build")
    if isinstance(build, dict):
        ctx = build.get("context", ".")
        df = build.get("dockerfile", "Dockerfile")
        if not os.path.isdir(ctx):
            fail(f"services.{name} 的 build.context 不存在: {ctx}")
        df_path = os.path.join(ctx, df) if ctx != "." else df
        if not os.path.exists(df_path):
            fail(f"services.{name} 的 Dockerfile 不存在: {df_path}")

checks += 1
print(f"[OK] compose 引用完整性: {len(services)} 个服务, {len(declared_volumes)} 个命名卷")

# ---------- 3. .dockerignore 排除规则 ----------
ignored: set[str] = set()
with open(".dockerignore", encoding="utf-8") as f:
    for line in f:
        line = line.strip()
        if line and not line.startswith("#"):
            ignored.add(line)
checks += 1
print(f"[OK] .dockerignore: {len(ignored)} 条规则")

# ---------- 4. Dockerfile 的 COPY 源路径 ----------
# 只做「源路径是否被 .dockerignore 排除」这一关键校验：
# 排除掉后端编译需要的 docs/ 会直接导致 go build 失败，是易犯且难察觉的错误。
CRITICAL_SOURCES = {
    "docs": "cmd/server/main.go 里 `_ \"go-admin/docs\"` 依赖该包",
    "config": "casbin.model_path 与 config.yaml 均在此目录",
    "sql": "Dockerfile 显式 COPY sql/",
}

for src, why in CRITICAL_SOURCES.items():
    if not os.path.exists(src):
        fail(f"关键路径缺失: {src}（{why}）")
        continue
    for rule in ignored:
        if rule.rstrip("/") == src or rule == f"{src}/":
            fail(f"Dockerfile 需要 {src}/，但它被 .dockerignore 的规则 {rule!r} 排除了 —— {why}")
checks += 1
print(f"[OK] Dockerfile 关键 COPY 源未被排除: {', '.join(CRITICAL_SOURCES)}")

# ---------- 5. 应用配置的关键项 ----------
if app_cfg.get("server", {}).get("mode") != "release":
    fail("deploy/config.docker.yaml 的 server.mode 应为 release（否则暴露 Swagger 与调试信息）")

if app_cfg.get("database", {}).get("host") != "mysql":
    fail("deploy/config.docker.yaml 的 database.host 应为 compose 服务名 mysql")

if app_cfg.get("redis", {}).get("addr") != "redis:6379":
    fail("deploy/config.docker.yaml 的 redis.addr 应为 redis:6379")

if app_cfg.get("cors", {}).get("allow_origins"):
    fail("deploy/config.docker.yaml 的 cors.allow_origins 应为空（同源部署不需要跨域）")

if app_cfg.get("cors", {}).get("allow_credentials") and "*" in (app_cfg["cors"].get("allow_origins") or []):
    fail("allow_credentials 与通配来源 '*' 不能同时启用")

# 敏感项不应在配置文件里写死
for key, val in [
    ("jwt.secret", app_cfg.get("jwt", {}).get("secret")),
    ("database.password", app_cfg.get("database", {}).get("password")),
    ("redis.password", app_cfg.get("redis", {}).get("password")),
]:
    if val:
        fail(f"deploy/config.docker.yaml 的 {key} 不应写死，应由环境变量注入")
checks += 1
print("[OK] 应用配置关键项（mode/host/addr/cors/密钥留空）")

# ---------- 6. nginx 配置的关键行为 ----------
nginx = open("deploy/nginx/default.conf", encoding="utf-8").read()
for needle, why in [
    ("try_files $uri $uri/ /index.html", "SPA history 模式必须回退 index.html"),
    ("location /api/", "前端 baseURL 是 /api/v1，必须反代 /api"),
    ("location /uploads/", "上传文件必须反代到后端"),
    ("proxy_pass         http://app:8080", "/api 反代目标应为 app:8080"),
]:
    if needle not in nginx:
        fail(f"deploy/nginx/default.conf 缺少必需配置 {needle!r} —— {why}")

# /uploads 不应被 root/alias 直接映射，否则绕过后端 UploadSecurity() 的 CSP 沙箱响应头
uploads_block = re.search(r"location /uploads/\s*\{(.*?)\n    \}", nginx, re.S)
if uploads_block and re.search(r"\b(root|alias)\b", uploads_block.group(1)):
    fail("deploy/nginx/default.conf 的 /uploads 直接映射了磁盘目录 —— 会绕过 UploadSecurity() 的 CSP 沙箱，.svg 变成存储型 XSS 落点")
checks += 1
print("[OK] nginx 关键行为（SPA 回退 / 反代 / uploads 未直接映射）")

# ---------- 7. nginx 安全响应头：add_header 不继承，必须逐处 include ----------
# nginx 的规则是「本层只要出现一条 add_header，就完全不继承上层的 add_header」。
# 于是某个 location 为了写 Cache-Control 而加了一条 add_header，就会**静默丢掉**
# server 级的 X-Frame-Options / X-Content-Type-Options / Referrer-Policy。
#
# 这不是假设，是本项目真实踩过的缺陷：`location = /index.html` 带了 Cache-Control，
# 而它正是后台主文档的响应者（`location /` 的 try_files 内部重定向到它），
# 结果整个后台可以被第三方站点 iframe 嵌套（点击劫持）；`location /assets/` 同样丢头。
# 而 `location /api/`、`location /` 因为没写 add_header 一直正常 —— 更容易误判为「没问题」。
#
# 这类失效不报错、不影响功能，只有抓响应头才看得出来，所以只能靠断言兜住。
SECURITY_HEADERS = ["X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"]
SNIPPET_PATH = "deploy/nginx/snippets/security-headers.conf"
SNIPPET_INCLUDE = "/etc/nginx/snippets/security-headers.conf"

if not os.path.exists(SNIPPET_PATH):
    fail(f"缺少安全响应头片段 {SNIPPET_PATH}（default.conf 的 include 会指向它）")
else:
    snippet = open(SNIPPET_PATH, encoding="utf-8").read()
    for header in SECURITY_HEADERS:
        if not re.search(rf"add_header\s+{re.escape(header)}\b", snippet):
            fail(f"{SNIPPET_PATH} 未声明 {header}")
    for line in snippet.splitlines():
        stripped = line.strip()
        # 不带 always 时 nginx 只为 2xx/3xx/204/304 添加响应头 → 4xx/5xx 错误页丢头
        if stripped.startswith("add_header") and "always" not in stripped:
            fail(f"{SNIPPET_PATH} 的 add_header 缺少 always: {stripped!r}（4xx/5xx 响应会丢掉该头）")

# 逐 location 花括号配对，检查「带 add_header 的块是否都 include 了片段」
#
# 扫描前先剥掉**整行注释**：注释文字里也会出现 "location" 这个词
# （本配置的说明就写着「这个 location 是后台主文档的实际响应者」），
# 不剥的话正则会把注释行当成块的起点，块名与块边界都会错 ——
# 那样断言虽然偶尔也能报警，但报的是错误的位置，且可能漏报。
nginx_code = "\n".join(
    line for line in nginx.splitlines() if not line.lstrip().startswith("#")
)

location_re = re.compile(r"location\s+([^{]*?)\{")
missing_include: list[str] = []
for match in location_re.finditer(nginx_code):
    open_brace = match.end() - 1
    depth = 0
    close_brace = len(nginx_code) - 1
    for i in range(open_brace, len(nginx_code)):
        if nginx_code[i] == "{":
            depth += 1
        elif nginx_code[i] == "}":
            depth -= 1
            if depth == 0:
                close_brace = i
                break
    block = nginx_code[open_brace:close_brace]
    if "add_header" in block and SNIPPET_INCLUDE not in block:
        missing_include.append(match.group(1).strip())

if missing_include:
    fail(
        "deploy/nginx/default.conf 中这些 location 自带 add_header 却没有 "
        f"include {SNIPPET_INCLUDE} —— nginx 的 add_header 不继承上层，"
        "会静默丢掉 X-Frame-Options / nosniff / Referrer-Policy（后台可被 iframe 嵌套）："
        + ", ".join(repr(x) for x in missing_include)
    )

# 片段必须真的被复制进镜像，否则 nginx 启动时 include 找不到文件而直接失败
web_dockerfile = open("web/Dockerfile", encoding="utf-8").read()
if f"COPY {SNIPPET_PATH}" not in web_dockerfile:
    fail(f"web/Dockerfile 未 COPY {SNIPPET_PATH} —— nginx 会因 include 找不到文件而启动失败")
checks += 1
print(f"[OK] nginx 安全响应头（片段声明 {len(SECURITY_HEADERS)} 个头 + 所有带 add_header 的 location 均已 include）")

# ---------- 汇总 ----------
print()
if errors:
    print(f"发现 {len(errors)} 个问题：")
    for e in errors:
        print("  -", e)
    sys.exit(1)

print(f"全部通过（{checks} 组校验）")
