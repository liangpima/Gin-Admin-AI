#!/usr/bin/env bash
#
# 在**真实 MySQL** 上验证数据库迁移，而不是只编译一下。
#
# # 它修的是什么
#
# 此前 CI 对迁移只做一件事：`go build ./cmd/migrate` —— 证明它能编译，
# 仅此而已。迁移从没在 CI 上被执行过，幂等性只在本地临时库上人工跑过。
# 而「迁移写错」是生产事故的常见来源：重复建索引（第二遍报 1062）、
# 引用还不存在的列、少判 information_schema 导致重跑失败 ——
# 这些在编译期全都看不出来，要等上线执行时才炸。
#
# # 三层验证
#
#   ① `init.sql` 能在空库上跑通（新部署的起点）
#   ② `cmd/migrate` 能在 init.sql 之后把 10 个迁移全部应用（工具链可用）
#   ③ 应用完再跑一次是 no-op、且把所有 .sql 原样重放 3 轮不报 ERROR（幂等）
#
# ③ 是核心：迁移脚本的硬约束是**必须幂等**（cmd/migrate 明确不做事务回滚，
# 因为 MySQL 的 DDL 会隐式提交，事务包不住它）。幂等性只能靠真的跑第二遍发现。
#
# # 用法
#
#   bash scripts/check-migrations.sh
#
# 环境变量（都有默认值，CI 与本机 phpstudy 都能直接用）：
#   MYSQL_BIN / MYSQL_HOST / MYSQL_PORT / MYSQL_USER / MYSQL_PASSWORD
#   MIGRATION_CHECK_DB      临时库名（会被 DROP 后重建，别指向真实库）
#   MIGRATION_CHECK_ROUNDS  重放轮数（默认 3）
#   SKIP_MIGRATE_TOOL=1     跳过 cmd/migrate 那两层（没有 Go 环境时用）
#
set -euo pipefail

TMPDB="${MIGRATION_CHECK_DB:-gin_migration_check}"
MYSQL_HOST="${MYSQL_HOST:-127.0.0.1}"
MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_USER="${MYSQL_USER:-root}"
MYSQL_PASSWORD="${MYSQL_PASSWORD-123456}"
ROUNDS="${MIGRATION_CHECK_ROUNDS:-3}"

INIT_SQL="sql/init.sql"
MIGRATIONS_DIR="sql/migrations"
BASE_CONFIG="config/config.yaml"
TMP_CONFIG=""

# 安全阀：这个脚本会 DROP 数据库。库名必须带「检查」语义，
# 防止有人把 MIGRATION_CHECK_DB 指到一个真实库上。
case "$TMPDB" in
  *check*|*test*|*tmp*) ;;
  *)
    echo "拒绝执行：MIGRATION_CHECK_DB='$TMPDB' 不像临时库名。" >&2
    echo "本脚本会 DROP 该库，库名必须包含 check / test / tmp。" >&2
    exit 1
    ;;
esac

resolve_mysql() {
  if [ -n "${MYSQL_BIN:-}" ]; then printf '%s' "$MYSQL_BIN"; return 0; fi
  if command -v mysql >/dev/null 2>&1; then printf 'mysql'; return 0; fi
  # 本机 phpstudy 的常见位置（CI 上是 PATH 里的 mysql）
  local c
  for c in /i/phpstudy_pro/Extensions/MySQL*/bin/mysql.exe; do
    [ -x "$c" ] && { printf '%s' "$c"; return 0; }
  done
  return 1
}

if ! MYSQL="$(resolve_mysql)"; then
  echo "找不到 mysql 客户端。设置 MYSQL_BIN 指向它，或把它放进 PATH。" >&2
  exit 1
fi

MYSQL_ARGS=(--host="$MYSQL_HOST" --port="$MYSQL_PORT" --user="$MYSQL_USER"
            --default-character-set=utf8mb4 --connect-timeout=10)
