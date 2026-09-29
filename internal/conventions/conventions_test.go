// Package conventions 是**约定强制化**的机械校验（P2-1）。
//
// 13 条铁律里靠人记的那部分，历史上出过真事故：H2（撤销方向无授权收敛）
// 从「发现」到「进入修复计划」之间静默蒸发 —— 因为没有任何东西在检查它。
// 本包把约定变成会红的断言，沿用 layering_test.go 的「先剥注释再扫」模式。
//
// 当前三条（对应 AGENTS.md 规则 5 / 规则 7 / swagger 约定）：
//
//  1. Repository 导出方法必须接收 tenantID（规则 7：漏传 = 静默全表泄漏），
//     白名单见 test 文件内的注释 —— 全局表、关联表、回调专用与全局序列
//  2. Service 函数体内禁止新建 errors.New（规则 5：业务错误必须
//     NewBizError/NewNotFoundError，系统错误必须原样上抛；
//     包级哨兵 var ErrXxx = errors.New(...) 是合法形态）
//  3. Service 函数体内禁止裸 fmt.Errorf 外抛**业务语义**的文案 —— 这条
//     无法机械区分「包装」与「新建」，故只查最明确的违规：`errors.New(`。
//     fmt.Errorf + %w 包装哨兵/原样错误是合法形态（payment 的 ErrRefundPending）
//
// 每条检查都做过变异验证（注入违规 → 必须红）。
package conventions

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot 测试运行目录是 internal/conventions，仓库根在两级之上。
const repoRoot = "../.."

// stripComments 去掉 Go 源码的 // 行注释与 /* */ 块注释。
//
// 不做真正的词法分析，处理三种足以覆盖本仓库的情况：
// 行内字符串里的 "//"（跳过引号内的内容）、行注释、块注释。
// 说明性注释里经常出现被禁模式的示例代码 —— 不剥注释就会满屏误报，
// 然后被人加白名单绕过（layering_test.go 里记过这个教训）。
func stripComments(src string) string {
	var b strings.Builder
	inStr, inChar, inLine, inBlock := false, false, false, false
	for i := 0; i < len(src); i++ {
		c := src[i]
		next := byte(0)
		if i+1 < len(src) {
			next = src[i+1]
		}
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
				b.WriteByte(c)
			}
		case inBlock:
			if c == '*' && next == '/' {
				inBlock = false
				i++
			}
		case inStr:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				b.WriteByte(next)
				i++
			} else if c == '"' {
				inStr = false
			}
		case inChar:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				b.WriteByte(next)
				i++
			} else if c == '\'' {
				inChar = false
			}
		case c == '/' && next == '/':
			inLine = true
		case c == '/' && next == '*':
			inBlock = true
			i++
		case c == '"':
			inStr = true
			b.WriteByte(c)
		case c == '\'':
			inChar = true
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// goFiles 递归收集 dir 下的 .go 文件（可带排除谓词），返回相对 repoRoot 的路径。
func goFiles(t *testing.T, dir string, keep func(relPath string) bool) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(filepath.Join(repoRoot, dir), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if keep != nil && !keep(rel) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s 失败: %v", dir, err)
	}
	if len(out) == 0 {
		t.Fatalf("在 %s 下扫不到任何非测试 Go 文件 —— 目录 moved/renamed？断言形同虚设", dir)
	}
	return out
}

// ─────────────────────────── 检查 1：Repository 必须带 tenantID ───────────────────────────

// methodRe 匹配仓储导出方法的签名行。
var methodRe = regexp.MustCompile(`^func \(\w+ \*(\w+Repository)\) ([A-Z]\w*)\(([^)]*)\)`)

// tenantExemptFiles 整文件豁免：**全局表**（无 tenant_id 列）与**纯关联表**
// 的仓储，见 AGENTS.md 规则 7 的两张清单。改表性质时必须同步这里。
var tenantExemptFiles = map[string]bool{
	// 全局表（BaseModel，无 tenant_id 列）
	"internal/module/system/repository/menu_repository.go":   true,
	"internal/module/system/repository/config_repository.go": true,
	"internal/module/system/repository/dict_repository.go":   true,
	"internal/module/system/repository/tenant_repository.go": true,
	"internal/module/system/repository/log_repository.go":    false, // 日志表有 tenant_id，不豁免
	// 纯关联表（无 tenant_id 列，归属校验在 Service 层）
	"internal/module/member/repository/member_tag_repository.go": false, // 有 tenant_id 校验，不豁免
}

