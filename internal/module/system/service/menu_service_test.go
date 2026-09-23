package service

import (
	"testing"

	"go-admin/internal/middleware"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"
)

// mockMenuRepo 是 MenuRepository 的可控替身（与 mockDeptRepo 同套路）。
type mockMenuRepo struct {
	// 返回的菜单，FindByID/FindParentID 共用
	existing *model.SysMenu
	// parentQueried 记录层级校验是否被触发（ParentID 为 nil 时必须不触发）
	parentQueried bool

	updated []*model.SysMenu
}

func (m *mockMenuRepo) Create(*model.SysMenu) error { return nil }

func (m *mockMenuRepo) FindByID(id uint) (*model.SysMenu, error) {
	if m.existing != nil {
		return m.existing, nil
	}
	return &model.SysMenu{}, nil
}

func (m *mockMenuRepo) FindAll() ([]model.SysMenu, error)           { return nil, nil }
func (m *mockMenuRepo) FindAllForManage() ([]model.SysMenu, error)  { return nil, nil }
func (m *mockMenuRepo) FindMenusByRoleIDs([]uint) ([]model.SysMenu, error) {
	return nil, nil
}

func (m *mockMenuRepo) Update(menu *model.SysMenu) error {
	// 记录快照：Service 就地修改同一个对象，只存指针检测不出字段被清零
	snapshot := *menu
	m.updated = append(m.updated, &snapshot)
	return nil
}

func (m *mockMenuRepo) Delete(id uint) error { return nil }

func (m *mockMenuRepo) FindParentID(id uint) (uint, bool, error) {
	m.parentQueried = true
	return 0, true, nil
}

func (m *mockMenuRepo) CountByParentID(id uint) (int64, error) { return 0, nil }

// newTestMenuService 构造菜单服务。
//
// 必须建内存库：Update 结尾无条件调用 syncPolicies → SyncPoliciesFromRoleMenus，
// 后者直接读 database.DB —— 为 nil 时是 panic 而不是可返回的错误。
// （建 casbin_rule 等表让它正常走通，而不是靠「只记日志」掩盖。）
func newTestMenuService(t *testing.T, repo *mockMenuRepo) *menuService {
	t.Helper()
	testsupport.NewDB(t, &model.SysMenu{}, &model.SysRole{}, &model.SysRoleMenu{},
		&middleware.CasbinRule{})
	return &menuService{menuRepo: repo}
}

// fullMenu 返回一个「字段饱满」的菜单，用于验证部分更新不误伤未提供字段。
func fullMenu() *model.SysMenu {
	return &model.SysMenu{
		ParentID:   3,
		Name:       "旧菜单",
		Path:       "/old",
		Component:  "old/index",
		Icon:       "old-icon",
		Title:      "旧标题",
		Type:       1,
		Permission: "system:old:list",
		Sort:       9,
		Visible:    1,
		Status:     1,
		IsExternal: 1,
		IsCache:    1,
	}
}

// TestMenuUpdatePartialKeepsFields 「只改标题」的部分更新回归。
//
// 菜单是五个 DTO 里字段最多的一个（ParentID + 12 个业务字段），
// 一旦退回无条件赋值，请求里没带的 path/component/permission 会全被清空，
// 路由和权限码直接失效 —— 比角色/会员那两次事故影响面更大。
func TestMenuUpdatePartialKeepsFields(t *testing.T) {
	repo := &mockMenuRepo{existing: fullMenu()}
	svc := newTestMenuService(t, repo)

	if err := svc.Update(&dto.UpdateMenuRequest{ID: 1, Title: "新标题"}, 1); err != nil {
		t.Fatalf("只改标题应成功: %v", err)
	}
	if len(repo.updated) != 1 {
		t.Fatalf("应落库一次，实际 %d 次", len(repo.updated))
	}
	got := repo.updated[0]
	if got.Title != "新标题" {
		t.Errorf("标题未更新: %q", got.Title)
	}
	if got.Path != "/old" || got.Component != "old/index" ||
		got.Permission != "system:old:list" || got.Name != "旧菜单" {
		t.Errorf("部分更新清掉了路由/权限等关键字段: %+v", got)
	}
	if got.ParentID != 3 || got.Sort != 9 || got.Visible != 1 ||
		got.Status != 1 || got.IsExternal != 1 || got.IsCache != 1 ||
		got.Icon != "old-icon" || got.Type != 1 {
		t.Errorf("部分更新清掉了未提供的字段: %+v", got)
	}
}

// TestMenuUpdateMoveSkipsWhenNil ParentID 为 nil 时不触发父级校验与环检测。
//
// 「只改排序」不带 parentId；若实现拿零值 0 参与校验，等于把菜单
// 静默移动到根节点（或反过来因 0 不是「存在的父级」而报错）。
func TestMenuUpdateMoveSkipsWhenNil(t *testing.T) {
	repo := &mockMenuRepo{existing: fullMenu()}
	svc := newTestMenuService(t, repo)

	if err := svc.Update(&dto.UpdateMenuRequest{ID: 1, Sort: intptr3(1)}, 1); err != nil {
		t.Fatalf("只改排序应成功: %v", err)
	}
	if repo.parentQueried {
		t.Error("未提供 parentId 时不应触发层级校验")
	}
	if len(repo.updated) != 1 || repo.updated[0].ParentID != 3 {
		t.Errorf("parentId 不应被改成零值: %+v", repo.updated)
	}
	if repo.updated[0].Sort != 1 {
		t.Error("显式 sort 应生效")
	}
}

func intptr3(v int) *int { return &v }
