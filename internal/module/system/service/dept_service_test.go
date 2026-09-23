package service

import (
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
)

type mockDeptRepo struct {
	countByParentFn func(parentID uint) (int64, error)
	findParentFn    func(id uint) (uint, bool, error)
	findByIDFn      func(id uint) (*model.SysDept, error)

	deletedIDs []uint
	updated    []*model.SysDept
}

func (m *mockDeptRepo) Create(*model.SysDept) error { return nil }

func (m *mockDeptRepo) FindByID(id uint) (*model.SysDept, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(id)
	}
	return &model.SysDept{}, nil
}

func (m *mockDeptRepo) FindAll() ([]model.SysDept, error) { return nil, nil }

func (m *mockDeptRepo) Update(dept *model.SysDept) error {
	// 记录快照：Service 就地修改同一个对象，只存指针检测不出字段被清零
	snapshot := *dept
	m.updated = append(m.updated, &snapshot)
	return nil
}

func (m *mockDeptRepo) Delete(id uint) error {
	m.deletedIDs = append(m.deletedIDs, id)
	return nil
}

func (m *mockDeptRepo) CountByParentID(parentID uint) (int64, error) {
	if m.countByParentFn != nil {
		return m.countByParentFn(parentID)
	}
	return 0, nil
}

func (m *mockDeptRepo) FindParentID(id uint) (uint, bool, error) {
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

		err := svc.Delete(1)

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

		if err := svc.Delete(5); err != nil {
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

		err := svc.Update(&dto.UpdateDeptRequest{ID: 2, ParentID: uintPtr(3)}, 1)

		assertBizError(t, err, common.CodeBadRequest)
		if len(repo.updated) != 0 {
			t.Error("成环时不应写入")
		}
	})

	t.Run("挂到自己之下被拒绝", func(t *testing.T) {
		repo := &mockDeptRepo{}
		svc := newTestDeptService(repo)

		err := svc.Update(&dto.UpdateDeptRequest{ID: 7, ParentID: uintPtr(7)}, 1)

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
		if err := svc.Update(&dto.UpdateDeptRequest{ID: 5, ParentID: uintPtr(9)}, 1); err != nil {
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

	if err := svc.Update(&dto.UpdateDeptRequest{ID: 1, Name: "新名称"}, 1); err != nil {
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

	if err := svc.Update(&dto.UpdateDeptRequest{ID: 1, Sort: intptr2(3)}, 1); err != nil {
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
