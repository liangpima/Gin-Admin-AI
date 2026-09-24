package repository

import (
	"errors"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/member/model"
	"go-admin/internal/testsupport"

	"gorm.io/gorm"
)

// 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。

func newLevelRepoWithDB(t *testing.T) MemberLevelRepository {
	t.Helper()
	// 先建库（注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	testsupport.NewDB(t, &model.MemberLevel{})
	return NewMemberLevelRepository()
}

func seedLevel(t *testing.T, tenantID uint, name string, minPoints int64, sort int, status int8) *model.MemberLevel {
	t.Helper()
	lv := &model.MemberLevel{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Name:            name,
		MinPoints:       minPoints,
		Discount:        10,
		Sort:            sort,
		Status:          status,
	}
	if err := NewMemberLevelRepository().Create(lv); err != nil {
		t.Fatalf("创建会员等级失败: %v", err)
	}
	return lv
}

// TestMemberLevelRepositoryCRUD 等级仓储的基本读写
func TestMemberLevelRepositoryCRUD(t *testing.T) {
	repo := newLevelRepoWithDB(t)

	created := seedLevel(t, memberTenantA, "黄金会员", 1000, 2, 1)

	t.Run("按 ID 可读回", func(t *testing.T) {
		got, err := repo.FindByID(memberTenantA, created.ID)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if got.Name != "黄金会员" || got.MinPoints != 1000 {
			t.Errorf("读回的数据不一致: %+v", got)
		}
	})

	t.Run("Update 生效", func(t *testing.T) {
		created.Name = "黄金会员PLUS"
		created.MinPoints = 2000
		if err := repo.Update(memberTenantA, created); err != nil {
			t.Fatalf("更新失败: %v", err)
		}

		got, err := repo.FindByID(memberTenantA, created.ID)
		if err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Name != "黄金会员PLUS" || got.MinPoints != 2000 {
			t.Errorf("更新未落库: %+v", got)
		}
	})

	t.Run("Update 不存在的 ID 返回 ErrRecordNotFound", func(t *testing.T) {
		ghost := &model.MemberLevel{
			TenantBaseModel: common.TenantBaseModel{TenantID: memberTenantA, BaseModel: common.BaseModel{ID: 999999}},
			Name:            "幽灵",
		}
		err := repo.Update(memberTenantA, ghost)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("应返回 ErrRecordNotFound（否则 Service 会把它当 500 而不是 404），实际 %v", err)
		}
	})

	t.Run("Delete 软删除", func(t *testing.T) {
		if err := repo.Delete(memberTenantA, created.ID); err != nil {
			t.Fatalf("删除失败: %v", err)
		}
		if _, err := repo.FindByID(memberTenantA, created.ID); err == nil {
			t.Error("已删除的等级不应还能查到")
		}
	})
}

// TestMemberLevelRepositoryTenantIsolation 等级数据的租户隔离。
//
// 等级带折扣率，是**定价**的一部分：读到别家等级、或改掉别家的折扣，
// 会直接影响交易金额，比普通配置泄漏更严重。
func TestMemberLevelRepositoryTenantIsolation(t *testing.T) {
	repo := newLevelRepoWithDB(t)
	mine := seedLevel(t, memberTenantA, "甲租户等级", 100, 1, 1)
	theirs := seedLevel(t, memberTenantB, "乙租户等级", 200, 1, 1)

	t.Run("列表按租户过滤", func(t *testing.T) {
		list, total, err := repo.FindList(memberTenantA, "", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(list) != 1 || list[0].ID != mine.ID {
			t.Fatalf("租户A 应只看到自己的 1 条，实际 total=%d list=%+v", total, list)
		}
	})

	t.Run("FindAll 按租户过滤", func(t *testing.T) {
		list, err := repo.FindAll(memberTenantA)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(list) != 1 || list[0].ID != mine.ID {
			t.Errorf("FindAll 越租户了: %+v", list)
		}
	})

	t.Run("跨租户按 ID 查不到", func(t *testing.T) {
		if _, err := repo.FindByID(memberTenantA, theirs.ID); err == nil {
			t.Error("跨租户按 ID 应查不到")
		}
	})

	t.Run("跨租户 Update 被拒", func(t *testing.T) {
		theirs.Name = "被改了"
		theirs.Discount = 1 // 一折
		err := repo.Update(memberTenantA, theirs)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("跨租户 Update 应被拒（否则可改别家折扣），实际 %v", err)
		}

		got, err := repo.FindByID(memberTenantB, theirs.ID)
		if err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Name == "被改了" || got.Discount == 1 {
			t.Errorf("跨租户修改竟然生效了: %+v", got)
		}
	})

	t.Run("跨租户 Delete 删不掉", func(t *testing.T) {
		if err := repo.Delete(memberTenantA, theirs.ID); err != nil {
			t.Fatalf("删除调用本身不应报错: %v", err)
		}
		if _, err := repo.FindByID(memberTenantB, theirs.ID); err != nil {
			t.Error("跨租户删除竟然生效了")
		}
	})
}

