-- ============================================================
-- 迁移脚本：新增「附件上传」权限，与「附件查看」解耦
-- 日期：2026-09-23
--
-- 【背景】
--   router.go 原先把 POST /system/file/upload 登记为 permFileList
--   （system:file:list，查看权限），没有独立的上传权限码。
--   后果：只被授予「附件查看」的低权角色也能往服务器写文件 ——
--   这是权限设计上的错配，上传属于写操作，不该与查看共用权限。
--
-- 【为什么需要本脚本】
--   init.sql 对 sys_menu / sys_role_menu 都是 INSERT IGNORE，只对新装生效。
--   已存在的库必须显式补记录，否则改成 permFileUpload 之后，
--   **所有非超管角色都会立刻失去上传能力**（超管走 * 通配不受影响）。
--
-- 【授权策略】
--   授予所有「当前已拥有附件管理菜单(id=15)」的角色，
--   而不是只授予 role_id=1 —— 这样升级后各角色的实际能力保持不变，
--   再由管理员按需回收，而不是升级即断功能。
--
-- 【可重复执行】
--   全部 INSERT IGNORE + 基于现有授权的 SELECT，重复执行无副作用。
--
-- 【执行方式】
--   mysql -h127.0.0.1 -P3306 -ugin -p gin < 2026-09-23-file-upload-permission.sql
-- ============================================================

-- 1) 按钮权限记录（挂在菜单 15「附件管理」之下）
INSERT IGNORE INTO `sys_menu`
  (`id`, `parent_id`, `name`, `path`, `component`, `icon`, `title`, `type`, `permission`,
   `sort`, `visible`, `status`, `is_cache`, `is_external`, `create_by`, `update_by`)
VALUES
  (444, 15, 'FileUpload', '', '', '', '上传', 2, 'system:file:upload', 2, 1, 1, 1, 0, 1, 1);

-- 2) 授予所有已拥有附件管理菜单的角色，保持升级前后能力一致
INSERT IGNORE INTO `sys_role_menu` (`role_id`, `menu_id`)
SELECT rm.`role_id`, 444
  FROM `sys_role_menu` rm
 WHERE rm.`menu_id` = 15;

-- 3) 校验
SELECT m.`id`, m.`title`, m.`permission`,
       (SELECT COUNT(*) FROM `sys_role_menu` rm WHERE rm.menu_id = m.`id`) AS 已授权角色数
  FROM `sys_menu` m
 WHERE m.`id` = 444;

-- ⚠️ 执行后需**重启服务**（或在后台随意改动一次角色）以触发 SyncPoliciesFromRoleMenus，
--    让新权限码写入 Casbin 策略，否则改动不会生效。
