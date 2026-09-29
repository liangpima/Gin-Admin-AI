-- ============================================================
-- 迁移脚本：pay_order / pay_member 增加「租户 + 状态 + 软删」复合索引
-- 日期：2026-09-29
--
-- 【适用场景】
--   升级**已存在**的数据库。
--   全新部署请直接执行 sql/init.sql，无需本脚本。
--
-- 【为什么改】
--   P2-4 性能审计（十万级数据实测，见 internal/benchmark 与
--   docs/bench-100k.md）发现：pay_order / pay_member 的列表接口
--   「SELECT COUNT(*) + 分页」因 `deleted_at IS NULL` 过滤**回表**，
--   每次请求回表 3 万余行：
--     pay_order  列表整链路 44ms → 3.1ms（14×）
--     pay_member 列表整链路 31ms → 7.9ms（3.9×）
--   新索引 (tenant_id, status, deleted_at) 让 COUNT 变成纯索引覆盖
--   （count 40ms → 2.1ms），且隐含主键后缀 id 使 ORDER BY id DESC
--   免排序。EXPLAIN 前后对比：
--     改前 type=index_merge (intersect 3 索引) rows=12396
--     改后 type=ref key=idx_tenant_status rows=20892 filtered=100.00
--
-- 【同时删除 idx_status】
--   pay_order 的单列 idx_status 在复合索引就位后没有消费方：
--   列表查询走 idx_tenant_status；回调路径的条件更新
--   （order_no = ? AND status = ?）走 uk_order_no 唯一索引。
--   单列 status 只有 3 个取值，选择度极差，留着只会误导优化器。
--   （删除前已核对全仓：无任何查询以 status 为唯一过滤条件访问 pay_order。）
--
-- 【数据风险：无】
--   纯索引变更，不触碰表数据。 pay_member 的 status/level 查询
--   同样受益（idx_level_id 保留：等级过滤是独立入口）。
--
-- 【幂等性】
--   通过 information_schema.STATISTICS / TABLES 判断索引是否存在，
--   重复执行不报错（cmd/migrate 不做事务回滚，DDL 隐式提交）。
-- ============================================================

-- ── pay_order：加复合索引 ──
SET @exists := (SELECT COUNT(*) FROM information_schema.STATISTICS
                WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'pay_order'
                  AND INDEX_NAME = 'idx_tenant_status');
SET @sql := IF(@exists = 0,
  'ALTER TABLE `pay_order` ADD INDEX `idx_tenant_status` (`tenant_id`, `status`, `deleted_at`)',
  'SELECT ''idx_tenant_status 已存在，跳过''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ── pay_order：删除失去消费方的单列 idx_status ──
SET @exists := (SELECT COUNT(*) FROM information_schema.STATISTICS
                WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'pay_order'
                  AND INDEX_NAME = 'idx_status');
SET @sql := IF(@exists > 0,
  'ALTER TABLE `pay_order` DROP INDEX `idx_status`',
  'SELECT ''idx_status 不存在，跳过''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ── pay_member：加复合索引 ──
SET @exists := (SELECT COUNT(*) FROM information_schema.STATISTICS
                WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'pay_member'
                  AND INDEX_NAME = 'idx_tenant_status');
SET @sql := IF(@exists = 0,
  'ALTER TABLE `pay_member` ADD INDEX `idx_tenant_status` (`tenant_id`, `status`, `deleted_at`)',
  'SELECT ''idx_tenant_status 已存在，跳过''');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
