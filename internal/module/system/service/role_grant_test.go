package service

import (
	"path/filepath"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/middleware"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/repository"
	"go-admin/internal/testsupport"
)

// 授权收敛（P0-1）的回归测试。
//
// 背景：Casbin 策略是按「角色-菜单」关系生成的（见 middleware.SyncPoliciesFromRoleMenus），
// 因此给角色挂上菜单就等于授予对应权限码。若只校验「操作者有没有 role:edit 权限」
// 而不校验「操作者自己是否持有被授予的权限」，一个低权管理员就能：
//   ① 建一个挂满全量菜单的角色并绑给自己   → 垂直提权到超管
//   ② 直接把已有的 admin 角色绑给新账号     → 绕过菜单维度校验
//   ③ 把自己角色的 code 改成 admin          → 白拿通配策略
// 本文件把这三条路径逐一钉死。
//
// ⚠️ 这些用例会改写包级 database.DB 与 middleware 的全局状态（resolver / enforcer），
// 因此**不能** t.Parallel。

const grantOperatorUserID uint = 9001

// grantFixture 一套「低权管理员」环境：
//   - 操作者（grantOperatorUserID）持有 editor 角色
//   - editor 角色只有 menu:user:list 这一个权限码
//   - 另有一个「够不着」的权限码 menu:role:add 挂在别的菜单上
type grantFixture struct {
	svc          *roleService
	roleRepo     repository.RoleRepository
	userListMenu *model.SysMenu // 操作者已持有
	roleAddMenu  *model.SysMenu // 操作者未持有
	dirMenu      *model.SysMenu // 目录型，permission 为空
	adminMenu    *model.SysMenu // 操作者未持有，供 admin 角色使用
}

func newGrantFixture(t *testing.T) *grantFixture {
	t.Helper()

	testsupport.NewDB(t, &model.SysRole{}, &model.SysRoleMenu{}, &model.SysMenu{},
		&model.SysUser{}, &model.SysUserRole{}, &middleware.CasbinRule{})

	// 角色解析器是中间件声明的窄接口，这里注入业务侧实现（与 cmd/server 一致）
	middleware.SetRoleResolver(NewRBACRoleResolver())

	roleRepo := repository.NewRoleRepository()
	menuRepo := repository.NewMenuRepository()

	mkMenu := func(name, perm string, typ int8) *model.SysMenu {
		t.Helper()
		m := &model.SysMenu{Name: name, Title: name, Type: typ, Permission: perm, Status: 1}
		if err := menuRepo.Create(m); err != nil {
			t.Fatalf("创建菜单失败: %v", err)
		}
		return m
	}

	f := &grantFixture{
		roleRepo:     roleRepo,
		userListMenu: mkMenu("用户列表", "system:user:list", 2),
		roleAddMenu:  mkMenu("新增角色", "system:role:add", 2),
		dirMenu:      mkMenu("系统管理", "", 0),
		adminMenu:    mkMenu("配置修改", "system:config:edit", 2),
	}

	// 操作者角色：只挂了「用户列表」这一个权限
	editor := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
		Name:            "编辑者",
		Code:            "editor",
		Status:          1,
	}
	if err := roleRepo.Create(editor); err != nil {
		t.Fatalf("创建角色失败: %v", err)
	}
	if err := roleRepo.ReplaceMenus(testTenantID, editor.ID, []uint{f.userListMenu.ID}); err != nil {
		t.Fatalf("绑定角色菜单失败: %v", err)
	}
	if err := repository.NewUserRepository().ReplaceRoles(grantOperatorUserID, []uint{editor.ID}); err != nil {
		t.Fatalf("绑定用户角色失败: %v", err)
	}

	// 依据角色-菜单关系重建策略并装载 enforcer —— OperatorHoldsPermissions
	// 正是拿这份策略做判定，不能跳过这一步
	modelPath := filepath.Join("..", "..", "..", "..", "config", "casbin", "model.conf")
	if err := middleware.InitCasbin(modelPath); err != nil {
		t.Fatalf("初始化 casbin 失败: %v", err)
	}

	f.svc = &roleService{
		roleRepo: roleRepo,
		menuRepo: menuRepo,
	}
	return f
}

