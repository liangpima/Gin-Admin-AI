package repository

import (
	"errors"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"

	"gorm.io/gorm"
)

// 协议租户隔离测试（P1-2）。
//
// 背景：sys_agreement 此前是全局表（继承 common.BaseModel，没有 tenant_id），
// 所有租户共用一份协议。任一租户管理员改协议，改的是**所有租户**页面上的内容。
// 协议是典型的「每个租户各有版本」的数据，因此改为租户内表。
//
// 这一组用例逐方法验证：拿别租户的 ID 既读不到、也改不动、也删不掉。

func newAgreementRepoWithDB(t *testing.T) AgreementRepository {
	t.Helper()
	// 先建库（注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	testsupport.NewDB(t, &model.SysAgreement{})
	return NewAgreementRepository()
}

func seedAgreement(t *testing.T, repo AgreementRepository, tenantID uint, title, typ string, status int8) *model.SysAgreement {
	t.Helper()
	a := &model.SysAgreement{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Title:           title,
		Content:         "<p>" + title + "</p>",
		Type:            typ,
		Status:          status,
	}
	if err := repo.Create(a); err != nil {
		t.Fatalf("创建协议失败: %v", err)
	}
	return a
}

// TestAgreementRepositoryFindIsolatesTenants 查询必须按租户隔离。
func TestAgreementRepositoryFindIsolatesTenants(t *testing.T) {
	repo := newAgreementRepoWithDB(t)
	a := seedAgreement(t, repo, deptTenantA, "A-用户协议", "user", 1)
	b := seedAgreement(t, repo, deptTenantB, "B-用户协议", "user", 1)

	t.Run("本租户可读", func(t *testing.T) {
		got, err := repo.FindByID(deptTenantA, a.ID)
		if err != nil {
			t.Fatalf("本租户协议应可读: %v", err)
		}
		if got.Title != "A-用户协议" {
			t.Errorf("读到的协议不对: %+v", got)
		}
	})

	t.Run("跨租户读不到", func(t *testing.T) {
		_, err := repo.FindByID(deptTenantA, b.ID)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("拿别租户的协议 ID 应返回 ErrRecordNotFound，实际: %v", err)
		}
	})

	t.Run("FindList 只返回本租户", func(t *testing.T) {
		list, total, err := repo.FindList(deptTenantA, "", "", nil, 1, 20)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(list) != 1 {
			t.Fatalf("租户 A 应只看到 1 条协议，实际 total=%d len=%d", total, len(list))
		}
		if list[0].Title != "A-用户协议" {
			t.Errorf("泄漏了其他租户的协议: %+v", list[0])
		}
	})

	t.Run("平台级（tenantID=0）可见全部", func(t *testing.T) {
		// 有意的语义：0 = 不过滤。入口由 middleware.Auth 保证只有 admin 能拿到 0。
		_, total, err := repo.FindList(0, "", "", nil, 1, 20)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 {
			t.Errorf("平台级应看到 2 条，实际 %d", total)
		}
	})
}

// TestAgreementRepositoryFindByTypeIsTenantScoped 按类型取协议也必须隔离。
//
// 这个方法是「前台展示协议」的入口：不隔离时租户 A 的页面会渲染出租户 B 的协议内容。
func TestAgreementRepositoryFindByTypeIsTenantScoped(t *testing.T) {
	repo := newAgreementRepoWithDB(t)
	seedAgreement(t, repo, deptTenantA, "A-隐私政策", "privacy", 1)
	seedAgreement(t, repo, deptTenantB, "B-隐私政策", "privacy", 1)

	gotA, err := repo.FindByType(deptTenantA, "privacy")
	if err != nil {
		t.Fatalf("租户 A 取协议失败: %v", err)
	}
	if gotA.Title != "A-隐私政策" {
		t.Errorf("租户 A 应取到自己的协议，实际 %q", gotA.Title)
	}

	gotB, err := repo.FindByType(deptTenantB, "privacy")
	if err != nil {
		t.Fatalf("租户 B 取协议失败: %v", err)
	}
	if gotB.Title != "B-隐私政策" {
		t.Errorf("租户 B 应取到自己的协议，实际 %q", gotB.Title)
	}

	// 第三个租户没建过该类型的协议 → 404 语义（而不是取到别人的）
	_, err = repo.FindByType(deptTenantA+99, "privacy")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("无该类型协议时应返回 ErrRecordNotFound，实际: %v", err)
	}
}

// TestAgreementRepositoryUpdateIsolatesTenants 改不动别租户的协议。
func TestAgreementRepositoryUpdateIsolatesTenants(t *testing.T) {
	repo := newAgreementRepoWithDB(t)
	b := seedAgreement(t, repo, deptTenantB, "B-原名", "user", 1)

	b.Title = "被劫持"
	b.Content = "<p>被劫持</p>"
	err := repo.Update(deptTenantA, b)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("跨租户更新应返回 ErrRecordNotFound，实际: %v", err)
	}

	got, err := repo.FindByID(deptTenantB, b.ID)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if got.Title != "B-原名" {
		t.Errorf("跨租户更新竟然生效了，标题变成 %q", got.Title)
	}
}

// TestAgreementRepositoryDeleteIsolatesTenants 删不掉别租户的协议。
func TestAgreementRepositoryDeleteIsolatesTenants(t *testing.T) {
	repo := newAgreementRepoWithDB(t)
	b := seedAgreement(t, repo, deptTenantB, "B-协议", "user", 1)

	if err := repo.Delete(deptTenantA, b.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("跨租户删除应返回 ErrRecordNotFound，实际: %v", err)
	}
	if _, err := repo.FindByID(deptTenantB, b.ID); err != nil {
		t.Errorf("协议不应被删除: %v", err)
	}
}
