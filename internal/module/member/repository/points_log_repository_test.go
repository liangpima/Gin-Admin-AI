package repository

import (
	"testing"

	"go-admin/internal/module/member/model"
	"go-admin/internal/testsupport"
)

// 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。

func newPointsLogRepoWithDB(t *testing.T) PointsLogRepository {
	t.Helper()
	testsupport.NewDB(t, &model.PointsLog{})
	return NewPointsLogRepository()
}

// TestPointsLogRepositoryCreateBindsTenant 创建流水时必须由仓储强制绑定租户。
//
// 这一条是「调用方传什么就存什么」与「仓储强制纠正」的分水岭：
// 积分流水是**账**，一旦租户写错（或调用方漏赋值），
// 该条流水就永久落在错误的租户名下 —— 既污染对方的账，也让本租户对不上账。
// 因此这里刻意传一个「已经带了错误 TenantID」的对象，要求被纠正回来。
func TestPointsLogRepositoryCreateBindsTenant(t *testing.T) {
	repo := newPointsLogRepoWithDB(t)

	log := &model.PointsLog{
		MemberID: 1,
		Change:   100,
		Type:     1,
		Source:   "下单赠送",
	}
	// 故意把 TenantID 设成别的租户，仓储应当覆盖它
	log.TenantID = memberTenantB

	if err := repo.Create(memberTenantA, log); err != nil {
		t.Fatalf("创建流水失败: %v", err)
	}
	if log.TenantID != memberTenantA {
		t.Errorf("仓储应强制绑定入参 tenantID，实际落库为 %d", log.TenantID)
	}

	// 用租户A 查得到、租户B 查不到，才算真的绑对了
	listA, totalA, err := repo.FindList(memberTenantA, 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if totalA != 1 || len(listA) != 1 {
		t.Fatalf("租户A 应看到 1 条流水，实际 total=%d", totalA)
	}

	_, totalB, err := repo.FindList(memberTenantB, 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if totalB != 0 {
		t.Errorf("流水落到了错误的租户名下（租户B 看到 %d 条）", totalB)
	}
}

// TestPointsLogRepositoryFindListFilters 流水查询的过滤条件与排序。
//
// 过滤条件被忽略是典型的静默故障：界面按「消费」筛选后仍然混着「获取」，
// 运营据此核算就会得出错误的消耗量。
func TestPointsLogRepositoryFindListFilters(t *testing.T) {
	repo := newPointsLogRepoWithDB(t)

	seed := func(memberID uint, change int64, changeType int8, source string) {
		t.Helper()
		if err := repo.Create(memberTenantA, &model.PointsLog{
			MemberID: memberID,
			Change:   change,
			Type:     changeType,
			Source:   source,
		}); err != nil {
			t.Fatalf("准备流水失败: %v", err)
		}
	}

	seed(11, 100, 1, "下单赠送")
	seed(11, -30, 2, "兑换商品")
	seed(22, 50, 1, "签到")

	t.Run("按会员过滤", func(t *testing.T) {
		list, total, err := repo.FindList(memberTenantA, 11, 0, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 || len(list) != 2 {
			t.Fatalf("会员 11 应有 2 条，实际 total=%d list=%+v", total, list)
		}
		for _, l := range list {
			if l.MemberID != 11 {
				t.Errorf("混入了其他会员的流水: %+v", l)
			}
		}
	})

	t.Run("按变动类型过滤", func(t *testing.T) {
		list, total, err := repo.FindList(memberTenantA, 0, 2, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(list) != 1 || list[0].Change != -30 {
			t.Fatalf("类型=2 应只有 1 条，实际 total=%d list=%+v", total, list)
		}
	})

	t.Run("会员+类型组合过滤", func(t *testing.T) {
		_, total, err := repo.FindList(memberTenantA, 11, 1, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 {
			t.Errorf("会员 11 且类型 1 应只有 1 条，实际 %d", total)
		}
	})

	t.Run("按 id 倒序（最新在前）", func(t *testing.T) {
		list, _, err := repo.FindList(memberTenantA, 0, 0, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		for i := 1; i < len(list); i++ {
			if list[i-1].ID <= list[i].ID {
				t.Fatalf("流水应按 id DESC 返回，实际 %+v", list)
			}
		}
	})

	t.Run("分页", func(t *testing.T) {
		page1, total, err := repo.FindList(memberTenantA, 0, 0, 1, 2)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 3 {
			t.Errorf("total 应为 3，实际 %d", total)
		}
		if len(page1) != 2 {
			t.Fatalf("第一页应有 2 条，实际 %d", len(page1))
		}

		page2, _, err := repo.FindList(memberTenantA, 0, 0, 2, 2)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(page2) != 1 {
			t.Fatalf("第二页应有 1 条，实际 %d", len(page2))
		}
		if page1[0].ID == page2[0].ID {
			t.Error("分页出现重复记录（Offset 算错）")
		}
	})
}

// TestPointsLogRepositoryTenantIsolation 积分流水的租户隔离。
//
// 流水含消费金额与行为轨迹，且是账目凭证：读到别家的流水等于泄漏经营数据。
func TestPointsLogRepositoryTenantIsolation(t *testing.T) {
	repo := newPointsLogRepoWithDB(t)

	if err := repo.Create(memberTenantA, &model.PointsLog{MemberID: 1, Change: 10, Type: 1}); err != nil {
		t.Fatalf("准备流水失败: %v", err)
	}
	if err := repo.Create(memberTenantB, &model.PointsLog{MemberID: 2, Change: 20, Type: 1}); err != nil {
		t.Fatalf("准备流水失败: %v", err)
	}

	list, total, err := repo.FindList(memberTenantA, 0, 0, 1, 100)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("租户A 应只看到 1 条，实际 total=%d list=%+v", total, list)
	}
	if list[0].MemberID != 1 {
		t.Errorf("看到了其他租户的流水: %+v", list[0])
	}
}