// TestMemberLevelRepositoryFindListFilterAndPage 名称过滤与分页。
//
// 过滤条件若被忽略，前端会「筛选后条数没变」；分页若算错 Offset，
// 表现为翻页后重复出现同一批数据 —— 两种都不报错，只是数据看起来不对。
func TestMemberLevelRepositoryFindListFilterAndPage(t *testing.T) {
	repo := newLevelRepoWithDB(t)

	seedLevel(t, memberTenantA, "青铜", 0, 1, 1)
	seedLevel(t, memberTenantA, "白银", 100, 2, 1)
	seedLevel(t, memberTenantA, "黄金", 1000, 3, 1)

	t.Run("按名称模糊匹配", func(t *testing.T) {
		list, total, err := repo.FindList(memberTenantA, "白", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(list) != 1 || list[0].Name != "白银" {
			t.Errorf("名称过滤未生效: total=%d list=%+v", total, list)
		}
	})

	t.Run("空名称不过滤", func(t *testing.T) {
		_, total, err := repo.FindList(memberTenantA, "", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 3 {
			t.Errorf("空条件应返回全部 3 条，实际 %d", total)
		}
	})

	t.Run("按 sort 升序", func(t *testing.T) {
		list, _, err := repo.FindList(memberTenantA, "", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		want := []string{"青铜", "白银", "黄金"}
		for i := range want {
			if list[i].Name != want[i] {
				t.Fatalf("排序不符合 sort ASC：%+v", list)
			}
		}
	})

	t.Run("分页不重不漏", func(t *testing.T) {
		first, total, err := repo.FindList(memberTenantA, "", 1, 2)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 3 {
			t.Errorf("total 应为过滤后的总数 3，实际 %d（写成当前页条数会让分页器少显示页数）", total)
		}
		if len(first) != 2 {
			t.Fatalf("第一页应有 2 条，实际 %d", len(first))
		}

		second, _, err := repo.FindList(memberTenantA, "", 2, 2)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(second) != 1 {
			t.Fatalf("第二页应有 1 条，实际 %d", len(second))
		}

		seen := map[uint]bool{}
		for _, lv := range append(first, second...) {
			if seen[lv.ID] {
				t.Errorf("分页出现重复记录 id=%d（Offset 算错）", lv.ID)
			}
			seen[lv.ID] = true
		}
		if len(seen) != 3 {
			t.Errorf("两页合计应覆盖 3 条，实际 %d", len(seen))
		}
	})
}

// TestMemberLevelRepositoryFindAllOnlyEnabled 下拉选项只应给启用中的等级。
//
// FindAll 的用途是「给会员分配等级」的下拉列表：把停用等级也返回，
// 运营就会把会员挂到一个已下线的等级上，之后既不能享受权益也无法解释。
func TestMemberLevelRepositoryFindAllOnlyEnabled(t *testing.T) {
	repo := newLevelRepoWithDB(t)

	enabled := seedLevel(t, memberTenantA, "启用中", 0, 2, 1)
	seedLevel(t, memberTenantA, "已停用", 0, 1, 0)

	list, err := repo.FindAll(memberTenantA)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("只应返回启用中的等级，实际 %+v", list)
	}
	if list[0].ID != enabled.ID {
		t.Errorf("返回了错误的等级: %+v", list[0])
	}
}
