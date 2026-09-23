package repository

import (
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/member/model"
	"go-admin/internal/testsupport"
)

// 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。

const (
	memberTenantA uint = 1
	memberTenantB uint = 2
)

func newMemberRepoWithDB(t *testing.T) MemberRepository {
	t.Helper()
	// 先建库（会注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	testsupport.NewDB(t, &model.Member{}, &model.MemberTagRel{}, &model.MemberTag{})
	return NewMemberRepository()
}

// seedMemberForTest 建一个会员。phone/memberNo 的唯一索引是全局的，两租户用不同值。
func seedMemberForTest(t *testing.T, tenantID uint, phone, memberNo, nickname string) *model.Member {
	t.Helper()
	m := &model.Member{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		MemberNo:        memberNo,
		Phone:           phone,
		Nickname:        nickname,
		Status:          1,
	}
	if err := NewMemberRepository().Create(m); err != nil {
		t.Fatalf("创建会员失败: %v", err)
	}
	return m
}

// TestMemberRepositoryTenantIsolation 会员数据的租户隔离。
//
// 会员是敏感个人信息（手机号/生日/消费积分），泄露即事故。
// 典型漏点是「漏传 tenantID → TenantScope 不过滤 → 全表查询」。
func TestMemberRepositoryTenantIsolation(t *testing.T) {
	repo := newMemberRepoWithDB(t)
	seedMemberForTest(t, memberTenantA, "13800000011", "000001", "甲租户会员")
	seedMemberForTest(t, memberTenantB, "13800000022", "000002", "乙租户会员")

	t.Run("列表按租户过滤", func(t *testing.T) {
		members, total, err := repo.FindList(memberTenantA, "", "", 0, -1, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(members) != 1 {
			t.Fatalf("租户A 应只看到 1 条，实际 total=%d len=%d", total, len(members))
		}
		if members[0].Phone != "13800000011" {
			t.Errorf("看到了其他租户的会员: %s", members[0].Phone)
		}
	})

	t.Run("跨租户按 ID 查不到", func(t *testing.T) {
		list, _, err := repo.FindList(memberTenantB, "", "", 0, -1, 1, 100)
		if err != nil || len(list) != 1 {
			t.Fatalf("准备数据异常: %v", err)
		}
		if _, err := repo.FindByID(memberTenantA, list[0].ID); err == nil {
			t.Error("跨租户按 ID 应查不到")
		}
	})

	t.Run("按手机号查询受租户过滤", func(t *testing.T) {
		// 手机号唯一索引是全局的，但业务上查询入口都在租户上下文里
		if _, err := repo.FindByPhone(memberTenantA, "13800000022"); err == nil {
			t.Error("跨租户按手机号应查不到")
		}
		if _, err := repo.FindByPhone(memberTenantA, "13800000011"); err != nil {
			t.Errorf("本租户按手机号应查到: %v", err)
		}
	})

	t.Run("跨租户 UpdateStatus 落不到数据", func(t *testing.T) {
		others, _, _ := repo.FindList(memberTenantB, "", "", 0, -1, 1, 100)
		if err := repo.UpdateStatus(memberTenantA, others[0].ID, 0); err != nil {
			t.Fatalf("更新失败: %v", err)
		}
		// GORM 的 Update 不返回影响行数，回读确认对方数据未被动过
		got, err := repo.FindByID(memberTenantB, others[0].ID)
		if err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Status == 0 {
			t.Error("跨租户 UpdateStatus 居然生效了")
		}
	})

	t.Run("跨租户 UpdatePoints 归零不生效", func(t *testing.T) {
		others, _, _ := repo.FindList(memberTenantB, "", "", 0, -1, 1, 100)
		if err := repo.UpdatePoints(memberTenantA, others[0].ID, 99999); err != nil {
			t.Fatalf("更新失败: %v", err)
		}
		got, _ := repo.FindByID(memberTenantB, others[0].ID)
		if got.Points == 99999 {
			t.Error("跨租户积分修改竟然生效了（可被用来刷分）")
		}
	})

	t.Run("跨租户 Delete 删不掉", func(t *testing.T) {
		others, _, _ := repo.FindList(memberTenantB, "", "", 0, -1, 1, 100)
		if err := repo.Delete(memberTenantA, others[0].ID); err == nil {
			t.Error("跨租户删除应失败")
		}
		if _, err := repo.FindByID(memberTenantB, others[0].ID); err != nil {
			t.Error("跨租户删除失败时不应影响数据")
		}
	})

	t.Run("FindMaxMemberNo 只看本租户", func(t *testing.T) {
		// 发号器按库内最大值推导起始号，跨租户串号会让编号失序，
		// 更糟的是对不上号段后可能发出重复编号（撞 uk_member_no）
		got, err := repo.FindMaxMemberNo(memberTenantA)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if got != "000001" {
			t.Errorf("应只看本租户的编号，实际 %q", got)
		}
	})
}

// TestMemberRepositoryFindTagIDsScoped 标签关联查询的租户约束。
//
// 回归背景：该方法签名带 tenantID 却一度没用它，靠调用方先查会员兜底。
// 补上子查询过滤后，直接传别的租户的 memberID 必须读到空列表。
func TestMemberRepositoryFindTagIDsScoped(t *testing.T) {
	repo := newMemberRepoWithDB(t)
	seedMemberForTest(t, memberTenantA, "13800000033", "000003", "本人")
	foreign := seedMemberForTest(t, memberTenantB, "13800000044", "000004", "别人")

	if err := repo.ReplaceTags(memberTenantB, foreign.ID, []uint{7}); err != nil {
		t.Fatalf("准备标签失败: %v", err)
	}

	got, err := repo.FindTagIDsByMemberID(memberTenantB, foreign.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("本租户应读到 1 个标签，实际 %v", got)
	}

	// 拿着别人的 memberID + 自己的 tenantID：必须读空
	leaked, err := repo.FindTagIDsByMemberID(memberTenantA, foreign.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(leaked) != 0 {
		t.Errorf("跨租户读到了标签关联 %v（= 隐私泄漏）", leaked)
	}
}

// TestMemberRepositorySoftDeleteReleasesUnique 软删除释放 phone / member_no 唯一索引。
//
// uk_phone、uk_member_no 都是全局唯一索引，不改写就再也建不出同号会员，
// 而且失败发生在 INSERT 阶段，前端只看得到一句数据库错误。
func TestMemberRepositorySoftDeleteReleasesUnique(t *testing.T) {
	repo := newMemberRepoWithDB(t)
	m := seedMemberForTest(t, memberTenantA, "13800000055", "000005", "要删的")

	// 先挂一个标签：删除应顺带清掉它，验证「不留孤儿关联」
	if err := repo.ReplaceTags(memberTenantA, m.ID, []uint{1}); err != nil {
		t.Fatalf("准备标签失败: %v", err)
	}
	got, _ := repo.FindTagIDsByMemberID(memberTenantA, m.ID)
	if len(got) != 1 {
		t.Fatalf("准备数据异常: %v", got)
	}

	if err := repo.Delete(memberTenantA, m.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := repo.FindByID(memberTenantA, m.ID); err == nil {
		t.Error("已删除的会员不应还能查到")
	}
	again := &model.Member{
		TenantBaseModel: common.TenantBaseModel{TenantID: memberTenantA},
		MemberNo:        "000005",
		Phone:           "13800000055",
		Nickname:        "重建",
		Status:          1,
	}
	if err := repo.Create(again); err != nil {
		t.Fatalf("同手机号会员无法重建（软删除未释放唯一值）: %v", err)
	}

	// 会员已被软删除 → 子查询不返回它 → 关联读出来必然是空
	rels, err := repo.FindTagIDsByMemberID(memberTenantA, m.ID)
	if err != nil {
		t.Fatalf("查询关联失败: %v", err)
	}
	if len(rels) != 0 {
		t.Errorf("删除后应清掉标签关联，实际残留 %v（孤儿记录）", rels)
	}
}
