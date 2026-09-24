package repository

import (
	"errors"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"

	"gorm.io/gorm"
)

// 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。
//
// sys_post 是**多租户表**（含 tenant_id，唯一约束是 (tenant_id, code) 复合索引），
// 因此既要测租户隔离，也要测「不同租户可以各自拥有同编码岗位」。
//
// 注意：唯一索引在 init.sql 里是 UNIQUE KEY uk_tenant_code (tenant_id, code)，
// 而模型只标了 Code 字段（TenantID 来自共享的 TenantBaseModel，无法在标签里
// 与 Code 组成复合索引），AutoMigrate 因此建不出它。测试里显式补上，
// 让测试 schema 与生产一致 —— 否则「同租户内编码重复」这条路径根本没被测到。

const (
	postTenantA uint = 1
	postTenantB uint = 2
)

func newPostRepoWithDB(t *testing.T) PostRepository {
	t.Helper()
	// 先建库（注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	testsupport.NewDB(t, &model.SysPost{}, &model.SysUserPost{})
	if err := database.DB.Exec(
		"CREATE UNIQUE INDEX IF NOT EXISTS uk_tenant_code ON sys_post (tenant_id, code)",
	).Error; err != nil {
		t.Fatalf("补建唯一索引失败: %v", err)
	}
	return NewPostRepository()
}

func seedPost(t *testing.T, repo PostRepository, tenantID uint, code, name string, sort int, status int8) *model.SysPost {
	t.Helper()
	p := &model.SysPost{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Code:            code,
		Name:            name,
		Sort:            sort,
		Status:          status,
	}
	if err := repo.Create(p); err != nil {
		t.Fatalf("创建岗位失败: %v", err)
	}
	return p
}

// TestPostRepositoryCRUD 基本读写
func TestPostRepositoryCRUD(t *testing.T) {
	repo := newPostRepoWithDB(t)
	created := seedPost(t, repo, postTenantA, "dev", "研发", 1, 1)

	t.Run("按 ID 读回", func(t *testing.T) {
		got, err := repo.FindByID(postTenantA, created.ID)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if got.Code != "dev" || got.Name != "研发" {
			t.Errorf("读回的数据不一致: %+v", got)
		}
	})

	t.Run("Update 生效且不改租户", func(t *testing.T) {
		created.Name = "研发中心"
		created.Status = 0
		if err := repo.Update(created); err != nil {
			t.Fatalf("更新失败: %v", err)
		}

		got, err := repo.FindByID(postTenantA, created.ID)
		if err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Name != "研发中心" || got.Status != 0 {
			t.Errorf("更新未落库: %+v", got)
		}
		// Select 列表刻意不含 TenantID：调用方对象里 tenant_id 为 0 时
		// 若被写回，岗位会「漂移」到平台租户，所有租户都看得见
		if got.TenantID != postTenantA {
			t.Errorf("租户归属被改写了: %d", got.TenantID)
		}
	})
}

// TestPostRepositoryCreateDuplicateCodeScoped 编码重复只在同租户内冲突。
//
// 唯一索引是 (tenant_id, code)：不同租户可以各自有 "dev" 岗位。
// 若唯一约束被误当成全局，第二个租户建同名岗位会直接失败。
func TestPostRepositoryCreateDuplicateCodeScoped(t *testing.T) {
	repo := newPostRepoWithDB(t)
	seedPost(t, repo, postTenantA, "dev", "研发", 1, 1)

	t.Run("同租户内重复编码被识别成业务错误", func(t *testing.T) {
		err := repo.Create(&model.SysPost{
			TenantBaseModel: common.TenantBaseModel{TenantID: postTenantA},
			Code:            "dev",
			Name:            "另一个研发",
		})
		if err == nil {
			t.Fatal("同租户内重复编码应报错")
		}
		if !errors.Is(err, common.ErrDuplicateKey) {
			t.Errorf("应包装成 common.ErrDuplicateKey（Service 据此返回 400 而非 500），实际 %v", err)
		}
	})

	t.Run("不同租户可用相同编码", func(t *testing.T) {
		if err := repo.Create(&model.SysPost{
			TenantBaseModel: common.TenantBaseModel{TenantID: postTenantB},
			Code:            "dev",
			Name:            "乙租户的研发",
		}); err != nil {
			t.Errorf("不同租户应允许同编码岗位: %v", err)
		}
	})
}

