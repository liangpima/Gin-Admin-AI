package repository

import (
	"errors"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/member/model"
	"go-admin/internal/testsupport"

	"gorm.io/gorm"
)

// 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。

func newTagRepoWithDB(t *testing.T) MemberTagRepository {
	t.Helper()
	testsupport.NewDB(t, &model.MemberTag{}, &model.MemberTagRel{})
	return NewMemberTagRepository()
}

func seedTag(t *testing.T, tenantID uint, name string, sort int, status int8) *model.MemberTag {
	t.Helper()
	tag := &model.MemberTag{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Name:            name,
		Color:           "#409eff",
		Sort:            sort,
		Status:          status,
	}
	if err := NewMemberTagRepository().Create(tag); err != nil {
		t.Fatalf("创建会员标签失败: %v", err)
	}
	return tag
}

// TestMemberTagRepositoryCRUD 标签仓储的基本读写
func TestMemberTagRepositoryCRUD(t *testing.T) {
	repo := newTagRepoWithDB(t)

	created := seedTag(t, memberTenantA, "高价值", 1, 1)

	t.Run("按 ID 可读回", func(t *testing.T) {
		got, err := repo.FindByID(memberTenantA, created.ID)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if got.Name != "高价值" {
			t.Errorf("读回的数据不一致: %+v", got)
		}
	})

	t.Run("Update 生效", func(t *testing.T) {
		created.Name = "高价值客户"
		created.Color = "#f56c6c"
		if err := repo.Update(memberTenantA, created); err != nil {
			t.Fatalf("更新失败: %v", err)
		}

		got, err := repo.FindByID(memberTenantA, created.ID)
		if err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Name != "高价值客户" || got.Color != "#f56c6c" {
			t.Errorf("更新未落库: %+v", got)
		}
	})

	t.Run("Update 不存在的 ID 返回 ErrRecordNotFound", func(t *testing.T) {
		ghost := &model.MemberTag{
			TenantBaseModel: common.TenantBaseModel{TenantID: memberTenantA, BaseModel: common.BaseModel{ID: 999999}},
			Name:            "幽灵",
		}
		if err := repo.Update(memberTenantA, ghost); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("应返回 ErrRecordNotFound，实际 %v", err)
		}
	})
}

// TestMemberTagRepositoryTenantIsolation 标签的租户隔离
func TestMemberTagRepositoryTenantIsolation(t *testing.T) {
	repo := newTagRepoWithDB(t)
	mine := seedTag(t, memberTenantA, "甲租户标签", 1, 1)
	theirs := seedTag(t, memberTenantB, "乙租户标签", 1, 1)

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
		if err := repo.Update(memberTenantA, theirs); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("跨租户 Update 应被拒，实际 %v", err)
		}
		if got, _ := repo.FindByID(memberTenantB, theirs.ID); got.Name == "被改了" {
			t.Error("跨租户修改竟然生效了")
		}
	})
}

// TestMemberTagRepositoryFindByIDsScoped 批量取标签必须按租户过滤。
//
// FindByIDs 的调用场景是「按会员已绑的 tagIDs 回显标签名」，
// 而 tagID 是可枚举的主键：不过滤就能拿到别家租户的标签名与配色，
// 前端会把它们显示在会员详情上。
func TestMemberTagRepositoryFindByIDsScoped(t *testing.T) {
	repo := newTagRepoWithDB(t)
	mine := seedTag(t, memberTenantA, "甲租户标签", 1, 1)
	theirs := seedTag(t, memberTenantB, "乙租户标签", 1, 1)

	t.Run("只返回本租户的", func(t *testing.T) {
		got, err := repo.FindByIDs(memberTenantA, []uint{mine.ID, theirs.ID})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(got) != 1 || got[0].ID != mine.ID {
			t.Errorf("应只返回本租户标签，实际 %+v（含别家租户数据即泄漏）", got)
		}
	})

	t.Run("空 ID 列表返回空切片且不报错", func(t *testing.T) {
		// 提前 return 是刻意的：`id IN ()` 在 MySQL 里是语法错误，
		// 不短路会让「会员还没绑任何标签」这条最常见路径直接 500。
		got, err := repo.FindByIDs(memberTenantA, nil)
		if err != nil {
			t.Fatalf("空 ID 列表不应报错: %v", err)
		}
		if got == nil {
			t.Error("应返回空切片而不是 nil，避免调用方再判空")
		}
		if len(got) != 0 {
			t.Errorf("空 ID 列表应返回 0 条，实际 %+v", got)
		}

		if got, err := repo.FindByIDs(memberTenantA, []uint{}); err != nil || len(got) != 0 {
			t.Errorf("空切片入参同样应返回 0 条，实际 err=%v got=%+v", err, got)
		}
	})
}

