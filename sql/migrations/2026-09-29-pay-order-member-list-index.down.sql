-- ============================================================
-- 回滚脚本：撤销 2026-09-29-pay-order-member-list-index.sql
--
-- 【用途】索引变更导致意外性能回退或优化器行为异常时的**应急退路**。
--   手工执行（cmd/migrate 绝不自动应用 down 脚本，loader 已显式跳过）：
--     mysql -u<user> -p <db_name> < sql/migrations/2026-09-29-pay-order-member-list-index.down.sql
--
-- 【做什么】
--   1. 删除 idx_tenant_status（pay_order / pay_member）
--   2. 恢复原 idx_status（pay_order 单列，保持与加索引前的 schema 一致）
--
-- 【代价提示】
--   回滚后列表接口的 COUNT(*) 将退回回表模式（十万级实测 44ms/31ms）。
--   回滚前请先确认性能回退确实由本索引引起（对比 docs/bench-100k.md）。
--
-- 【幂等】information_schema 判断 + PREPARE/EXECUTE，重复执行不报错。
-- ============================================================

-- 1. 删除 idx_tenant_status
SET @exists := (SELECT COUNT(*) FROM information_schema.STATISTICS
                WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'pay_order'
                  AND INDEX_NAME = 'idx_tenant_status');
SET @sql := IF(@exists > 0,
  'ALTER TABLE `pay_order` DROP INDEX `idx_tenant_status`',
  'SELECT ''pay_order.idx_tenant_status 不存在，跳过''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @exists := (SELECT COUNT(*) FROM information_schema.STATISTICS
                WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'pay_member'
                  AND INDEX_NAME = 'idx_tenant_status');
SET @sql := IF(@exists > 0,
  'ALTER TABLE `pay_member` DROP INDEX `idx_tenant_status`',
  'SELECT ''pay_member.idx_tenant_status 不存在，跳过''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- 2. 恢复原 idx_status
SET @exists := (SELECT COUNT(*) FROM information_schema.STATISTICS
                WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'pay_order'
                  AND INDEX_NAME = 'idx_status');
SET @sql := IF(@exists = 0,
  'ALTER TABLE `pay_order` ADD INDEX `idx_status` (`status`)',
  'SELECT ''pay_order.idx_status 已存在，跳过''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