// TestPostRepositoryCountByCode 编码重复校验的计数口径。
//
// CountByCode 是 Service 落库前的预校验。它的 excludeID 用于更新场景
// （不排除自身则「只改名称」也会被判重复）；租户过滤用于让两个租户各自用 dev。
func TestPostRepositoryCountByCode(t *testing.T) {
	repo := newPostRepoWithDB(t)
	mine := seedPost(t, repo, postTenantA, "dev", "研发", 1, 1)
	seedPost(t, repo, postTenantB, "dev", "乙租户研发", 1, 1)

	t.Run("只统计本租户", func(t *testing.T) {
		n, err := repo.CountByCode(postTenantA, "dev", 0)
		if err != nil {
			t.Fatalf("统计失败: %v", err)
		}
		if n != 1 {
			t.Errorf("应只统计到本租户的 1 条（统计到别家会让两个租户互相挡住），实际 %d", n)
		}
	})

	t.Run("更新场景排除自身后为 0", func(t *testing.T) {
		n, err := repo.CountByCode(postTenantA, "dev", mine.ID)
		if err != nil {
			t.Fatalf("统计失败: %v", err)
		}
		if n != 0 {
			t.Errorf("排除自身后应为 0（否则只改名称也会被判重复），实际 %d", n)
		}
	})

	t.Run("不存在的编码为 0", func(t *testing.T) {
		n, err := repo.CountByCode(postTenantA, "not_exists", 0)
		if err != nil {
			t.Fatalf("统计失败: %v", err)
		}
		if n != 0 {
			t.Errorf("应返回 0，实际 %d", n)
		}
	})
}

