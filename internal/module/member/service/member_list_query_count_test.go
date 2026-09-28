package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"go-admin/internal/database"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/model"

	"gorm.io/gorm"
)

// 本文件用「统计 SQL 次数」的方式钉住会员列表不再 N+1。
//
// 为什么必须用次数而不是只断言结果正确：N+1 的**结果是对的**，
// 只是慢。断言「标签能查出来」对旧实现同样成立，用例永远转不了红，
// 也就拦不住任何人把批量查询改回循环。
//
// ⚠️ testsupport.NewDB 会改写包级 database.DB，因此不能 t.Parallel。

var countQuerySeq int

// countQueries 统计 fn 执行期间产生的 SELECT 次数（Count / Find / Pluck 都算）。
//
// 用 GORM 回调而不是 mock 仓储：回调不改变任何行为，统计的是**真实发生**的查询，
// 因此「Service 换了一种写法但底层仍然逐行查」也会被算出来。
func countQueries(t *testing.T, fn func()) int {
	t.Helper()

	db := database.DB
	countQuerySeq++
	cbName := fmt.Sprintf("test:count_query_%d", countQuerySeq)

	var n int
	db.Callback().Query().Before("gorm:query").Register(cbName, func(tx *gorm.DB) {
		n++
	})
	t.Cleanup(func() { db.Callback().Query().Remove(cbName) })

	fn()
	return n
}

// listItem 用于从列表项里取出 id 与 tags。
//
// memberWithTag 定义在 FindList 函数体内，测试无法按名字引用它；
// 而它又是响应体的一部分，走 JSON 正好同时验证了对外契约。
type listItem struct {
	ID   uint              `json:"id"`
	Tags []model.MemberTag `json:"tags"`
}

func decodeListItem(t *testing.T, item interface{}) listItem {
	t.Helper()

	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("序列化列表项失败: %v", err)
	}
	var out listItem
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("反序列化列表项失败: %v", err)
	}
	return out
}

// TestMemberListDoesNotQueryPerRow 列表查询次数必须是常数级，与行数无关。
//
// 旧实现是循环里「每行查一次关联表 + 一次标签表」，pageSize 上限 100 时
// 一次列表请求最多 201 次查询 —— 页码越靠后越慢，且随页大小线性放大。
func TestMemberListDoesNotQueryPerRow(t *testing.T) {
	s := newTestMemberService(t)

	tagA := seedTag(t, tenantA, "VIP")
	tagB := seedTag(t, tenantA, "老客")

	const rows = 6
	for i := 0; i < rows; i++ {
		m := seedMember(t, tenantA,
			fmt.Sprintf("1310000%04d", i), fmt.Sprintf("3000%02d", i))

		// 交替挂标签：偶数行只挂 A，奇数行挂 A+B。
		// 这样既能验证「一行多标签」，也能验证「同一标签被多行共用」——
		// 后者是「去重后一次性取详情」这条优化最容易写错的地方。
		tagIDs := []uint{tagA.ID}
		if i%2 == 1 {
			tagIDs = append(tagIDs, tagB.ID)
		}
		if err := s.memberRepo.ReplaceTags(tenantA, m.ID, tagIDs); err != nil {
			t.Fatalf("关联标签失败: %v", err)
		}
	}

	// 一个完全没有标签的会员：验证 map 里取不到键时不会 panic、
	// 也不会塞进一个 ID 为 0 的空标签。
	bare := seedMember(t, tenantA, "13100009999", "300099")

	var items []interface{}
	var total int64
	n := countQueries(t, func() {
		var err error
		items, total, err = s.FindList(tenantA, &dto.MemberListRequest{})
		if err != nil {
			t.Fatalf("查询会员列表失败: %v", err)
		}
	})

	if total != rows+1 {
		t.Fatalf("应返回 %d 条，实际 %d", rows+1, total)
	}

	// 当前实现：Count + 会员列表 + 关联表 + 标签详情 = 4 次。
	// 留一点余量（6）以免将来加一两个辅助查询就误报。
	// 旧实现随行数线性增长 —— 变异验证实测本用例 7 行时为 17 次，
	// 与阈值有明显差距，不会出现「刚好卡在边界」的假通过。
	if n > 6 {
		t.Errorf("列表查询次数应与行数无关，实际 %d 次（%d 行）；"+
			"若这里转红，说明又退回了「每行各查两次」的写法", n, rows+1)
	}

	// 次数对了不代表内容对：逐行核对标签
	byID := make(map[uint]listItem, len(items))
	for _, item := range items {
		it := decodeListItem(t, item)
		byID[it.ID] = it
	}

	for i := 0; i < rows; i++ {
		phone := fmt.Sprintf("1310000%04d", i)
		m, err := s.memberRepo.FindByPhone(tenantA, phone)
		if err != nil {
			t.Fatalf("回读会员失败: %v", err)
		}

		it, ok := byID[m.ID]
		if !ok {
			t.Fatalf("列表里找不到会员 %s（id=%d）", phone, m.ID)
		}

		want := 1
		if i%2 == 1 {
			want = 2
		}
		if len(it.Tags) != want {
			t.Errorf("会员 %s 应有 %d 个标签，实际 %d 个: %+v", phone, want, len(it.Tags), it.Tags)
		}

		// 标签详情必须真的取回来（只返回 ID 列表不算通过）
		for _, tag := range it.Tags {
			if tag.ID == 0 || tag.Name == "" {
				t.Errorf("会员 %s 的标签详情不完整: %+v", phone, tag)
			}
			if tag.ID != tagA.ID && tag.ID != tagB.ID {
				t.Errorf("会员 %s 出现了本租户以外的标签: %+v", phone, tag)
			}
		}
	}

	if it, ok := byID[bare.ID]; !ok {
		t.Fatal("列表里找不到无标签会员")
	} else if len(it.Tags) != 0 {
		t.Errorf("无标签会员的 tags 应为空，实际 %+v", it.Tags)
	}
}

// TestMemberListTagsAreTenantScoped 批量查询不能丢掉租户约束。
//
// 关联表 pay_member_tag_rel 没有 tenant_id 列，批量版靠子查询回连
// pay_member 施加约束。改成批量时最容易「顺手」省掉这个子查询
// （毕竟调用方已经过滤过会员了），于是签名里的 tenantID 变成一句空话。
func TestMemberListTagsAreTenantScoped(t *testing.T) {
	s := newTestMemberService(t)

	foreignTag := seedTag(t, tenantB, "别家的标签")
	foreign := seedMember(t, tenantB, "13200000001", "400001")
	if err := s.memberRepo.ReplaceTags(tenantB, foreign.ID, []uint{foreignTag.ID}); err != nil {
		t.Fatalf("关联标签失败: %v", err)
	}

	// 本租户放一个会员，保证列表非空、批量查询真的被执行
	mineTag := seedTag(t, tenantA, "本家的标签")
	mine := seedMember(t, tenantA, "13200000002", "400002")
	if err := s.memberRepo.ReplaceTags(tenantA, mine.ID, []uint{mineTag.ID}); err != nil {
		t.Fatalf("关联标签失败: %v", err)
	}

	items, _, err := s.FindList(tenantA, &dto.MemberListRequest{})
	if err != nil {
		t.Fatalf("查询会员列表失败: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("租户 A 应只看到 1 条，实际 %d 条", len(items))
	}

	it := decodeListItem(t, items[0])
	if len(it.Tags) != 1 || it.Tags[0].ID != mineTag.ID {
		t.Errorf("租户 A 只应看到自己的标签 %d，实际 %+v", mineTag.ID, it.Tags)
	}
}
