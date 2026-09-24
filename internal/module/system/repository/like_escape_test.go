package repository

import "testing"

// TestUserFindListEscapesLikeWildcards 模糊搜索里的通配符必须按字面量处理（P1-4）。
//
// 回归背景：`FindList` 原先直接拼 `"%"+username+"%"`。用户传 `%` 时模式变成
// `%%%`，等价于「匹配全部用户」—— 一个只需要 user:list 权限的账号就能用它
// 枚举整张用户表（分页参数成了唯一限制），顺带把索引扫描变成全表扫描。
//
// 断言用「不能匹配到任何一条无关记录」而不是「必须匹配到某条」：
// 测试环境是 SQLite，它不认反斜杠转义（`\%` 被当成字面量 `\` + 通配），
// 转义后的模式会**少匹配**。失败方向是安全的那一侧，
// 生产（MySQL）下则是精确的字面量匹配 —— 两种引擎下本用例都成立。
func TestUserFindListEscapesLikeWildcards(t *testing.T) {
	repo := newUserRepoWithDB(t)
	seedUser(t, repo, "alice", testTenant)
	seedUser(t, repo, "bob", testTenant)

	// 未转义时 `%` 会匹配全部；转义后应一条都匹配不到
	for _, input := range []string{"%", "%%%", "_", "%a%"} {
		users, total, err := repo.FindList(testTenant, input, "", nil, 0, 1, 20)
		if err != nil {
			t.Fatalf("搜索 %q 失败: %v", input, err)
		}
		if total != 0 || len(users) != 0 {
			t.Errorf("通配符输入 %q 应被当作字面量、匹配不到任何用户，实际 total=%d", input, total)
		}
	}

	// 正常关键字仍要能命中（确认转义没有把功能一起改坏）
	users, total, err := repo.FindList(testTenant, "ali", "", nil, 0, 1, 20)
	if err != nil {
		t.Fatalf("正常搜索失败: %v", err)
	}
	if total != 1 || len(users) != 1 {
		t.Fatalf("普通关键字应命中 1 条，实际 total=%d len=%d", total, len(users))
	}
	if users[0].Username != "alice" {
		t.Errorf("命中的应是 alice，实际 %q", users[0].Username)
	}
}