// tenantExemptMethods 方法级豁免（文件路径 → 方法名集合），每条必须带理由。
// 加条目前先想清楚：是「真的不该有租户」，还是「忘了传」——
// 后者正是本检查要抓的缺陷。
var tenantExemptMethods = map[string]map[string]string{
	"internal/module/member/repository/member_repository.go": {
		// FindMaxMemberNo 全平台一个序列发号（uk_member_no 全局唯一，
		// 见该方法的注释与 §P2-1d 的产品口径）—— 租户过滤是错的
		"FindMaxMemberNo": "会员编号由全平台共用序列发出，加租户过滤是缺陷",
	},
	"internal/module/payment/repository/pay_order_repository.go": {
		// 以下全部按**全局唯一** uk_order_no 定位（回调无租户上下文，
		// 条件更新按订单号抢占；规则 7 明文豁免）
		"FindByOrderNoForNotify": "支付回调无租户上下文，按全局唯一订单号",
		"MarkPaidIfPending":      "同上：回调幂等条件更新",
		"ClaimRefund":            "退款抢占：按全局唯一订单号的条件更新",
		"UpdateRefund":           "退款落定：同上",
		"ReleaseRefundClaim":     "退款抢占释放：同上",
	},
	"internal/module/system/repository/user_repository.go": {
		// uk_username 全局唯一（init.sql），登录时租户未知
		"FindByUsernameForAuth": "登录按全局唯一 username 定位（uk_username）",
		"CountByUsername":       "同上：查重依据是全局唯一的 username",
		// sys_user_role / sys_user_post 是纯关联表（无 tenant_id 列），
		// 归属校验在 Service 层做（规则 7）；机械校验只看签名
		"ReplaceRoles":         "纯关联表写入，归属校验在 Service（规则 7）",
		"ReplacePosts":         "同上",
		"FindRoleIDsByUserID":  "纯关联表读取",
		"FindRoleIDsByUserIDs": "同上（批量变体）",
	},
	"internal/module/system/repository/role_repository.go": {
		"CountByCode":              "uk_code 全局唯一（init.sql），查重按全局",
		"FindPermissionsByRoleIDs": "sys_role_menu 纯关联表读取（规则 7）",
	},
	"internal/module/system/repository/log_repository.go": {
		// 日志保留期清理任务：凌晨 3 点按时间**刻意跨租户**删除，
		// 租户维度缺失是设计而非缺陷
		"DeleteOperationLogsBefore": "保留期清理任务，刻意跨租户",
		"DeleteLoginLogsBefore":     "同上",
		// CreateOperationLog / CreateLoginLog 的租户在实体里
		//（OperationLog 中间件从 Auth 上下文取租户后填进结构体），
		// 与全仓 Create 同一模式 —— 但方法名不是 Create，需显式豁免
		"CreateOperationLog": "租户在实体里（中间件填充），同 Create 模式",
		"CreateLoginLog":     "同上",
	},
}

// TestRepositoriesTakeTenantID 规则 7 的机械版：
// 仓储的导出方法签名必须接收 tenantID，豁免只允许出现在白名单里。
func TestRepositoriesTakeTenantID(t *testing.T) {
	files := goFiles(t, "internal/module", func(rel string) bool {
		return strings.Contains(rel, "/repository/")
	})

	var violations []string
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatalf("读 %s 失败: %v", rel, err)
		}
		if tenantExemptFiles[rel] {
			continue
		}
		src := stripComments(string(raw))
		for _, line := range strings.Split(src, "\n") {
			m := methodRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			method, params := m[2], m[3]
			if strings.Contains(params, "tenantID") {
				continue
			}
			if strings.Contains(method, "ForNotify") {
				continue // 回调专用：规则 7 明文豁免
			}
			// 模式级豁免（带理由，非白名单式的无脑跳过）：
			switch method {
			case "Create":
				// Create 的租户在**实体内部**（Service 层 item.TenantID = tenantID），
				// 这是全仓统一模式；签名带 tenantID 反而是冗余
				continue
			case "Transaction":
				// 事务基础设施 helper（fn 回调），不触任何表
				continue
			}
			if reason, ok := tenantExemptMethods[rel][method]; ok {
				_ = reason
				continue
			}
			violations = append(violations,
				fmt.Sprintf("%s: %s(...) 缺少 tenantID 参数 —— 漏传会让 TenantScope 退化成全表查询（规则 7）", rel, method))
		}
	}
	if len(violations) > 0 {
		t.Errorf("发现 %d 处仓储方法缺 tenantID：\n%s\n\n若属合法豁免（全局表/关联表/回调/全局序列），"+
			"请加进本文件的白名单并写明理由；若是漏传，修复签名。", len(violations), strings.Join(violations, "\n"))
	}
}

// ─────────────────────────── 检查 2：Service 禁止内联 errors.New ───────────────────────────

// sentinelRe 包级哨兵声明（合法形态）。
var sentinelRe = regexp.MustCompile(`^var Err\w+ = errors\.New\(`)

// TestServicesDoNotCreateInlineErrors 规则 5 的机械版：
// Service 函数体内不得新建 errors.New —— 业务错误必须 NewBizError /
// NewNotFoundError（对外 400/404 + 可读文案），系统错误必须原样上抛
// （对外 500 + 通用文案）。裸 errors.New 两头都不占：文案进得了日志、
// 状态码却永远是 500。
//
// 合法形态：包级哨兵 `var ErrXxx = errors.New(...)`（供调用方 errors.Is），
// 如 payment 的 ErrRefundPending。
func TestServicesDoNotCreateInlineErrors(t *testing.T) {
	files := goFiles(t, "internal/module", func(rel string) bool {
		return strings.Contains(rel, "/service/")
	})

	var violations []string
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatalf("读 %s 失败: %v", rel, err)
		}
		src := stripComments(string(raw))
		for i, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if sentinelRe.MatchString(trimmed) {
				continue
			}
			if strings.Contains(line, "errors.New(") {
				violations = append(violations,
					fmt.Sprintf("%s:%d: 函数体内新建 errors.New —— 业务错误用 common.NewBizError/NewNotFoundError，系统错误原样上抛（规则 5）", rel, i+1))
			}
		}
	}
	if len(violations) > 0 {
		t.Errorf("发现 %d 处 Service 内联 errors.New：\n%s", len(violations), strings.Join(violations, "\n"))
	}
}
