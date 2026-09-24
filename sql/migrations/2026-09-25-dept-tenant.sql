-- ============================================================
-- 迁移脚本：sys_dept 由「全局表」改为「租户内表」
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
--   3. 回填租户归属（两步，见下）
--   4. 同步表注释
--
-- 【为什么要回填，而不是像 sys_post 那样一律留 0】
--   部门与岗位不同：`sys_user.dept_id` 指向部门，而租户账号查询部门时
--   会带上 `tenant_id = <本租户>` 过滤。若所有历史部门都留在 0（平台级），
--   租户账号的部门树会变成**空**，并且无法再给用户选择部门 ——
--   功能直接不可用，而不只是「看不到历史数据」。
--
--   回填分两步：
--     第一步 按用户归属推断：某部门下的用户全部属于同一个非 0 租户时，
--            该部门归入这个租户。多个租户混用、或只有平台账号的部门不推断。
--     第二步 沿部门树向下传播：父部门已确定租户的，其子部门一并归入 ——
--            否则子部门的父节点不在本租户结果集里，整棵子树会不可达。
--
--   推断不出的部门（含平台自用部门）保持 0 = 平台级，由人工按文件末尾的
--   清单确认后手工分配。
--
-- 【行为变化（务必知悉）】
--   1. 各租户账号从此只能看到 `tenant_id = 本租户` 的部门；
--      历史部门若未被回填（仍是 0），租户账号**看不到它们**。
--   2. 平台级账号（tenant_id=0，如默认 admin）不受影响：0 表示不过滤，
--      仍能看到全部部门。
--   3. 部门树在「父节点被租户过滤掉」时会把子节点提升为顶层节点
--      （见 common.BuildTreeForest），因此不会出现整页空白；
--      代价是这类部门在界面上会平铺，而不是挂在原来的父节点下。
--   4. 新增/修改用户时的 `deptId` 会校验是否属于当前租户
--      （见 userService.normalizeDeptID）：跨租户的部门 ID 会被拒绝，
--      而不是静默写入一个查不到的引用。
--
-- 【可重复执行】
--   脚本通过查询 information_schema 判断，已应用的变更会跳过；
--   回填只作用于 `tenant_id = 0` 的行，重复执行不会覆盖已确定的归属。
--
-- 【执行方式】
--   mysql -h<host> -P<port> -u<user> -p <dbname> < 2026-09-25-dept-tenant.sql
--   或： go run ./cmd/migrate
-- ============================================================

DROP PROCEDURE IF EXISTS `_apply_dept_tenant_migration`;

DELIMITER $$
CREATE PROCEDURE `_apply_dept_tenant_migration`()
BEGIN
    -- 1. 新增 tenant_id。默认 0 = 平台级，先保持现状，随后回填。
    IF NOT EXISTS (SELECT 1 FROM information_schema.COLUMNS
                   WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sys_dept'
                     AND COLUMN_NAME = 'tenant_id') THEN
        ALTER TABLE `sys_dept`
            ADD COLUMN `tenant_id` bigint unsigned DEFAULT 0 COMMENT '租户ID' AFTER `id`;
    END IF;

    -- 2. 租户过滤查询走 idx_tenant_id（与 sys_user / sys_role / sys_post 口径一致）
    IF NOT EXISTS (SELECT 1 FROM information_schema.STATISTICS
                   WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sys_dept'
                     AND INDEX_NAME = 'idx_tenant_id') THEN
        ALTER TABLE `sys_dept` ADD INDEX `idx_tenant_id` (`tenant_id`);
    END IF;

    -- 3. 回填（一）：按用户归属推断。
    --    只处理「该部门下的在职用户全部属于同一个非 0 租户」的情形：
    --      n = 1  → 归属唯一，可以安全推断
    --      t <> 0 → 排除「用户全是平台账号」的部门（它们本就该留在平台级）
    --    刻意排除软删除用户：当前归属才算证据，离职/已删用户的历史部门
    --    会把 n 抬高到 2，从而让本来可以推断的部门落进人工确认清单。
    UPDATE `sys_dept` d
      JOIN (SELECT `dept_id`,
                   MIN(`tenant_id`)         AS t,
                   COUNT(DISTINCT `tenant_id`) AS n
              FROM `sys_user`
             WHERE `dept_id` > 0 AND `deleted_at` IS NULL
             GROUP BY `dept_id`) u
        ON u.`dept_id` = d.`id`
       SET d.`tenant_id` = u.t
     WHERE d.`tenant_id` = 0
       AND u.n = 1
       AND u.t <> 0;

    -- 4. 回填（二）：沿部门树向下传播。
    --    只往下传、不往上推断：父部门若仍是 0（平台级），子部门保持 0，
    --    避免把一个「多租户共用的上级部门」整体划给某一个租户。
    --    循环直到没有新的行被更新（每轮至少确定一行，因此必然终止）。
    SET @_dept_propagated := 1;
    WHILE @_dept_propagated > 0 DO
        UPDATE `sys_dept` c
          JOIN `sys_dept` p ON p.`id` = c.`parent_id`
           SET c.`tenant_id` = p.`tenant_id`
         WHERE c.`tenant_id` = 0
           AND p.`tenant_id` <> 0;
        SET @_dept_propagated := ROW_COUNT();
    END WHILE;

    -- 5. 同步表注释（幂等，可重复执行）
    ALTER TABLE `sys_dept` COMMENT = '部门表（租户内数据）';
END$$

DELIMITER ;

CALL `_apply_dept_tenant_migration`();
DROP PROCEDURE `_apply_dept_tenant_migration`;

-- ============================================================
-- 后续步骤（人工确认，按需执行）
-- ============================================================
-- 1. 查看仍未确定归属的部门（tenant_id 仍为 0 = 平台级）：
--      SELECT id, parent_id, name, status FROM sys_dept
--       WHERE tenant_id = 0 ORDER BY parent_id, id;
--    若某个部门实际属于某租户，手工分配（其子部门会被应用层正常展示）：
--      UPDATE sys_dept SET tenant_id = <租户ID> WHERE id = <部门ID>;
--
-- 2. 排查「部门归属与部门内用户归属不一致」的情况 —— 这类数据在应用层
--    不会报错，只是租户账号看不到该用户（用户列表按租户过滤，部门树按租户过滤，
--    两者不一致时用户会显得「没有部门」）：
--      SELECT d.id AS dept_id, d.name, d.tenant_id AS dept_tenant,
--             u.id AS user_id, u.username, u.tenant_id AS user_tenant
--        FROM sys_dept d
--        JOIN sys_user u ON u.dept_id = d.id AND u.deleted_at IS NULL
--       WHERE d.tenant_id <> u.tenant_id;
--
-- 3. 多租户混用的部门（同一部门下存在不同租户的用户）需要业务判断：
--    要么拆成多个部门，要么确认它属于平台级（保持 tenant_id = 0）：
--      SELECT dept_id, COUNT(DISTINCT tenant_id) AS tenant_kinds,
--             GROUP_CONCAT(DISTINCT tenant_id) AS tenants
--        FROM sys_user
--       WHERE dept_id > 0 AND deleted_at IS NULL
--       GROUP BY dept_id HAVING tenant_kinds > 1;
-- ============================================================
