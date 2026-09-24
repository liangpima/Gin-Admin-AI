package service

import (
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/repository"
	"go-admin/internal/testsupport"
)

type mockDeptRepo struct {
	countByParentFn func(parentID uint) (int64, error)
	findParentFn    func(id uint) (uint, bool, error)
	findByIDFn      func(id uint) (*model.SysDept, error)

	deletedIDs []uint
	updated    []*model.SysDept
	// tenantIDs 记录各方法实际收到的租户 ID。
	// 部门是租户内数据，Service 一旦漏传 tenantID，Repository 就会拿到 0，
	// 而 TenantScope(db,0) **不过滤** —— 静默退化成全表查询。
	// 这个切片让「漏传」在测试里立刻暴露（见 TestDeptServiceThreadsTenantID）。
	tenantIDs []uint
}

func (m *mockDeptRepo) record(tenantID uint) { m.tenantIDs = append(m.tenantIDs, tenantID) }

func (m *mockDeptRepo) Create(*model.SysDept) error { return nil }

func (m *mockDeptRepo) FindByID(tenantID, id uint) (*model.SysDept, error) {
	m.record(tenantID)
	if m.findByIDFn != nil {
		return m.findByIDFn(id)
	}
	return &model.SysDept{}, nil
}

func (m *mockDeptRepo) FindAll(tenantID uint) ([]model.SysDept, error) {
	m.record(tenantID)
	return nil, nil
}

func (m *mockDeptRepo) Update(tenantID uint, dept *model.SysDept) error {
	m.record(tenantID)
	// 记录快照：Service 就地修改同一个对象，只存指针检测不出字段被清零
	snapshot := *dept
	m.updated = append(m.updated, &snapshot)
	return nil
}

func (m *mockDeptRepo) Delete(tenantID, id uint) error {
	m.record(tenantID)
	m.deletedIDs = append(m.deletedIDs, id)
	return nil
}

func (m *mockDeptRepo) CountByParentID(tenantID, parentID uint) (int64, error) {
	m.record(tenantID)
	if m.countByParentFn != nil {
		return m.countByParentFn(parentID)
	}
	return 0, nil
}

func (m *mockDeptRepo) FindParentID(tenantID, id uint) (uint, bool, error) {
	m.record(tenantID)
	if m.findParentFn != nil {
		return m.findParentFn(id)
	}
	return 0, true, nil
}

func newTestDeptService(repo *mockDeptRepo) *deptService {
	return &deptService{deptRepo: repo}
}

// TestDeptServiceDelete 删除部门的前置校验。
//
// 存在子部门时必须拒绝：直接删父节点会让子部门的 parent_id 悬空，
// 而 FindTree 从 parent_id=0 递归构建，这棵子树会「从界面上消失」，
// 数据却还在库里 —— 既看不见也删不掉。
func TestDeptServiceDelete(t *testing.T) {
	t.Run("存在下级部门时拒绝删除", func(t *testing.T) {
		repo := &mockDeptRepo{
			countByParentFn: func(uint) (int64, error) { return 2, nil },
		}
		svc := newTestDeptService(repo)

		err := svc.Delete(testTenantID, 1)

		if err != ErrDeptHasChildren {
			t.Errorf("应返回 ErrDeptHasChildren，实际: %v", err)
		}
		// 必须是 400（调用方问题）而不是 500（服务端故障）
		assertBizError(t, err, common.CodeBadRequest)
		if len(repo.deletedIDs) != 0 {
			t.Error("校验未通过时不应真正删除")
		}
	})

	t.Run("无下级部门时正常删除", func(t *testing.T) {
		repo := &mockDeptRepo{
			countByParentFn: func(uint) (int64, error) { return 0, nil },
		}
		svc := newTestDeptService(repo)

		if err := svc.Delete(testTenantID, 5); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(repo.deletedIDs) != 1 || repo.deletedIDs[0] != 5 {
			t.Errorf("应删除 id=5，实际 %v", repo.deletedIDs)
		}
	})
}