// TestRoleCreateRejectsPrivilegeEscalation 低权管理员不得授予自己未持有的权限码。
func TestRoleCreateRejectsPrivilegeEscalation(t *testing.T) {
	f := newGrantFixture(t)

	// 超集：勾选了自己没有的 system:role:add → 必须 403
	err := f.svc.Create(&dto.CreateRoleRequest{
		Name:    "提权角色",
		Code:    "escalated",
		Status:  1,
		MenuIds: []uint{f.roleAddMenu.ID},
	}, grantOperatorUserID, testTenantID)
	assertBizError(t, err, common.CodeForbidden)

	// 校验必须在落库之前：失败不能留下半成品角色
	if count, _ := f.roleRepo.CountByCode("escalated", 0); count != 0 {
		t.Errorf("被拒绝的创建不应落库，实际存在 %d 条", count)
	}
}

// TestRoleCreateAllowsSubset 子集放行：只授予自己已持有的权限码是合法的。
func TestRoleCreateAllowsSubset(t *testing.T) {
	f := newGrantFixture(t)

	if err := f.svc.Create(&dto.CreateRoleRequest{
		Name:    "同权角色",
		Code:    "samelevel",
		Status:  1,
		MenuIds: []uint{f.userListMenu.ID},
	}, grantOperatorUserID, testTenantID); err != nil {
		t.Fatalf("授予自己已持有的权限应成功: %v", err)
	}
}

// TestRoleCreateIgnoresDirectoryMenus 目录型菜单（permission 为空）不构成授权，
// 不应拦住授权操作 —— 否则低权管理员连建目录都做不到。
func TestRoleCreateIgnoresDirectoryMenus(t *testing.T) {
	f := newGrantFixture(t)

	if err := f.svc.Create(&dto.CreateRoleRequest{
		Name:    "只挂目录",
		Code:    "dironly",
		Status:  1,
		MenuIds: []uint{f.dirMenu.ID},
	}, grantOperatorUserID, testTenantID); err != nil {
		t.Fatalf("仅勾选目录型菜单不应被拦截: %v", err)
	}
}

// TestRoleUpdateRejectsPrivilegeEscalation 改权限同样要收敛 ——
// 「先建一个空角色，再慢慢加权限」是绕开创建时校验的经典走法。
func TestRoleUpdateRejectsPrivilegeEscalation(t *testing.T) {
	f := newGrantFixture(t)

	role := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
		Name:            "我的角色",
		Code:            "mine",
		Status:          1,
	}
	if err := f.roleRepo.Create(role); err != nil {
		t.Fatalf("创建角色失败: %v", err)
	}

	err := f.svc.Update(&dto.UpdateRoleRequest{ID: role.ID, MenuIds: []uint{f.roleAddMenu.ID}},
		grantOperatorUserID, testTenantID)
	assertBizError(t, err, common.CodeForbidden)

	// 权限不得被写进去
	menuIDs, _ := f.roleRepo.FindMenuIDsByRoleID(testTenantID, role.ID)
	if len(menuIDs) != 0 {
		t.Errorf("被拒绝的授权不应落库，实际 %v", menuIDs)
	}
}

