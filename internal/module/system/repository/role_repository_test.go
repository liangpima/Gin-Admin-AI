package repository

import (
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"
)

// 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。

const (
	roleTenantA uint = 1
	roleTenantB uint = 2
)

func newRoleRepoWithDB(t *testing.T) RoleRepository {
	t.Helper()
	// 先建库（会注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	testsupport.NewDB(t, &model.SysRole{}, &model.SysRoleMenu{}, &model.SysMenu{}, &model.SysUserRole{})
	return NewRoleRepository()
}

func seedRoleForTest(t *testing.T, repo RoleRepository, tenantID uint, name, code string) *model.SysRole {
	t.Helper()
	role := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Name:            name,
		Code:            code,
		Status:          1,
	}
	if err := repo.Create(role); err != nil {
		t.Fatalf("创建角色失败: %v", err)
	}
	return role
}

// TestRoleRepositoryTenantIsolation 角色的租户隔离。
//
// 角色与菜单权限直接挂钩（sys_role_menu → Casbin 策略），
// 隔离失效等于跨租户权限泄漏，是整个 RBAC 里后果最重的一处。
func TestRoleRepositoryTenantIsolation(t *testing.T) {
	repo := newRoleRepoWithDB(t)

	// code 是全局唯一（uk_code），所以两个租户用不同 code，考察的是查询过滤
	seedRoleForTest(t, repo, roleTenantA, "租户A编辑", "editor")
	seedRoleForTest(t, repo, roleTenantB, "租户B编辑", "editor-b")

	t.Run("列表按租户过滤", func(t *testing.T) {
		roles, total, err := repo.FindList(roleTenantA, "", "", nil, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(roles) != 1 {
			t.Fatalf("租户A 应只看到 1 条，实际 total=%d len=%d", total, len(roles))
		}
		if roles[0].Code != "editor" {
			t.Errorf("看到了其他租户的角色: %s", roles[0].Code)
		}
	})

	t.Run("跨租户按 ID 查不到", func(t *testing.T) {
		_, total, err := repo.FindList(roleTenantB, "", "", nil, 1, 100)
		if err != nil || total != 1 {
			t.Fatalf("准备数据异常: total=%d err=%v", total, err)
		}
		roles, _, _ := repo.FindList(roleTenantB, "", "", nil, 1, 100)
		if _, err := repo.FindByID(roleTenantA, roles[0].ID); err == nil {
			t.Error("跨租户按 ID 应查不到")
		}
	})

	t.Run("跨租户按 code 查不到", func(t *testing.T) {
		if _, err := repo.FindByCode(roleTenantA, "editor-b"); err == nil {
			t.Error("跨租户按 code 应查不到")
		}
	})

	t.Run("FindByIDs 只返回本租户的", func(t *testing.T) {
		roles, _, _ := repo.FindList(roleTenantB, "", "", nil, 1, 100)
		got, err := repo.FindByIDs(roleTenantA, []uint{roles[0].ID})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("应返回空，实际 %d 条: %+v", len(got), got)
		}
	})

	t.Run("跨租户 Update 被拒", func(t *testing.T) {
		roles, _, _ := repo.FindList(roleTenantB, "", "", nil, 1, 100)
		roles[0].Name = "劫持"
		if err := repo.Update(roleTenantA, &roles[0]); err == nil {
			t.Error("跨租户 Update 应失败")
		}
	})
}

// TestRoleRepositoryCountByCodeIsGlobal 重名校验**刻意不按租户隔离**。
//
// 这与 uk_code 全局唯一索引、以及 Casbin 主体用角色 code 都一致（见实现处注释）。
// 写这条用例是为了锁住「故意为之」的现状：后来者看到签名没有 tenantID
// 很容易当 bug 补上过滤 —— 那样校验放行、INSERT 撞唯一索引，对外是 500。
func TestRoleRepositoryCountByCodeIsGlobal(t *testing.T) {
	repo := newRoleRepoWithDB(t)
	seedRoleForTest(t, repo, roleTenantA, "编辑", "editor")

	// 同 code 建到另一个租户：全局唯一索引会拒绝
	dup := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: roleTenantB},
		Name:            "另一租户同名",
		Code:            "editor",
	}
	if err := repo.Create(dup); err == nil {
		t.Error("跨租户同 code 应被唯一索引拒绝")
	}

	n, err := repo.CountByCode("editor", 0)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 1 {
		t.Errorf("应计到 1 条（全局），实际 %d", n)
	}

	// 排除自身后归零（编辑页重名校验的形态）
	self := seedRoleForTest(t, repo, roleTenantA, "独占", "exclusive")
	n, err = repo.CountByCode("exclusive", self.ID)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 0 {
		t.Errorf("排除自身后应为 0，实际 %d", n)
	}
}