// TestMemberTagRepositoryDeleteCleansRelations 删除标签必须同时清掉会员关联。
//
// pay_member_tag_rel 是纯关联表（无 tenant_id、无软删除），标签软删除后
// 关联行若不删，就会留下指向「不存在的标签」的孤儿记录：
// 会员详情回显时找不到标签名，前端渲染出空白标签或 undefined。
func TestMemberTagRepositoryDeleteCleansRelations(t *testing.T) {
	repo := newTagRepoWithDB(t)
	tag := seedTag(t, memberTenantA, "要删的标签", 1, 1)

	// 造两条关联（两个会员挂同一个标签）
	rels := []model.MemberTagRel{{MemberID: 101, TagID: tag.ID}, {MemberID: 102, TagID: tag.ID}}
	if err := database.DB.Create(&rels).Error; err != nil {
		t.Fatalf("准备关联失败: %v", err)
	}

	if err := repo.Delete(memberTenantA, tag.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	if _, err := repo.FindByID(memberTenantA, tag.ID); err == nil {
		t.Error("已删除的标签不应还能查到")
	}

	var left int64
	if err := database.DB.Model(&model.MemberTagRel{}).Where("tag_id = ?", tag.ID).Count(&left).Error; err != nil {
		t.Fatalf("统计关联失败: %v", err)
	}
	if left != 0 {
		t.Errorf("删除标签后应清掉全部关联，实际残留 %d 条（孤儿记录）", left)
	}
}

// TestMemberTagRepositoryDeleteOnlyOwnTenant 跨租户 Delete 不能删掉别家的标签。
//
// 注意：关联清理那一步是按 tag_id 直接删的，没有租户维度（关联表无 tenant_id）。
// 因此租户校验必须发生在标签本身的删除上 —— 若那一步漏了租户条件，
// 攻击者可以拿别家的 tagID 删掉对方的标签并连带清空对方的会员关联。
func TestMemberTagRepositoryDeleteOnlyOwnTenant(t *testing.T) {
	repo := newTagRepoWithDB(t)
	theirs := seedTag(t, memberTenantB, "乙租户标签", 1, 1)

	if err := repo.Delete(memberTenantA, theirs.ID); err != nil {
		t.Fatalf("删除调用本身不应报错: %v", err)
	}

	if _, err := repo.FindByID(memberTenantB, theirs.ID); err != nil {
		t.Error("跨租户删除竟然生效了")
	}
}

// TestMemberTagRepositoryFindAllOnlyEnabled 下拉选项只应给启用中的标签
func TestMemberTagRepositoryFindAllOnlyEnabled(t *testing.T) {
	repo := newTagRepoWithDB(t)

	enabled := seedTag(t, memberTenantA, "启用中", 2, 1)
	seedTag(t, memberTenantA, "已停用", 1, 0)

	list, err := repo.FindAll(memberTenantA)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(list) != 1 || list[0].ID != enabled.ID {
		t.Errorf("只应返回启用中的标签，实际 %+v", list)
	}
}

// TestMemberTagRepositoryFindListPage 分页与名称过滤
func TestMemberTagRepositoryFindListPage(t *testing.T) {
	repo := newTagRepoWithDB(t)

	seedTag(t, memberTenantA, "青铜", 1, 1)
	seedTag(t, memberTenantA, "白银", 2, 1)
	seedTag(t, memberTenantA, "黄金", 3, 1)

	list, total, err := repo.FindList(memberTenantA, "", 1, 2)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 3 {
		t.Errorf("total 应为 3，实际 %d", total)
	}
	if len(list) != 2 || list[0].Name != "青铜" || list[1].Name != "白银" {
		t.Errorf("第一页应为 sort ASC 的前两条，实际 %+v", list)
	}

	filtered, total, err := repo.FindList(memberTenantA, "黄金", 1, 100)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 1 || len(filtered) != 1 || filtered[0].Name != "黄金" {
		t.Errorf("名称过滤未生效: total=%d list=%+v", total, filtered)
	}
}
