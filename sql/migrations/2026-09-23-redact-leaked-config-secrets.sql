-- ============================================================
-- 清理历史上明文落库的配置密钥
--
-- 【背景】
--   operation_log.go 的敏感词片段表此前未覆盖本项目真实的配置项命名：
--   支付模块用的是 pay.alipay_key / pay.wechat_key，归一化后是
--   alipaykey / wechatkey，不含 secret / accesskey / privatekey 任何片段。
--   于是配置批量保存（POST /system/config/batch）时，商户私钥原文被写进
--   sys_operation_log.request_param。词表已修复（新增「末段匹配」规则），
--   本脚本负责清理修复前已经落库的记录。
--
-- 【处理方式】
--   整条 request_param 替换为标记文本，而不是逐字段替换。
--   原因：MySQL 5.7 没有 REGEXP_REPLACE，逐字段替换需要写存储过程遍历
--   JSON 结构，成本与出错风险都更高；而一条含明文私钥的审计记录，
--   其保留价值远低于泄露风险。
--
-- 【为什么要限定 request_url】
--   第一版脚本只在所有接口上按关键字匹配，结果误伤了 6 条创建用户的日志
--   （它们的密码字段早已被正确脱敏成 ******，只是因为正文里恰好出现关键字
--   而被整条抹掉）。漏洞**只存在于配置保存接口**，因此这里限定到该路径，
--   把误伤面收敛为零。
--
-- 【幂等】
--   已按 ****** 脱敏过的记录不会被处理（条件里排除了任何字段已打码的行），
--   替换后的文本也不含触发关键字，重复执行不会命中任何行。
--
-- 【执行方式】
--   mysql -h<host> -P<port> -u<user> -p <dbname> < 2026-09-23-redact-leaked-config-secrets.sql
--   或由 cmd/migrate 自动执行
-- ============================================================

UPDATE `sys_operation_log`
SET `request_param` = '[已脱敏：原记录含明文密钥，已由安全修复清理]'
WHERE `request_url` LIKE '%/system/config/%'
  AND (
        `request_param` LIKE '%alipay_key%'
     OR `request_param` LIKE '%wechat_key%'
     OR `request_param` LIKE '%apiv3_key%'
     OR `request_param` LIKE '%secret_key%'
     OR `request_param` LIKE '%access_key%'
     OR `request_param` LIKE '%cert_pem%'
     OR `request_param` LIKE '%key_pem%'
     OR `request_param` LIKE '%private_key%'
      )
  -- 任何字段只要已被正确脱敏，整条记录就保留：避免损失
  -- 「谁改过哪一项」的审计线索（比只判 value 字段更保守）
  AND `request_param` NOT LIKE '%:"******"%';