// TestDeptServiceUpdateCycle 把部门挂到自己或自己的下级之下必须被拒绝。
//
// 环不会导致递归死循环（每个节点只有一个父节点），但那棵子树会从界面上消失。
// uintPtr / 是测试用的取地址辅助：UpdateDeptRequest.ParentID 是 *uint，
// 复合字面量里不能直接写地址，需要这个函数。
func uintPtr(v uint) *uint { return &v }

func TestDeptServiceUpdateCycle(t *testing.T) {
	t.Run("挂到自己的下级之下被拒绝", func(t *testing.T) {
		repo := &mockDeptRepo{
			// 目标父节点 3 的父链是 3 → 2 → 1 → 0，而当前部门 id 是 2，
			// 说明 2 是 3 的祖先，挂过去会成环
			findParentFn: func(id uint) (uint, bool, error) {
				chain := map[uint]uint{3: 2, 2: 1, 1: 0}
				p, ok := chain[id]
				return p, ok, nil
			},
		}
		svc := newTestDeptService(repo)

		err := svc.Update(&dto.UpdateDeptRequest{ID: 2, ParentID: uintPtr(3)}, 1, testTenantID)

		assertBizError(t, err, common.CodeBadRequest)
		if len(repo.updated) != 0 {
			t.Error("成环时不应写入")
		}
	})

	t.Run("挂到自己之下被拒绝", func(t *testing.T) {
		repo := &mockDeptRepo{}
		svc := newTestDeptService(repo)

		err := svc.Update(&dto.UpdateDeptRequest{ID: 7, ParentID: uintPtr(7)}, 1, testTenantID)

		assertBizError(t, err, common.CodeBadRequest)
	})

	t.Run("正常的层级调整允许通过", func(t *testing.T) {
		repo := &mockDeptRepo{
			findParentFn: func(id uint) (uint, bool, error) {
				chain := map[uint]uint{9: 8, 8: 0}
				p, ok := chain[id]
				return p, ok, nil
			},
		}
		svc := newTestDeptService(repo)

		// 把部门 5 挂到部门 9 之下：9 的父链是 9→8→0，不含 5，不成环
		if err := svc.Update(&dto.UpdateDeptRequest{ID: 5, ParentID: uintPtr(9)}, 1, testTenantID); err != nil {
			t.Fatalf("不应报错，实际: %v", err)
		}
		if len(repo.updated) != 1 {
			t.Error("应写入一次更新")
		}
	})
}

// TestDeptUpdatePartialKeepsFields 「只改名」的部分更新回归。
//
// 与角色/会员同一套约定：指针字段 nil 即「本次不涉及」。
// 曾踩过的坑是无条件赋值把未提供字段清成零值（角色、会员各一次）。
func TestDeptUpdatePartialKeepsFields(t *testing.T) {
	existing := &model.SysDept{
		ParentID: 2,
		Name:     "旧名称",
		Sort:     7,
		Leader:   "张三",
		Phone:    "13800000002",
		Email:    "dept@example.com",
		Status:   1,
	}
	repo := &mockDeptRepo{findByIDFn: func(uint) (*model.SysDept, error) {
		return existing, nil
	}}
	svc := newTestDeptService(repo)

	if err := svc.Update(&dto.UpdateDeptRequest{ID: 1, Name: "新名称"}, 1, testTenantID); err != nil {
		t.Fatalf("只改名应成功: %v", err)
	}
	if len(repo.updated) != 1 {
		t.Fatalf("应落库一次，实际 %d 次", len(repo.updated))
	}
	got := repo.updated[0]
	if got.Name != "新名称" {
		t.Errorf("名称未更新: %q", got.Name)
	}
	// 未提供的字段一个都不能动
	if got.ParentID != 2 || got.Sort != 7 || got.Leader != "张三" ||
		got.Phone != "13800000002" || got.Email != "dept@example.com" || got.Status != 1 {
		t.Errorf("部分更新清掉了未提供的字段: %+v", got)
	}
}

