#!/usr/bin/env bash
# ============================================================
# 数据库每日备份（P2-3）
#
# 用法（cron 建议 02:30，避开 03:00 的日志清理任务）：
#   30 2 * * * /path/to/go-admin/scripts/backup.sh >> /var/log/go-admin-backup.log 2>&1
#
# 环境变量（均可覆盖，默认值对齐 config/config.yaml 的 database 段）：
#   BACKUP_DIR     备份输出目录（默认 <仓库根>/runtime/backups，自动创建；
#                  runtime/ 已被 .gitignore 排除，不会污染仓库）
#   RETENTION_DAYS 保留天数（默认 7；到期文件自动删除）
#   DB_HOST/DB_PORT/DB_USER/DB_PASS/DB_NAME  连接信息
#   MYSQLDUMP/MYSQL 客户端可执行文件路径（自动探测 phpstudy / PATH / 容器）
#
# 行为约定：
#   - 产物为 gzip 压缩的 SQL 文件，文件名带时间戳
#   - **产物非空校验**：空产物视为失败，退出码 1（接监控/告警的钩子）
#   - 失败退出码非 0：cron 重定向的日志里 grep "backup" 即可发现
#   - 恢复步骤见 scripts/restore.md —— 改备份参数后必须重跑一次演练
#
# 本脚本 LF 行尾（bash on CI/Linux 直接可跑）；Windows 下经 Git Bash 执行。
# ============================================================
set -euo pipefail

# ── 定位仓库根（脚本可能在 cron 里从任意 cwd 调用）──
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

BACKUP_DIR="${BACKUP_DIR:-$REPO_ROOT/runtime/backups}"
RETENTION_DAYS="${RETENTION_DAYS:-7}"
DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-3306}"
DB_USER="${DB_USER:-root}"
DB_PASS="${DB_PASS:-123456}"
DB_NAME="${DB_NAME:-gin}"

TS="$(date +%Y%m%d-%H%M%S)"
OUT="$BACKUP_DIR/${DB_NAME}-${TS}.sql.gz"

# ── 探测 mysqldump ──
find_mysqldump() {
  if [ -n "${MYSQLDUMP:-}" ] && [ -x "$MYSQLDUMP" ]; then echo "$MYSQLDUMP"; return; fi
  if command -v mysqldump >/dev/null 2>&1; then command -v mysqldump; return; fi
  for p in /i/phpstudy_pro/Extensions/MySQL5.7.26/bin/mysqldump.exe \
           /c/phpstudy_pro/Extensions/MySQL5.7.26/bin/mysqldump.exe; do
    [ -x "$p" ] && { echo "$p"; return; }
  done
  echo ""
}

DUMP="$(find_mysqldump)"
if [ -z "$DUMP" ]; then
  echo "[backup] FAIL: 找不到 mysqldump（设 MYSQLDUMP 环境变量指定路径）"
  exit 1
fi

mkdir -p "$BACKUP_DIR"

echo "[backup] $TS 开始备份 $DB_NAME -> $OUT"
# --single-transaction：InnoDB 一致性快照，不锁写（MyISAM 表不存在于本 schema）
# --routines/--triggers： schema 无存储过程，带上以防将来加
# --set-gtid-purged=OFF：5.7 未开 GTID 时该参数报错，故不传（本地部署常态）
if ! "$DUMP" --single-transaction --quick \
      -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" ${DB_PASS:+"-p$DB_PASS"} \
      "$DB_NAME" | gzip > "$OUT"; then
  echo "[backup] FAIL: mysqldump 执行失败"
  rm -f "$OUT"
  exit 1
fi

# ── 产物非空校验（空产物 = 没备份，比没有更危险）──
SIZE=$(stat -c%s "$OUT" 2>/dev/null || wc -c < "$OUT" | tr -d ' ')
if [ "${SIZE:-0}" -lt 1024 ]; then
  echo "[backup] FAIL: 产物仅 ${SIZE:-0} 字节（<1KB），疑似空库或导出失败"
  exit 1
fi

echo "[backup] OK: $OUT（$SIZE 字节）"

# ── 备份成功时间戳上报（可选：设 PUSHGATEWAY_URL 后生效）──
# 供 Prometheus 的 GoAdminBackupMissing 告警（deploy/prometheus/goadmin-alerts.yml）
# 判断「最近一次成功备份」—— 未配置则告警退化为 cron 邮件兜底
if [ -n "${PUSHGATEWAY_URL:-}" ]; then
  printf 'goadmin_backup_last_success_timestamp_seconds %s\n' "$(date +%s)" |
    curl -s --max-time 10 --data-binary @- "$PUSHGATEWAY_URL/metrics/job/backup/instance/$(hostname)" \
    && echo "[backup] 已上报备份时间戳到 Pushgateway"
fi

# ── 保留期清理 ──
if [ "$RETENTION_DAYS" -gt 0 ]; then
  find "$BACKUP_DIR" -name "${DB_NAME}-*.sql.gz" -mtime +"$RETENTION_DAYS" -print -delete |
    while read -r f; do echo "[backup] 清理过期备份: $f"; done
fi