// TestRoleUpdateAdminOperatorBypassesCheck admin 持有通配策略，收敛判定对它恒真。
func TestRoleUpdateAdminOperatorBypassesCheck(t *testing.T) {
	f := newGrantFixture(t)

	// 让操作者同时持有 admin 角色（admin 的权限在同步策略时被写成 */*）
	admin := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
		Name:            "超级管理员",
		Code:            middleware.AdminRoleCode,
		Status:          1,
	}
	if err := f.roleRepo.Create(admin); err != nil {
		t.Fatalf("创建 admin 角色失败: %v", err)
	}
	// admin 角色刻意只挂一个普通菜单：它真正拥有的权限来自通配策略，
	// 与菜单无关。若实现改成「比对目标角色的菜单权限」，这里就会误判为无权。
	if err := f.roleRepo.ReplaceMenus(testTenantID, admin.ID, []uint{f.adminMenu.ID}); err != nil {
		t.Fatalf("绑定菜单失败: %v", err)
	}
	if err := repository.NewUserRepository().ReplaceRoles(grantOperatorUserID, []uint{admin.ID}); err != nil {
		t.Fatalf("绑定用户角色失败: %v", err)
	}
	if err := middleware.InitCasbin(filepath.Join("..", "..", "..", "..", "config", "casbin", "model.conf")); err != nil {
		t.Fatalf("初始化 casbin 失败: %v", err)
	}

	role := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
		Name:            "任意角色",
		Code:            "anything",
		Status:          1,
	}
	if err := f.roleRepo.Create(role); err != nil {
		t.Fatalf("创建角色失败: %v", err)
	}

	if err := f.svc.Update(&dto.UpdateRoleRequest{ID: role.ID, MenuIds: []uint{f.roleAddMenu.ID}},
		grantOperatorUserID, testTenantID); err != nil {
		t.Fatalf("admin 操作者不应被拦截: %v", err)
	}
}

// TestRoleReservedCodeGuards admin 编码等价于「全部权限」，非 admin 不得染指。
//
// 这里刻意**不预置** code=admin 的角色：否则重名校验会先返回 400，
// 把「保留编码护栏」这条路径遮住 —— 而护栏真正要拦的是「admin 角色被删掉后
// 有人抢先占用这个编码」的情形（通配策略不依赖菜单，只依赖编码）。
func TestRoleReservedCodeGuards(t *testing.T) {
	f := newGrantFixture(t)

	t.Run("新建 admin 角色被拒", func(t *testing.T) {
		err := f.svc.Create(&dto.CreateRoleRequest{Name: "伪超管", Code: middleware.AdminRoleCode, Status: 1},
			grantOperatorUserID, testTenantID)
		assertBizError(t, err, common.CodeForbidden)

		if count, _ := f.roleRepo.CountByCode(middleware.AdminRoleCode, 0); count != 0 {
			t.Errorf("被拒绝的创建不应落库，实际 %d 条", count)
		}
	})

	t.Run("改名为 admin 被拒", func(t *testing.T) {
		mine := &model.SysRole{
			TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
			Name:            "我的角色",
			Code:            "mine",
			Status:          1,
		}
		if err := f.roleRepo.Create(mine); err != nil {
			t.Fatalf("创建角色失败: %v", err)
		}

		err := f.svc.Update(&dto.UpdateRoleRequest{ID: mine.ID, Code: middleware.AdminRoleCode},
			grantOperatorUserID, testTenantID)
		assertBizError(t, err, common.CodeForbidden)

		got, _ := f.roleRepo.FindByID(testTenantID, mine.ID)
		if got.Code != "mine" {
			t.Errorf("被拒绝的改名不应生效，实际 code=%q", got.Code)
		}
	})
}

// TestRoleCannotModifyAdminRole 非 admin 操作者不得改动 admin 角色本身。
//
// 这条最容易被忽略：admin 的通配策略不依赖菜单，但**依赖角色编码**，
// 把 admin 改名成别的编码，等于把通配权限交给了那个编码。
func TestRoleCannotModifyAdminRole(t *testing.T) {
	f := newGrantFixture(t)

	admin := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
		Name:            "超级管理员",
		Code:            middleware.AdminRoleCode,
		Status:          1,
	}
	if err := f.roleRepo.Create(admin); err != nil {
		t.Fatalf("创建 admin 角色失败: %v", err)
	}

	err := f.svc.Update(&dto.UpdateRoleRequest{ID: admin.ID, Name: "劫持"}, grantOperatorUserID, testTenantID)
	assertBizError(t, err, common.CodeForbidden)

	got, _ := f.roleRepo.FindByID(testTenantID, admin.ID)
	if got.Name == "劫持" {
		t.Error("被拒绝的修改竟然生效了")
	}
}

