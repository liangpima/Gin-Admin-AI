package repository

import (
	"errors"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"

	"gorm.io/gorm"
)

// 部门租户隔离测试（P1-1）。
//
// 背景：sys_dept 此前是全局表（继承 common.BaseModel，没有 tenant_id），
// 所有查询都是全表 —— 租户 A 的管理员可以枚举、改名、删除租户 B 的部门。
// 更隐蔽的是 sys_user.dept_id 指向别租户的部门时不会有任何报错，
// 用户归属悄悄错位，界面上看不出来。
//
// 这一组用例逐方法验证：拿别租户的 ID 既读不到、也改不动、也删不掉。

const (
	deptTenantA uint = 1
	deptTenantB uint = 2
)

func newDeptRepoWithDB(t *testing.T) DeptRepository {
	t.Helper()
	// 先建库（注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	testsupport.NewDB(t, &model.SysDept{})
	return NewDeptRepository()
}

func seedDept(t *testing.T, repo DeptRepository, tenantID uint, name string, parentID uint) *model.SysDept {
	t.Helper()
	d := &model.SysDept{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		ParentID:        parentID,
		Name:            name,
		Status:          1,
	}
	if err := repo.Create(d); err != nil {
		t.Fatalf("创建部门失败: %v", err)
	}
	return d
}

// TestDeptRepositoryFindIsolatesTenants 查询必须按租户隔离。
func TestDeptRepositoryFindIsolatesTenants(t *testing.T) {
	repo := newDeptRepoWithDB(t)
	a := seedDept(t, repo, deptTenantA, "A-研发", 0)
	b := seedDept(t, repo, deptTenantB, "B-研发", 0)

	t.Run("本租户可读", func(t *testing.T) {
		got, err := repo.FindByID(deptTenantA, a.ID)
		if err != nil {
			t.Fatalf("本租户部门应可读: %v", err)
		}
		if got.Name != "A-研发" {
			t.Errorf("读到的部门不对: %+v", got)
		}
	})

	t.Run("跨租户读不到", func(t *testing.T) {
		_, err := repo.FindByID(deptTenantA, b.ID)
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("拿别租户的部门 ID 应返回 ErrRecordNotFound（而非数据），实际: %v", err)
		}
	})

	t.Run("FindAll 只返回本租户", func(t *testing.T) {
		depts, err := repo.FindAll(deptTenantA)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(depts) != 1 || depts[0].ID != a.ID {
			t.Errorf("租户 A 应只看到 1 个部门，实际 %+v", depts)
		}
		for _, d := range depts {
			if d.TenantID != deptTenantA {
				t.Errorf("泄漏了其他租户的部门: %+v", d)
			}
		}
	})

	t.Run("平台级（tenantID=0）可见全部", func(t *testing.T) {
		// 这是有意的语义：0 = 不过滤。入口处由 middleware.Auth 保证
		// 只有 admin 角色能持有 tenantID=0 的 token（见 P1-3）。
		depts, err := repo.FindAll(0)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(depts) != 2 {
			t.Errorf("平台级应看到 2 个部门，实际 %d", len(depts))
		}
	})
}

// TestDeptRepositoryUpdateIsolatesTenants 改不动别租户的部门。
func TestDeptRepositoryUpdateIsolatesTenants(t *testing.T) {
	repo := newDeptRepoWithDB(t)
	b := seedDept(t, repo, deptTenantB, "B-原名", 0)

	// 用租户 A 的身份去改租户 B 的部门
	b.Name = "被劫持"
	err := repo.Update(deptTenantA, b)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("跨租户更新应返回 ErrRecordNotFound，实际: %v", err)
	}

	// 数据必须原样
	got, err := repo.FindByID(deptTenantB, b.ID)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if got.Name != "B-原名" {
		t.Errorf("跨租户更新竟然生效了，名称变成 %q", got.Name)
	}
}

// TestDeptRepositoryDeleteIsolatesTenants 删不掉别租户的部门。
func TestDeptRepositoryDeleteIsolatesTenants(t *testing.T) {
	repo := newDeptRepoWithDB(t)
	b := seedDept(t, repo, deptTenantB, "B-部门", 0)

	if err := repo.Delete(deptTenantA, b.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("跨租户删除应返回 ErrRecordNotFound，实际: %v", err)
	}

	if _, err := repo.FindByID(deptTenantB, b.ID); err != nil {
		t.Errorf("部门不应被删除: %v", err)
	}
}

// TestDeptRepositoryCountByParentIsolatesTenants 子部门统计必须按租户。
//
// 不隔离的后果不只是数字错：删除部门前的「有没有下级」判断会看到别的租户的
// 子部门，于是本租户一个子部门都没有的部门也删不掉（或被误判）。
func TestDeptRepositoryCountByParentIsolatesTenants(t *testing.T) {
	repo := newDeptRepoWithDB(t)
	parentA := seedDept(t, repo, deptTenantA, "A-总部", 0)
	parentB := seedDept(t, repo, deptTenantB, "B-总部", 0)

	// 两个租户各在「自己的」父节点下挂一个子部门；
	// 注意 parent_id 相同（都指向自己的父节点，ID 恰好相同是重点：
	// 若不带租户过滤，统计会把两个租户的子部门算在一起）
	seedDept(t, repo, deptTenantA, "A-子", parentA.ID)
	seedDept(t, repo, deptTenantB, "B-子", parentB.ID)

	countA, err := repo.CountByParentID(deptTenantA, parentA.ID)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if countA != 1 {
		t.Errorf("租户 A 的父节点应有 1 个子部门，实际 %d", countA)
	}

	countB, err := repo.CountByParentID(deptTenantB, parentB.ID)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if countB != 1 {
		t.Errorf("租户 B 的父节点应有 1 个子部门，实际 %d", countB)
	}

	// 拿 A 的租户身份去统计 B 的父节点：应为 0（看不见）
	cross, err := repo.CountByParentID(deptTenantA, parentB.ID)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if cross != 0 {
		t.Errorf("跨租户统计应为 0，实际 %d", cross)
	}
}

// TestDeptRepositoryFindParentIDIsolatesTenants 父节点查询也必须隔离。
//
// 它被成环校验使用；不隔离时可用它探测其他租户是否存在某个部门
// （ok=true 就说明该 ID 存在），也是一种信息泄漏。
func TestDeptRepositoryFindParentIDIsolatesTenants(t *testing.T) {
	repo := newDeptRepoWithDB(t)
	parentB := seedDept(t, repo, deptTenantB, "B-总部", 0)
	childB := seedDept(t, repo, deptTenantB, "B-子", parentB.ID)

	if _, ok, err := repo.FindParentID(deptTenantA, childB.ID); err != nil {
		t.Fatalf("查询失败: %v", err)
	} else if ok {
		t.Error("跨租户查询父节点应返回 ok=false（否则可探测其他租户的部门 ID）")
	}

	pid, ok, err := repo.FindParentID(deptTenantB, childB.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if !ok || pid != parentB.ID {
		t.Errorf("本租户应返回父节点 %d，实际 ok=%v pid=%d", parentB.ID, ok, pid)
	}
}
