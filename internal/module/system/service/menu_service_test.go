package service

import (
	"testing"

	"go-admin/internal/common"
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
	// nameTaken 非 0 时 CountByName 返回该值，模拟「标识已被占用」
	nameTaken int64

	created []*model.SysMenu
	updated []*model.SysMenu
}

func (m *mockMenuRepo) Create(menu *model.SysMenu) error {
	// 记录快照，用于断言「校验失败时没有落库」
	snapshot := *menu
	m.created = append(m.created, &snapshot)
	return nil
}

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

// FindPermissionsByIDs 本文件的用例不涉及授权收敛校验，返回空即可。
func (m *mockMenuRepo) FindPermissionsByIDs([]uint) ([]string, error) { return nil, nil }

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

// CountByName 空标识恒为 0（与真实仓储一致：空 name 不构成重名）。
func (m *mockMenuRepo) CountByName(name string, excludeID uint) (int64, error) {
	if name == "" {
		return 0, nil
	}
	return m.nameTaken, nil
}

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

// ---- 菜单 permission 白名单校验（C1 提权链的收口点）----

// TestMenuCreateRejectsWildcardPermission 菜单的 permission 不得为通配符 `*`。
//
// 这是一条真实的提权链，不是理论风险：
// permission 会被原样编译成 Casbin 策略 `{roleCode, default, *, *}`，
// 而 model.conf 的 matcher 对 `p.obj == "*"` 直接放行 —— 持有该菜单的角色
// 立刻获得全部 protected 路由的通行权，并且让项目专门实现的授权收敛
// （OperatorHoldsPermissions / checkMenusGrantable）整体失效。
//
// 断言「没有落库」而不只是「返回了错误」：校验若放在 Create 之后，
// 会留下一个已写入 `*` 的菜单，而它已经生效 —— 光看错误码发现不了。
func TestMenuCreateRejectsWildcardPermission(t *testing.T) {
	repo := &mockMenuRepo{}
	svc := newTestMenuService(t, repo)

	err := svc.Create(&dto.CreateMenuRequest{
		Name: "越权菜单", Title: "越权菜单", Type: 2, Permission: "*",
	}, 1)
	assertBizError(t, err, common.CodeBadRequest)
	if len(repo.created) != 0 {
		t.Errorf("通配符权限码必须在落库前被拒绝，实际写入了 %d 条", len(repo.created))
	}
}

// TestMenuCreateRejectsUnregisteredPermission 未登记的权限码一并拒绝。
//
// 它不会造成放行（匹配不到任何路由），但会让菜单「配了权限却不生效」，
// 用户只能靠逐个接口试错来排查，所以给一个明确文案更划算。
func TestMenuCreateRejectsUnregisteredPermission(t *testing.T) {
	repo := &mockMenuRepo{}
	svc := newTestMenuService(t, repo)

	err := svc.Create(&dto.CreateMenuRequest{
		Name: "错配菜单", Title: "错配菜单", Type: 2,
		Permission: "definitely:not:a:registered:code",
	}, 1)
	assertBizError(t, err, common.CodeBadRequest)
	if len(repo.created) != 0 {
		t.Errorf("非法权限码必须在落库前被拒绝，实际写入了 %d 条", len(repo.created))
	}
}

