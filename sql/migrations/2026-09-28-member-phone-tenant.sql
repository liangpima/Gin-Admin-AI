-- ============================================================
-- 迁移脚本：pay_member 手机号由「全平台唯一」改为「租户内唯一」
-- 日期：2026-09-28
--
-- 【适用场景】
--   升级**已存在**的数据库。
--   全新部署请直接执行 sql/init.sql，无需本脚本。
--
-- 【为什么需要本脚本】
--   init.sql 使用 `CREATE TABLE IF NOT EXISTS`，对已存在的表**完全不改动**
--   （含列与索引）。因此唯一索引范围的调整必须显式执行 ALTER。
--
-- 【本脚本做什么】
--   唯一索引 `uk_phone(phone)` → `uk_tenant_phone(tenant_id, phone)`。
--
-- 【为什么改】
--   会员不是平台级实体。手机号是租户内的业务标识，应用层的查重
--   （`memberRepository.FindByPhone(tenantID, phone)`）一直按租户过滤，
--   而索引却是全局的 —— 两边语义不一致，表现为**校验通过却插入报 1062**：
--     租户 A 已有手机号 X → 租户 B 用 X 建会员 → 按 tenant_id=B 查重查不到
--     → INSERT 撞全局 uk_phone → 1062 → 对外 500。
--   与 2026-09-16 的 sys_post.uk_code → uk_tenant_code 是同一类修复。
--
-- 【数据风险：无】
--   这是**放宽**约束（全局唯一 ⊇ 租户内唯一），原本就满足全局唯一的存量数据
--   必然满足 (tenant_id, phone) 唯一，不可能出现建索引失败。
--   反过来（租户内 → 全局）才需要先去重，本次不属于这种情况。
--   因此**不需要**任何回填或去重步骤。
--
-- 【刻意不动的地方】
--   `uk_member_no(member_no)` 保持**全平台**唯一。会员编号由全平台共用的
--   序列发出（见 `memberRepository.FindMaxMemberNo` 的说明），改编号范围必须
--   连同 `memberService.generateMemberNo` 的 Redis 键与推导逻辑一起改，
--   属于产品决策。两个索引范围不同是刻意为之，不是漏改。
--
-- 【可重复执行】
--   脚本通过查询 information_schema 判断，已应用的变更会跳过。
--
-- 【执行方式】
--   mysql -h<host> -P<port> -u<user> -p <dbname> < 2026-09-28-member-phone-tenant.sql
-- ============================================================

DROP PROCEDURE IF EXISTS `_apply_member_phone_tenant_migration`;

DELIMITER $$
CREATE PROCEDURE `_apply_member_phone_tenant_migration`()
BEGIN
    -- 1. 先建新索引再删旧索引，避免中间出现「手机号无任何唯一约束」的时间窗口
    --    （那段时间并发插入可以写进两条同租户同手机号的记录，之后新索引建不上）。
    IF NOT EXISTS (SELECT 1 FROM information_schema.STATISTICS
                   WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'pay_member'
                     AND INDEX_NAME = 'uk_tenant_phone') THEN
        ALTER TABLE `pay_member`
            ADD UNIQUE KEY `uk_tenant_phone` (`tenant_id`, `phone`);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.STATISTICS
               WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'pay_member'
                 AND INDEX_NAME = 'uk_phone') THEN
        ALTER TABLE `pay_member` DROP INDEX `uk_phone`;
    END IF;

    -- 2. 同步表注释（幂等）
    ALTER TABLE `pay_member` COMMENT = '会员表（手机号租户内唯一，会员编号全平台唯一）';
END$$

DELIMITER ;

CALL `_apply_member_phone_tenant_migration`();
DROP PROCEDURE `_apply_member_phone_tenant_migration`;

-- ============================================================
-- 后续核查
-- ============================================================
-- 1. 确认索引已生效（应只剩 uk_tenant_phone 与 uk_member_no 两个唯一索引）：
--      SHOW INDEX FROM pay_member WHERE Non_unique = 0;
--
-- 2. 确认没有跨租户的重复手机号被「意外放行」——
--    正常情况下这条查询返回 0 行（存量数据来自全局唯一索引，不可能有重复）：
--      SELECT phone, COUNT(*) AS cnt
--        FROM pay_member
--       WHERE deleted_at IS NULL AND phone <> ''
--       GROUP BY tenant_id, phone
--      HAVING cnt > 1;
--    若有返回，说明数据是在索引缺失的窗口期写入的，需人工确认保留哪一条。
--
-- 3. uk_member_no 有意保持全平台唯一，不在本脚本范围内。
-- ============================================================