// TestEnsureRolesGrantable admin 角色不得由非 admin 授予他人。
//
// 这是比「改角色菜单」更短的一条提权路径：直接把 admin 角色绑到账号上。
// 注意 admin 角色名下可能只挂了少数菜单，所以必须按「角色编码」判定，
// 不能只看它的菜单权限集合。
func TestEnsureRolesGrantable(t *testing.T) {
	f := newGrantFixture(t)

	admin := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
		Name:            "超级管理员",
		Code:            middleware.AdminRoleCode,
		Status:          1,
	}
	if err := f.roleRepo.Create(admin); err != nil {
		t.Fatalf("创建 admin 角色失败: %v", err)
	}

	t.Run("授予 admin 角色被拒", func(t *testing.T) {
		err := f.svc.EnsureRolesGrantable(testTenantID, grantOperatorUserID, []model.SysRole{*admin})
		assertBizError(t, err, common.CodeForbidden)
	})

	t.Run("授予权限超出自身的角色被拒", func(t *testing.T) {
		high := &model.SysRole{
			TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
			Name:            "高权角色",
			Code:            "high",
			Status:          1,
		}
		if err := f.roleRepo.Create(high); err != nil {
			t.Fatalf("创建角色失败: %v", err)
		}
		if err := f.roleRepo.ReplaceMenus(testTenantID, high.ID, []uint{f.roleAddMenu.ID}); err != nil {
			t.Fatalf("绑定菜单失败: %v", err)
		}

		err := f.svc.EnsureRolesGrantable(testTenantID, grantOperatorUserID, []model.SysRole{*high})
		assertBizError(t, err, common.CodeForbidden)
	})

	t.Run("授予权限为子集的角色放行", func(t *testing.T) {
		low := &model.SysRole{
			TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
			Name:            "同权角色",
			Code:            "low",
			Status:          1,
		}
		if err := f.roleRepo.Create(low); err != nil {
			t.Fatalf("创建角色失败: %v", err)
		}
		if err := f.roleRepo.ReplaceMenus(testTenantID, low.ID, []uint{f.userListMenu.ID}); err != nil {
			t.Fatalf("绑定菜单失败: %v", err)
		}

		if err := f.svc.EnsureRolesGrantable(testTenantID, grantOperatorUserID, []model.SysRole{*low}); err != nil {
			t.Fatalf("授予子集角色应放行: %v", err)
		}
	})

	t.Run("空角色列表不拦截", func(t *testing.T) {
		if err := f.svc.EnsureRolesGrantable(testTenantID, grantOperatorUserID, nil); err != nil {
			t.Fatalf("空列表应放行: %v", err)
		}
	})
}

// TestUserServiceRejectsGrantingAdminRole 端到端：通过「给用户绑角色」接口
// 授予 admin 角色同样要被拦住 —— 这条路径此前完全不受限。
func TestUserServiceRejectsGrantingAdminRole(t *testing.T) {
	f := newGrantFixture(t)

	admin := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
		Name:            "超级管理员",
		Code:            middleware.AdminRoleCode,
		Status:          1,
	}
	if err := f.roleRepo.Create(admin); err != nil {
		t.Fatalf("创建 admin 角色失败: %v", err)
	}

	userRepo := repository.NewUserRepository()
	target := &model.SysUser{
		TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
		Username:        "victim",
		Status:          1,
	}
	if err := userRepo.Create(target); err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	svc := NewUserService()
	err := svc.UpdateRoles(testTenantID, grantOperatorUserID, &dto.UpdateUserRolesRequest{
		ID:      target.ID,
		RoleIds: []uint{admin.ID},
	})
	assertBizError(t, err, common.CodeForbidden)

	roleIDs, _ := userRepo.FindRoleIDsByUserID(target.ID)
	if len(roleIDs) != 0 {
		t.Errorf("被拒绝的绑定不应落库，实际 %v", roleIDs)
	}
}