if [ -n "$MYSQL_PASSWORD" ]; then
  MYSQL_ARGS+=("--password=$MYSQL_PASSWORD")
fi

# mysql 会把密码写在命令行上，别让它出现在 CI 日志里
mysql_quiet() { "$MYSQL" "${MYSQL_ARGS[@]}" "$@" 2>&1 | grep -v "Using a password on the command line" || true; }

cleanup() {
  # 清理的两步都刻意兜底成「绝不失败」。
  #
  # 原因是一次真实的踩坑：脚本主体全部通过、最后打印「迁移检查通过」，
  # 退出码却是 1 —— 因为 `mktemp -t` 在 Git Bash 下返回的是带盘符的 Windows
  # 路径（`C:\Users\...\Temp\...`），而本机的安全删除封装**拒绝带盘符的路径**，
  # `rm` 失败；`set -e` 又在 `&&` 右侧触发，cleanup 当场中断，`return 0`
  # 根本没执行，于是 trap 的失败码被当成了脚本的结论。
  #
  # 教训：**清理动作永远不该影响检查结论**。删临时文件失败、DROP 库失败，
  # 都不代表迁移有问题 —— 把它们与结论解耦。
  mysql_quiet -e "DROP DATABASE IF EXISTS \`$TMPDB\`;" >/dev/null 2>&1 || true
  if [ -n "$TMP_CONFIG" ]; then
    rm -f "$TMP_CONFIG" 2>/dev/null || true
  fi
  return 0
}
trap cleanup EXIT

echo "== 迁移检查 =="
echo "客户端:   $MYSQL"
echo "目标:     $MYSQL_USER@$MYSQL_HOST:$MYSQL_PORT/$TMPDB"
echo "重放轮数: $ROUNDS"
echo

# ---------- 等 MySQL 就绪 ----------
# CI 的 service 容器即使 healthcheck 通过，也偶发连接被拒；重试比 sleep 可靠
for i in $(seq 1 30); do
  if "$MYSQL" "${MYSQL_ARGS[@]}" -e "SELECT 1" >/dev/null 2>&1; then break; fi
  if [ "$i" = "30" ]; then
    echo "MySQL 在 30 次重试后仍不可连接（$MYSQL_HOST:$MYSQL_PORT）" >&2
    exit 1
  fi
  sleep 1
done

# ---------- ① init.sql ----------
mysql_quiet -e "DROP DATABASE IF EXISTS \`$TMPDB\`; CREATE DATABASE \`$TMPDB\` DEFAULT CHARACTER SET utf8mb4;"
echo "[1/4] 建库完成"

# init.sql 硬编码了 USE `gin`，替换成临时库名。
# 只替换反引号包裹的形式，避免误伤表名/字段名里的 "gin"。
sed "s/\`gin\`/\`$TMPDB\`/g" "$INIT_SQL" | mysql_quiet --database="$TMPDB" >/tmp/init_apply.log 2>&1 || {
  echo "init.sql 执行失败：" >&2; cat /tmp/init_apply.log >&2; exit 1;
}
echo "[2/4] init.sql 应用成功"

