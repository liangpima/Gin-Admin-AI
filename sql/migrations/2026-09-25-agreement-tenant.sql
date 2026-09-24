-- ============================================================
-- 迁移脚本：sys_agreement 由「全局表」改为「租户内表」
-- 日期：2026-09-25
--
-- 【适用场景】
--   升级**已存在**的数据库。
--   全新部署请直接执行 sql/init.sql，无需本脚本。
--
-- 【为什么需要本脚本】
--   init.sql 使用 `CREATE TABLE IF NOT EXISTS`，对已存在的表**完全不改动**
--   （含列与索引）。因此新增列、索引调整必须显式执行 ALTER。
--
-- 【本脚本做什么】
--   1. 新增 `tenant_id` 列（默认 0）
--   2. 新增 `idx_tenant_id`
--   3. 同步表注释
--
-- 【为什么不回填】
--   与 sys_dept 不同，协议没有任何「归属线索」可用：
--   sys_agreement 里没有指向用户的列，业务上也无法从内容推断它属于哪个租户。
--   因此历史协议一律保留 `tenant_id = 0`，即**平台级**。
--
-- 【行为变化（务必知悉）】
--   1. 历史协议成为平台级数据。由于 `TenantScope(db, 0)` 表示不过滤，
--      平台级账号（如默认 admin）仍能看到并编辑它们；
--      而各租户账号从此只能看到属于自己租户的协议 —— 历史协议对它们**不可见**。
--   2. 若某个历史协议其实属于某个租户，请手工改归属：
--        UPDATE sys_agreement SET tenant_id = <租户ID> WHERE id = <协议ID>;
--   3. 前台「按类型取协议」（`GET /system/agreement/type/:type`）同样是租户内查询：
--      租户账号取不到协议时该接口返回 404，前端应引导先在该租户下创建协议。
--
-- 【可重复执行】
--   脚本通过查询 information_schema 判断，已应用的变更会跳过。
--
-- 【执行方式】
--   mysql -h<host> -P<port> -u<user> -p <dbname> < 2026-09-25-agreement-tenant.sql
--   或： go run ./cmd/migrate
-- ============================================================

DROP PROCEDURE IF EXISTS `_apply_agreement_tenant_migration`;

DELIMITER $$
CREATE PROCEDURE `_apply_agreement_tenant_migration`()
BEGIN
    -- 1. 新增 tenant_id。默认 0 = 平台级：历史协议仍归平台，租户账号不再看到。
    IF NOT EXISTS (SELECT 1 FROM information_schema.COLUMNS
                   WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sys_agreement'
                     AND COLUMN_NAME = 'tenant_id') THEN
        ALTER TABLE `sys_agreement`
            ADD COLUMN `tenant_id` bigint unsigned DEFAULT 0 COMMENT '租户ID' AFTER `id`;
    END IF;

    -- 2. 租户过滤查询走 idx_tenant_id（与 sys_user / sys_role / sys_dept 口径一致）
    IF NOT EXISTS (SELECT 1 FROM information_schema.STATISTICS
                   WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sys_agreement'
                     AND INDEX_NAME = 'idx_tenant_id') THEN
        ALTER TABLE `sys_agreement` ADD INDEX `idx_tenant_id` (`tenant_id`);
    END IF;

    -- 3. 同步表注释（幂等，可重复执行）
    ALTER TABLE `sys_agreement` COMMENT = '协议管理表（租户内数据）';
END$$

DELIMITER ;

CALL `_apply_agreement_tenant_migration`();
DROP PROCEDURE `_apply_agreement_tenant_migration`;

-- ============================================================
-- 后续步骤（人工确认，按需执行）
-- ============================================================
-- 1. 查看历史协议的归属，把确属某租户的挑出来改掉：
--      SELECT id, tenant_id, type, title, status FROM sys_agreement ORDER BY tenant_id, id;
--      UPDATE sys_agreement SET tenant_id = <租户ID> WHERE id = <协议ID>;
--
-- 2. 确认各租户的协议类型覆盖情况（缺哪种类型，前台取协议就会 404）：
--      SELECT tenant_id, type, COUNT(*) AS cnt FROM sys_agreement
--       WHERE deleted_at IS NULL AND status = 1
--       GROUP BY tenant_id, type ORDER BY tenant_id, type;
-- ============================================================
