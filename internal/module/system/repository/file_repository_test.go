package repository

import (
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"
)

// 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。

const (
	fileTenantA uint = 1
	fileTenantB uint = 2
)

func newFileRepoWithDB(t *testing.T) FileRepository {
	t.Helper()
	// 先建库（注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	testsupport.NewDB(t, &model.SysFile{})
	return NewFileRepository()
}

func seedFile(t *testing.T, repo FileRepository, tenantID uint, name, mimeType string) *model.SysFile {
	t.Helper()
	f := &model.SysFile{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Name:            name,
		StorageName:     "stored-" + name,
		Path:            "/uploads/" + name,
		URL:             "/uploads/" + name,
		Size:            1024,
		MimeType:        mimeType,
	}
	if err := repo.Create(f); err != nil {
		t.Fatalf("创建文件记录失败: %v", err)
	}
	return f
}

// TestFileRepositoryCreateAndFindByID 基本读写
func TestFileRepositoryCreateAndFindByID(t *testing.T) {
	repo := newFileRepoWithDB(t)
	created := seedFile(t, repo, fileTenantA, "合同.pdf", "application/pdf")

	got, err := repo.FindByID(fileTenantA, created.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got.Name != "合同.pdf" || got.MimeType != "application/pdf" {
		t.Errorf("读回的数据不一致: %+v", got)
	}
}

// TestFileRepositoryTenantIsolation 文件的租户隔离。
//
// 文件记录里有 path / url，隔离失效意味着可以拿到别家租户上传的
// 合同、发票、身份证照片的直链 —— 属于最严重的一类数据泄漏。
func TestFileRepositoryTenantIsolation(t *testing.T) {
	repo := newFileRepoWithDB(t)
	mine := seedFile(t, repo, fileTenantA, "我的合同.pdf", "application/pdf")
	theirs := seedFile(t, repo, fileTenantB, "别家的合同.pdf", "application/pdf")

	t.Run("列表按租户过滤", func(t *testing.T) {
		list, total, err := repo.FindList(fileTenantA, "", "", "", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(list) != 1 || list[0].ID != mine.ID {
			t.Fatalf("租户A 应只看到自己的 1 条，实际 total=%d list=%+v", total, list)
		}
	})

	t.Run("跨租户按 ID 查不到", func(t *testing.T) {
		if _, err := repo.FindByID(fileTenantA, theirs.ID); err == nil {
			t.Error("跨租户按 ID 应查不到（否则能拿到别家的文件直链）")
		}
	})

	t.Run("跨租户 Delete 删不掉", func(t *testing.T) {
		if err := repo.Delete(fileTenantA, theirs.ID); err != nil {
			t.Fatalf("删除调用本身不应报错: %v", err)
		}
		if _, err := repo.FindByID(fileTenantB, theirs.ID); err != nil {
			t.Error("跨租户删除竟然生效了")
		}
	})
}

// TestFileRepositoryFindListFilters 名称/类型过滤与排序方向。
//
// sortOrder 是白名单取值（只认 "asc"，其余一律按 id DESC）：
// 若被直接拼进 SQL，就成了一条注入点。这里同时验证
// 「传非法值不会改变默认倒序」。
func TestFileRepositoryFindListFilters(t *testing.T) {
	repo := newFileRepoWithDB(t)

	seedFile(t, repo, fileTenantA, "合同.pdf", "application/pdf")
	seedFile(t, repo, fileTenantA, "合同扫描件.png", "image/png")
	seedFile(t, repo, fileTenantA, "头像.png", "image/png")

	t.Run("名称模糊匹配", func(t *testing.T) {
		list, total, err := repo.FindList(fileTenantA, "合同", "", "", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 || len(list) != 2 {
			t.Errorf("应命中 2 条，实际 total=%d list=%+v", total, list)
		}
	})

	t.Run("MIME 前缀匹配", func(t *testing.T) {
		// mimeType 用的是「前缀 + %」而不是「% 前缀 %」：传 image/ 应命中全部图片
		list, total, err := repo.FindList(fileTenantA, "", "image/", "", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 || len(list) != 2 {
			t.Errorf("应命中 2 条图片，实际 total=%d list=%+v", total, list)
		}
	})

	t.Run("默认按 id 倒序（最新在前）", func(t *testing.T) {
		list, _, err := repo.FindList(fileTenantA, "", "", "", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		for i := 1; i < len(list); i++ {
			if list[i-1].ID <= list[i].ID {
				t.Fatalf("默认应按 id DESC，实际 %+v", list)
			}
		}
	})

	t.Run("asc 时按 id 升序", func(t *testing.T) {
		list, _, err := repo.FindList(fileTenantA, "", "", "asc", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		for i := 1; i < len(list); i++ {
			if list[i-1].ID >= list[i].ID {
				t.Fatalf("sortOrder=asc 应按 id ASC，实际 %+v", list)
			}
		}
	})

	t.Run("非法排序值退回默认倒序", func(t *testing.T) {
		list, _, err := repo.FindList(fileTenantA, "", "", "id; DROP TABLE sys_file", 1, 100)
		if err != nil {
			t.Fatalf("非法排序值不应导致报错: %v", err)
		}
		if len(list) != 3 {
			t.Fatalf("应正常返回 3 条，实际 %d", len(list))
		}
		for i := 1; i < len(list); i++ {
			if list[i-1].ID <= list[i].ID {
				t.Fatalf("非法排序值应退回 id DESC，实际 %+v", list)
			}
		}
	})

	t.Run("分页", func(t *testing.T) {
		first, total, err := repo.FindList(fileTenantA, "", "", "asc", 1, 2)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 3 {
			t.Errorf("total 应为 3，实际 %d", total)
		}
		if len(first) != 2 {
			t.Fatalf("第一页应有 2 条，实际 %d", len(first))
		}

		second, _, err := repo.FindList(fileTenantA, "", "", "asc", 2, 2)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(second) != 1 {
			t.Fatalf("第二页应有 1 条，实际 %d", len(second))
		}
		if first[0].ID == second[0].ID {
			t.Error("分页出现重复记录（Offset 算错）")
		}
	})
}

// TestFileRepositoryDelete 删除只影响本租户，且删除后查不到
func TestFileRepositoryDelete(t *testing.T) {
	repo := newFileRepoWithDB(t)
	f := seedFile(t, repo, fileTenantA, "待删除.txt", "text/plain")

	if err := repo.Delete(fileTenantA, f.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := repo.FindByID(fileTenantA, f.ID); err == nil {
		t.Error("已删除的文件记录不应还能查到")
	}

	// 软删除（TenantBaseModel 带 DeletedAt），物理行仍在，但不应出现在列表里
	_, total, err := repo.FindList(fileTenantA, "", "", "", 1, 100)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 0 {
		t.Errorf("已删除的记录不应出现在列表中，实际 total=%d", total)
	}
}