// TestPostRepositoryTenantIsolation 岗位的租户隔离
func TestPostRepositoryTenantIsolation(t *testing.T) {
	repo := newPostRepoWithDB(t)
	mine := seedPost(t, repo, postTenantA, "a-post", "甲的岗位", 1, 1)
	theirs := seedPost(t, repo, postTenantB, "b-post", "乙的岗位", 1, 1)

	t.Run("列表按租户过滤", func(t *testing.T) {
		list, total, err := repo.FindList(postTenantA, "", nil, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(list) != 1 || list[0].ID != mine.ID {
			t.Fatalf("租户A 应只看到自己的 1 条，实际 total=%d list=%+v", total, list)
		}
	})

	t.Run("FindAll 按租户过滤", func(t *testing.T) {
		list, err := repo.FindAll(postTenantA)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(list) != 1 || list[0].ID != mine.ID {
			t.Errorf("FindAll 越租户了: %+v", list)
		}
	})

	t.Run("FindByIDs 按租户过滤", func(t *testing.T) {
		// 这是「给用户分配岗位」时的归属校验入口：拿别家的 postID 必须查不出来
		got, err := repo.FindByIDs(postTenantA, []uint{mine.ID, theirs.ID})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(got) != 1 || got[0].ID != mine.ID {
			t.Errorf("应只返回本租户岗位，实际 %+v（返回别家即可跨租户提权）", got)
		}
	})

	t.Run("FindByIDs 空列表返回空切片", func(t *testing.T) {
		got, err := repo.FindByIDs(postTenantA, nil)
		if err != nil {
			t.Fatalf("空列表不应报错: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("应返回空切片，实际 %+v", got)
		}
	})

	t.Run("跨租户按 ID 查不到", func(t *testing.T) {
		if _, err := repo.FindByID(postTenantA, theirs.ID); err == nil {
			t.Error("跨租户按 ID 应查不到")
		}
	})
}

// TestPostRepositoryFindListFilters 列表过滤与分页
func TestPostRepositoryFindListFilters(t *testing.T) {
	repo := newPostRepoWithDB(t)
	seedPost(t, repo, postTenantA, "dev", "研发", 1, 1)
	seedPost(t, repo, postTenantA, "ops", "运维", 2, 1)
	seedPost(t, repo, postTenantA, "pm", "产品", 3, 0)

	t.Run("名称模糊匹配", func(t *testing.T) {
		list, total, err := repo.FindList(postTenantA, "运", nil, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(list) != 1 || list[0].Code != "ops" {
			t.Errorf("名称过滤未生效: total=%d list=%+v", total, list)
		}
	})

	t.Run("status 指针过滤（0 是有效取值）", func(t *testing.T) {
		// status 用 *int8 而不是 int8：0 是有效取值（停用），
		// 写成「零值即不过滤」会让「只看停用」退化成「返回全部」
		disabled := int8(0)
		list, total, err := repo.FindList(postTenantA, "", &disabled, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(list) != 1 || list[0].Code != "pm" {
			t.Errorf("status=0 应只命中停用的 1 条，实际 total=%d list=%+v", total, list)
		}

		enabled := int8(1)
		_, total, err = repo.FindList(postTenantA, "", &enabled, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 {
			t.Errorf("status=1 应命中 2 条，实际 %d", total)
		}
	})

	t.Run("按 sort 升序且分页不重不漏", func(t *testing.T) {
		first, total, err := repo.FindList(postTenantA, "", nil, 1, 2)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 3 {
			t.Errorf("total 应为 3，实际 %d", total)
		}
		if len(first) != 2 || first[0].Code != "dev" || first[1].Code != "ops" {
			t.Fatalf("第一页应为 sort ASC 的前两条，实际 %+v", first)
		}

		second, _, err := repo.FindList(postTenantA, "", nil, 2, 2)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(second) != 1 || second[0].Code != "pm" {
			t.Fatalf("第二页应为 pm，实际 %+v", second)
		}
	})
}

// TestPostRepositoryFindAllOnlyEnabled 下拉选项只应给启用中的岗位
func TestPostRepositoryFindAllOnlyEnabled(t *testing.T) {
	repo := newPostRepoWithDB(t)
	enabled := seedPost(t, repo, postTenantA, "dev", "研发", 2, 1)
	seedPost(t, repo, postTenantA, "pm", "产品", 1, 0)

	list, err := repo.FindAll(postTenantA)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(list) != 1 || list[0].ID != enabled.ID {
		t.Errorf("只应返回启用中的岗位，实际 %+v", list)
	}
}

// TestPostRepositoryDeleteReleasesCodeAndRelations 删除岗位必须释放编码并清理关联。
//
// 两件事缺一不可：
//  1. 改写 code 释放 (tenant_id, code) 唯一索引 —— 否则同编码岗位再也建不出来
//  2. 清理 sys_user_post 关联 —— 否则留下孤儿记录，用户详情回显时
//     会显示一个已不存在的岗位
func TestPostRepositoryDeleteReleasesCodeAndRelations(t *testing.T) {
	repo := newPostRepoWithDB(t)
	post := seedPost(t, repo, postTenantA, "dev", "研发", 1, 1)

	// 造两条关联（两个用户挂同一个岗位）
	rels := []model.SysUserPost{{UserID: 101, PostID: post.ID}, {UserID: 102, PostID: post.ID}}
	if err := database.DB.Create(&rels).Error; err != nil {
		t.Fatalf("准备关联失败: %v", err)
	}

	if err := repo.Delete(postTenantA, post.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := repo.FindByID(postTenantA, post.ID); err == nil {
		t.Error("已删除的岗位不应还能查到")
	}

	var left int64
	if err := database.DB.Model(&model.SysUserPost{}).Where("post_id = ?", post.ID).Count(&left).Error; err != nil {
		t.Fatalf("统计关联失败: %v", err)
	}
	if left != 0 {
		t.Errorf("删除岗位后应清掉全部关联，实际残留 %d 条（孤儿记录）", left)
	}

	// 同编码重建必须成功
	if err := repo.Create(&model.SysPost{
		TenantBaseModel: common.TenantBaseModel{TenantID: postTenantA},
		Code:            "dev",
		Name:            "重建的研发",
	}); err != nil {
		t.Fatalf("同编码无法重建（软删除未释放唯一值）: %v", err)
	}
}

// TestPostRepositoryDeleteRejectsForeignTenant 跨租户删除必须失败。
//
// 关联清理那一步是按 post_id 直接删的（关联表无 tenant_id），
// 因此租户校验必须发生在岗位本身的归属确认上；漏了它，
// 攻击者拿别家 postID 就能删掉对方的岗位并连带清空对方的用户关联。
func TestPostRepositoryDeleteRejectsForeignTenant(t *testing.T) {
	repo := newPostRepoWithDB(t)
	theirs := seedPost(t, repo, postTenantB, "b-post", "乙的岗位", 1, 1)

	if err := repo.Delete(postTenantA, theirs.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("跨租户删除应返回 ErrRecordNotFound，实际 %v", err)
	}
	if _, err := repo.FindByID(postTenantB, theirs.ID); err != nil {
		t.Error("跨租户删除竟然生效了")
	}
}