// TestRoleRepositoryMenuQueriesScoped 角色菜单关联查询必须受租户约束。
//
// sys_role_menu 是纯关联表（只有 role_id/menu_id，没有 tenant_id 列），
// 仓储通过「role_id IN (本租户角色的子查询)」间接施加约束。
// 漏掉这层的话，传任意 role_id 就能读出其他租户角色的菜单（= 权限清单）。
func TestRoleRepositoryMenuQueriesScoped(t *testing.T) {
	repo := newRoleRepoWithDB(t)
	seedRoleForTest(t, repo, roleTenantA, "A管理", "a-admin")

	// 菜单是全局表（sys_menu 无 tenant_id），两个租户共享
	if err := NewMenuRepository().Create(&model.SysMenu{
		Name: "用户管理", Path: "/system/user", Permission: "system:user:list", Status: 1,
	}); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	menus, err := NewMenuRepository().FindAllForManage()
	if err != nil || len(menus) == 0 {
		t.Fatalf("准备菜单失败: %v", err)
	}

	foreign, err := repo.FindByCode(roleTenantA, "a-admin")
	if err != nil {
		t.Fatalf("准备角色失败: %v", err)
	}
	if err := repo.ReplaceMenus(roleTenantA, foreign.ID, []uint{menus[0].ID}); err != nil {
		t.Fatalf("挂菜单失败: %v", err)
	}

	t.Run("本租户能读到菜单", func(t *testing.T) {
		got, err := repo.FindMenuIDsByRoleID(roleTenantA, foreign.ID)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(got) != 1 || got[0] != menus[0].ID {
			t.Errorf("应读到刚挂的菜单，实际 %v", got)
		}
	})

	t.Run("别的租户拿着同一个 roleID 读不到", func(t *testing.T) {
		got, err := repo.FindMenuIDsByRoleID(roleTenantB, foreign.ID)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("跨租户应读到空列表，实际 %v（= 权限清单泄漏）", got)
		}
		list, err := repo.FindMenusByRoleID(roleTenantB, foreign.ID)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("跨租户应读到空菜单，实际 %d 条", len(list))
		}
	})

	t.Run("ReplaceMenus 不能改其他租户角色的授权", func(t *testing.T) {
		if err := repo.ReplaceMenus(roleTenantB, foreign.ID, nil); err == nil {
			t.Error("跨租户 ReplaceMenus 应失败")
		}
		// 原授权必须原样保留
		got, _ := repo.FindMenuIDsByRoleID(roleTenantA, foreign.ID)
		if len(got) != 1 {
			t.Errorf("跨租户操作破坏了原授权: %v", got)
		}
	})
}

// TestRoleRepositorySoftDelete 与唯一索引释放。
//
// uk_code 是全局索引，软删除若不改写 code，同名角色就永远建不出来。
func TestRoleRepositorySoftDelete(t *testing.T) {
	repo := newRoleRepoWithDB(t)
	role := seedRoleForTest(t, repo, roleTenantA, "临时", "temp-role")

	if err := repo.Delete(roleTenantA, role.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := repo.FindByID(roleTenantA, role.ID); err == nil {
		t.Error("已删除的角色不应还能查到")
	}

	again := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: roleTenantA},
		Name:            "重建",
		Code:            "temp-role",
	}
	if err := repo.Create(again); err != nil {
		t.Fatalf("同 code 角色无法重建（软删除未释放唯一值）: %v", err)
	}
}
