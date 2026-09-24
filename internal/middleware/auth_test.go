package middleware

import "testing"

// TestCanAccessWithoutTenant 平台级身份的判定策略（P1-3）。
//
// 背景：`TenantScope(db, 0)` 的语义是「不过滤租户」，而租户 ID 恰好来自
// JWT claims。也就是说，一张 tenantID=0 的 token 等价于跨租户读写能力。
// 而「上下文丢失」在下游看来与「平台级账号」完全一样（都是 0），
// 无法分辨 —— 所以只能在入口按**身份**把住：光有 token 里写着 0 不够，
// 还必须持有 admin 角色。
//
// 本用例锁定这条策略。将来若引入其它平台级角色（如平台审计员），
// 需要同时修改 canAccessWithoutTenant 与本用例 —— 这正是目的。
func TestCanAccessWithoutTenant(t *testing.T) {
	cases := []struct {
		name      string
		roleCodes []string
		want      bool
	}{
		{"持有 admin 角色放行", []string{AdminRoleCode}, true},
		{"admin 与其它角色并存仍放行", []string{"editor", AdminRoleCode}, true},
		{"普通角色拒绝", []string{"editor"}, false},
		{"多个普通角色也拒绝", []string{"editor", "viewer", "auditor"}, false},
		// 角色解析失败（Redis/DB 故障、RoleResolver 未注入）时 resolveRoleCodes
		// 返回 nil —— 必须按无权限处理，否则「算不出来」会变成「放行」
		{"角色解析失败（空列表）拒绝", nil, false},
		{"角色列表为空拒绝", []string{}, false},
		// 大小写敏感：角色 code 是数据库里的精确值，不做模糊匹配
		{"大小写不同不视为 admin", []string{"Admin"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canAccessWithoutTenant(tc.roleCodes); got != tc.want {
				t.Errorf("canAccessWithoutTenant(%v) = %v，期望 %v", tc.roleCodes, got, tc.want)
			}
		})
	}
}