// TestDeptUpdateMoveSkipsWhenNil ParentID 为 nil 时不得触发父级校验/环检测。
//
// 「只改名」的请求不带 parentId；如果实现拿零值 0 去做环检测或移动，
// 等于把部门静默移动到根节点。mock 的 findParentFn 会记录是否被调用。
func TestDeptUpdateMoveSkipsWhenNil(t *testing.T) {
	parentQueried := false
	repo := &mockDeptRepo{
		findByIDFn: func(uint) (*model.SysDept, error) {
			return &model.SysDept{ParentID: 5, Name: "n", Status: 1}, nil
		},
		findParentFn: func(uint) (uint, bool, error) {
			parentQueried = true
			return 0, true, nil
		},
	}
	svc := newTestDeptService(repo)

	if err := svc.Update(&dto.UpdateDeptRequest{ID: 1, Sort: intptr2(3)}, 1, testTenantID); err != nil {
		t.Fatalf("只改排序应成功: %v", err)
	}
	if parentQueried {
		t.Error("未提供 parentId 时不应触发层级校验")
	}
	if len(repo.updated) != 1 || repo.updated[0].ParentID != 5 {
		t.Errorf("parentId 不应被改成零值: %+v", repo.updated)
	}
	if repo.updated[0].Sort != 3 {
		t.Error("显式 sort 应生效")
	}
}

func intptr2(v int) *int { return &v }

// TestDeptServiceThreadsTenantID Service 必须把 tenantID 原样透传到 Repository。
//
// 这是 P1-1 的核心回归保护：部门此前是全局表，改造后所有查询都要带租户过滤，
// 而漏传 tenantID 不会报错 —— Repository 收到 0，`TenantScope(db,0)` 不过滤，
// 于是静默退化成全表查询，接口照常返回 200，只是把别的租户的部门也带出来了。
//
// 断言方式：mock 记录每个方法实际收到的 tenantID，逐个检查是否等于入参。
// 任何一处漏传（例如把 tenantID 写死成 0、或参数顺序写反）都会立刻失败。
func TestDeptServiceThreadsTenantID(t *testing.T) {
	const tenantID uint = 42

	t.Run("Create 把部门建到操作者所属租户下", func(t *testing.T) {
		var created *model.SysDept
		repo := &mockDeptRepo{}
		svc := &deptService{deptRepo: &captureCreateRepo{mockDeptRepo: repo, on: func(d *model.SysDept) { created = d }}}

		if err := svc.Create(&dto.CreateDeptRequest{Name: "研发部", Status: 1}, 7, tenantID); err != nil {
			t.Fatalf("创建失败: %v", err)
		}
		if created == nil {
			t.Fatal("应落库一次")
		}
		// 租户只能来自登录上下文，不能由请求体指定
		if created.TenantID != tenantID {
			t.Errorf("部门租户应为 %d，实际 %d", tenantID, created.TenantID)
		}
		if created.CreateBy != 7 {
			t.Errorf("创建者应为 7，实际 %d", created.CreateBy)
		}
	})

	t.Run("查询与删除都带上租户", func(t *testing.T) {
		repo := &mockDeptRepo{}
		svc := newTestDeptService(repo)

		if _, err := svc.FindByID(tenantID, 1); err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if _, err := svc.FindTree(tenantID); err != nil {
			t.Fatalf("查询树失败: %v", err)
		}
		if err := svc.Delete(tenantID, 1); err != nil {
			t.Fatalf("删除失败: %v", err)
		}
		if err := svc.Update(&dto.UpdateDeptRequest{ID: 1, Name: "改名"}, 7, tenantID); err != nil {
			t.Fatalf("更新失败: %v", err)
		}

		if len(repo.tenantIDs) == 0 {
			t.Fatal("mock 未记录到任何租户 ID，说明测试没有覆盖到 Repository 调用")
		}
		for i, got := range repo.tenantIDs {
			if got != tenantID {
				t.Errorf("第 %d 次调用收到的租户 ID 为 %d，期望 %d（漏传会让 TenantScope 退化成全表查询）",
					i+1, got, tenantID)
			}
		}
	})

	t.Run("校验父级时也带租户（否则可挂到别租户的部门之下）", func(t *testing.T) {
		repo := &mockDeptRepo{}
		svc := newTestDeptService(repo)

		// 挂到 id=99 之下：校验父级存在性必须按本租户查
		err := svc.Update(&dto.UpdateDeptRequest{ID: 1, ParentID: uintPtr(99)}, 7, tenantID)
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		for _, got := range repo.tenantIDs {
			if got != tenantID {
				t.Errorf("父级校验收到的租户 ID 为 %d，期望 %d", got, tenantID)
			}
		}
	})
}