// TestMenuCreateAllowsEmptyPermission 目录/纯展示型菜单没有权限码，是合法取值。
//
// 反向验证：若把空串也当成非法，所有目录型菜单都建不了 —— 这是
// 「修了提权却让正常功能不可用」的典型回归。
func TestMenuCreateAllowsEmptyPermission(t *testing.T) {
	repo := &mockMenuRepo{}
	svc := newTestMenuService(t, repo)

	if err := svc.Create(&dto.CreateMenuRequest{
		Name: "目录", Title: "目录", Type: 1, Permission: "",
	}, 1); err != nil {
		t.Fatalf("无权限码的目录型菜单应可创建: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("应落库一条，实际 %d 条", len(repo.created))
	}
}

// TestMenuCreateAllowsRegisteredPermission 真实存在的权限码必须放行。
//
// 这条同时充当「白名单不会误杀」的护栏：白名单数据来自路由注册阶段的
// RegisterPermission，若哪天登记表被清空或改名，这条会立刻红。
func TestMenuCreateAllowsRegisteredPermission(t *testing.T) {
	middleware.RegisterPermission("GET", "/api/v1/_test_menu_perm", "test:menu:perm")

	repo := &mockMenuRepo{}
	svc := newTestMenuService(t, repo)

	if err := svc.Create(&dto.CreateMenuRequest{
		Name: "正常菜单", Title: "正常菜单", Type: 2, Permission: "test:menu:perm",
	}, 1); err != nil {
		t.Fatalf("已登记的权限码应可创建: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("应落库一条，实际 %d 条", len(repo.created))
	}
}

// TestMenuUpdateRejectsWildcardPermission 改权限码时同样要过白名单。
//
// 只拦 Create 是不够的：已有菜单改成 `*` 是同一提权链的更短路径
// （不需要新建菜单，也就不会触发「菜单数量」之类的直觉检查）。
func TestMenuUpdateRejectsWildcardPermission(t *testing.T) {
	repo := &mockMenuRepo{existing: fullMenu()}
	svc := newTestMenuService(t, repo)

	wildcard := "*"
	err := svc.Update(&dto.UpdateMenuRequest{ID: 1, Permission: &wildcard}, 1)
	assertBizError(t, err, common.CodeBadRequest)
	if len(repo.updated) != 0 {
		t.Errorf("通配符权限码必须在落库前被拒绝，实际更新了 %d 次", len(repo.updated))
	}
}

// TestMenuUpdateSkipsPermissionCheckWhenNil 未提供 permission 时不触发校验。
//
// 与 ParentID 同一口径：部分更新（如只改标题）不带 permission，
// 拿零值空串去校验虽然恰好合法，但会掩盖「nil 与空串语义不同」这一事实，
// 将来白名单逻辑一变就会误伤。
func TestMenuUpdateSkipsPermissionCheckWhenNil(t *testing.T) {
	repo := &mockMenuRepo{existing: fullMenu()}
	svc := newTestMenuService(t, repo)

	if err := svc.Update(&dto.UpdateMenuRequest{ID: 1, Title: "新标题"}, 1); err != nil {
		t.Fatalf("只改标题应成功: %v", err)
	}
	if len(repo.updated) != 1 {
		t.Fatalf("应落库一次，实际 %d 次", len(repo.updated))
	}
	if repo.updated[0].Permission != "system:old:list" {
		t.Errorf("未提供的 permission 不应被改动: %q", repo.updated[0].Permission)
	}
}

// ---- 菜单标识（name）全局唯一 ----

// TestMenuCreateRejectsDuplicateName 菜单标识重名必须在落库前被拒。
//
// 为什么这条重要：name 会被写进 Vue Router 的 route.name，而 tagsView 的
// keep-alive include 列表与登出时的 removeRoute 都直接按它匹配。
// 重名时 vue-router 只保留最后一次注册，缓存与清理会落到别的页面上，
// 且**没有任何报错** —— 只能靠这个校验挡住。
func TestMenuCreateRejectsDuplicateName(t *testing.T) {
	repo := &mockMenuRepo{nameTaken: 1}
	svc := newTestMenuService(t, repo)

	err := svc.Create(&dto.CreateMenuRequest{
		Name: "User", Title: "管理员", Type: 1, Permission: "",
	}, 1)
	assertBizError(t, err, common.CodeBadRequest)
	if len(repo.created) != 0 {
		t.Errorf("重名必须在落库前被拒绝，实际写入了 %d 条", len(repo.created))
	}
}

// TestMenuUpdateRejectsDuplicateName 改标识时同样要查重（排除自身）。
func TestMenuUpdateRejectsDuplicateName(t *testing.T) {
	repo := &mockMenuRepo{existing: fullMenu(), nameTaken: 1}
	svc := newTestMenuService(t, repo)

	err := svc.Update(&dto.UpdateMenuRequest{ID: 1, Name: "User"}, 1)
	assertBizError(t, err, common.CodeBadRequest)
	if len(repo.updated) != 0 {
		t.Errorf("重名必须在落库前被拒绝，实际更新了 %d 次", len(repo.updated))
	}
}

// TestMenuCreateAllowsEmptyNameWhenRepoReportsNoConflict 空标识不触发查重。
//
// 反向验证：若把空串也送去查重，凡是「重名计数非 0」的库都会连
// 无标识菜单都建不了。真实仓储对空 name 直接返回 0，这里用桩模拟
// 「库里已有同名记录」的同时传空 name，确保判定走的是 name 本身而不是计数。
func TestMenuCreateAllowsEmptyNameWhenRepoReportsNoConflict(t *testing.T) {
	repo := &mockMenuRepo{nameTaken: 1}
	svc := newTestMenuService(t, repo)

	if err := svc.Create(&dto.CreateMenuRequest{
		Name: "", Title: "无标识", Type: 1,
	}, 1); err != nil {
		t.Fatalf("空标识不应触发重名校验: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("应落库一条，实际 %d 条", len(repo.created))
	}
}