# ---------- ② cmd/migrate 应用全部迁移 ----------
if [ "${SKIP_MIGRATE_TOOL:-0}" != "1" ]; then
  if ! command -v go >/dev/null 2>&1; then
    echo "跳过 cmd/migrate 检查：PATH 上没有 go（设 SKIP_MIGRATE_TOOL=1 可显式跳过）" >&2
  else
    # 用带 -t 的 mktemp：它走 TMPDIR，在 Git Bash 下给出**带盘符的 Windows
    # 路径**，而 `go run ./cmd/migrate` 是原生 Windows 程序，只认这种路径
    # （不带 -t 得到 `/tmp/...`，MSYS 的路径映射对原生程序并不保证生效）。
    # 代价是本地那个 rm 会被安全删除封装拒绝 —— 已由 cleanup 兜底，
    # 删不掉临时文件不影响检查结论。CI 在 Linux 上两者都正常。
    TMP_CONFIG="$(mktemp -t migrate-check-XXXXXX.yaml)"
    # 只改 database: 段内的五个键 —— 不能全局 sed，
    # 因为 redis 段也有 host/password 等同名键（会把 Redis 配置一起改坏）
    awk -v h="$MYSQL_HOST" -v p="$MYSQL_PORT" -v u="$MYSQL_USER" \
        -v pw="$MYSQL_PASSWORD" -v d="$TMPDB" '
      /^database:/       { in_db = 1; print; next }
      /^[^[:space:]]/    { in_db = 0 }
      in_db && /^  host:/     { print "  host: " h;      next }
      in_db && /^  port:/     { print "  port: " p;      next }
      in_db && /^  username:/ { print "  username: " u;  next }
      in_db && /^  password:/ { print "  password: \"" pw "\""; next }
      in_db && /^  dbname:/   { print "  dbname: " d;    next }
      { print }
    ' "$BASE_CONFIG" > "$TMP_CONFIG"

    if ! go run ./cmd/migrate -config "$TMP_CONFIG" > /tmp/migrate_apply.log 2>&1; then
      echo "cmd/migrate 应用迁移失败：" >&2; cat /tmp/migrate_apply.log >&2; exit 1
    fi
    applied_count=$(grep -oE '本次应用了 [0-9]+ 个迁移' /tmp/migrate_apply.log | grep -oE '[0-9]+' || echo 0)
    if [ "$applied_count" = "0" ]; then
      echo "cmd/migrate 一个迁移都没应用 —— 要么迁移目录为空，要么它没连上目标库：" >&2
      cat /tmp/migrate_apply.log >&2; exit 1
    fi
    echo "[3/4] cmd/migrate 应用了 $applied_count 个迁移"

    # 再跑一次必须是 no-op。这是**工具层幂等**：应用过的版本记录在
    # schema_migrations 里，第二次不该再执行任何 SQL。
    if ! go run ./cmd/migrate -config "$TMP_CONFIG" -status > /tmp/migrate_status.log 2>&1; then
      echo "cmd/migrate -status 失败：" >&2; cat /tmp/migrate_status.log >&2; exit 1
    fi
    if ! grep -q "数据库已是最新状态" /tmp/migrate_status.log; then
      echo "应用完迁移后仍有待执行项 —— 版本记录没写对：" >&2
      cat /tmp/migrate_status.log >&2; exit 1
    fi
    echo "     二次执行确认为 no-op（数据库已是最新状态）"
  fi
else
  echo "[3/4] 已按要求跳过 cmd/migrate 检查（SKIP_MIGRATE_TOOL=1）"
fi

# ---------- ③ 原样重放全部 .sql N 轮（幂等性） ----------
# 这一层与 ② 不同：它绕过 schema_migrations，把所有 DDL 再执行一遍，
# 直接检验脚本自身是否幂等（② 只能证明「工具不会重复执行」）。
fail=0
for round in $(seq 1 "$ROUNDS"); do
  for f in "$MIGRATIONS_DIR"/*.sql; do
    [ -e "$f" ] || { echo "迁移目录下没有 .sql 文件" >&2; exit 1; }
    out=$(mysql_quiet --database="$TMPDB" < "$f" | grep -i "error" || true)
    if [ -n "$out" ]; then
      echo "第 $round 轮 $(basename "$f") 报错：" >&2
      echo "$out" >&2
      fail=1
    fi
  done
done

if [ "$fail" != "0" ]; then
  echo >&2
  echo "迁移脚本不幂等。cmd/migrate 明确不做事务回滚（MySQL 的 DDL 会隐式提交），" >&2
  echo "所以每个迁移都必须能重复执行 —— 用 information_schema 判断后再操作。" >&2
  exit 1
fi
echo "[4/4] 全部迁移重放 $ROUNDS 轮无 ERROR（幂等）"

echo
echo "迁移检查通过。"
