# 恢复演练步骤（P2-3）

> **没有恢复演练的备份不算备份。** 每次修改备份参数（库/目录/保留期）
> 之后，必须按本文件完整跑一遍演练，并把「演练记录」追加到文件末尾。

## 演练目标

1. 备份产物能**完整恢复到一个空库**
2. 恢复后的库通过 `migrate -status`（无待执行、无缺失）
3. 记录恢复耗时（决定 RTO 的唯一依据）

## 步骤

```bash
# 0) 准备：确认最新备份产物存在且 >1KB
ls -la runtime/backups/ | tail -3

# 1) 建一个全新的演练库（绝不能直接恢复到生产/开发库）
mysql -uroot -p -e "CREATE DATABASE go_restore_drill DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;"

# 2) 恢复（计时）
time gunzip < runtime/backups/gin-<时间戳>.sql.gz \
  | sed "s/\`gin\`/\`go_restore_drill\`/g" \
  | mysql -uroot -p go_restore_drill
#   ↑ 备份里带 CREATE DATABASE/USE `gin`，sed 改名恢复到演练库

# 3) 校验行数（应与生产/开发库同量级）
mysql -uroot -p go_restore_drill -e "
  SELECT 'pay_member', COUNT(*) FROM pay_member
  UNION ALL SELECT 'pay_order', COUNT(*) FROM pay_order
  UNION ALL SELECT 'sys_operation_log', COUNT(*) FROM sys_operation_log;"

# 4) 迁移状态一致性
mysql -uroot -p go_restore_drill < sql/init.sql   # 幂等，应无 ERROR
go run ./cmd/migrate -status                       # 应显示全部已应用

# 5) 应用烟测（可选但推荐）：起服务连演练库，登录一次 + 拉一次列表
#    这能抓住「备份里缺了某张表/数据」这类纯行数校验漏掉的问题

# 6) 清理
mysql -uroot -p -e "DROP DATABASE go_restore_drill;"
```

## 演练记录（追加）

| 日期 | 备份产物 | 恢复耗时 | 行数校验 | migrate -status | 备注 |
|---|---|---|---|---|---|
| 2026-09-29 | go_bench-20260929-135421.sql.gz（4.2MB，10 万行×3 表） | **13.8s** | ✅ 3×100000 | ✅ | 首次演练 |

### 2026-09-29 首次演练记录

- 源库：go_bench（P2-4 基准库，pay_member / pay_order / sys_operation_log 各 10 万行）
- 恢复方式：`mysqldump` 产物 gunzip 后经 sed 改库名导入新库 go_restore_drill
- 耗时：**备份 ~5s、恢复 13.8s**（30 万行 + 全部索引重建）——
  **RTO 基线 <1 分钟**，据此把备份频率定为每日（最坏丢一天数据）是可接受的口径
- 校验：三表行数与源库一致；init.sql 幂等重放 rc=0、数据无损
- 结论：产物可恢复，流程可用；演练库已清理