// captureCreateRepo 包一层以捕获 Create 入参（Create 不在 mockDeptRepo 的记录范围内，
// 因为它没有租户参数 —— 租户写在 dept 对象上）。
type captureCreateRepo struct {
	*mockDeptRepo
	on func(*model.SysDept)
}

func (c *captureCreateRepo) Create(dept *model.SysDept) error {
	c.on(dept)
	return nil
}

// TestDeptServiceFindTreeKeepsOrphansVisible 租户的部门树不能因为父节点被过滤而空白。
//
// 这是 sys_dept 改为租户内数据后最容易踩的坑，也是迁移脚本刻意分两步回填的原因：
// 历史数据的父部门可能仍留在平台级（tenant_id=0）。租户账号查询自己的部门时，
// 子部门（tenant_id=本租户）的父指针指向一个**不在结果集里**的节点。
//
// 若沿用 common.BuildTree（只认 parent_id=0 为根），返回的会是空数组 ——
// 部门管理页一片空白、新建用户时选不到部门，而数据其实完好无损。
// 本用例用真实库（内存 SQLite）端到端验证这条路径。
func TestDeptServiceFindTreeKeepsOrphansVisible(t *testing.T) {
	testsupport.NewDB(t, &model.SysDept{})
	repo := repository.NewDeptRepository()
	svc := &deptService{deptRepo: repo}

	seed := func(tenantID uint, name string, parentID uint) *model.SysDept {
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

	// 平台级根部门（tenant_id=0，租户查询时会被过滤掉）
	platformRoot := seed(0, "集团总部", 0)
	// 租户 1 的两个部门：一个挂在平台级根下（父节点不在结果集里），一个挂在自己下面
	tenantRoot := seed(1, "租户1-研发", platformRoot.ID)
	tenantChild := seed(1, "租户1-前端组", tenantRoot.ID)

	tree, err := svc.FindTree(1)
	if err != nil {
		t.Fatalf("查询部门树失败: %v", err)
	}
	if len(tree) == 0 {
		t.Fatal("部门树不应为空：父节点被租户过滤掉时必须把子节点提升为顶层节点")
	}
	if len(tree) != 1 {
		t.Fatalf("应只有 1 个顶层节点（tenantRoot 被提升），实际 %d 个", len(tree))
	}
	if tree[0].ID != tenantRoot.ID {
		t.Errorf("顶层节点应是 %q，实际 %q", tenantRoot.Name, tree[0].Name)
	}
	// 层级关系不能因为提升而丢失
	if len(tree[0].Children) != 1 || tree[0].Children[0].ID != tenantChild.ID {
		t.Errorf("子部门应仍挂在原父节点下，实际 %+v", tree[0].Children)
	}

	// 平台级账号（tenantID=0）不受影响：不过滤，看到完整层级
	all, err := svc.FindTree(0)
	if err != nil {
		t.Fatalf("平台级查询失败: %v", err)
	}
	if len(all) != 1 || all[0].ID != platformRoot.ID {
		t.Fatalf("平台级应看到 1 个顶层节点（集团总部），实际 %+v", all)
	}
	if len(all[0].Children) != 1 || all[0].Children[0].ID != tenantRoot.ID {
		t.Errorf("平台级视图的层级不对: %+v", all[0].Children)
	}
}
