package service

import (
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/middleware"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/repository"
	"go-admin/internal/testsupport"
)

// 撤销方向的授权收敛（第三轮审查 H2）的回归测试。
//
// 背景：授权收敛此前只做了「授予」方向 —— `normalizeRoleIDs` 会调用
// `EnsureRolesGrantable` 拦住「把 admin 角色绑给别人」。但撤销方向为零：
//
//   - `normalizeRoleIDs` 对**空数组**提前 return，于是
//     `PUT /system/user/roles {"id":<超管>,"roleIds":[]}` 直接清空超管的角色绑定；
//   - `UpdateStatus` / `Delete` 连 `operatorID` 参数都没有，
//     结构上不可能判定「操作者能否动这个账号」。
//
// 后果：一个**平台级**（tenant_id=0，即 TenantScope 不过滤）且持有
// `system:user:edit` 但不持有 admin 的账号，可以把超管清权 / 禁用 / 删除，
// 全程无告警。
//
// ⚠️ 这些用例会改写包级 database.DB 与 middleware 的全局状态（resolver），
// 因此**不能** t.Parallel。

const (
	adminGuardOperatorID uint = 9101 // 低权操作者（只持 viewer 角色）
	adminGuardAdminOpID  uint = 9104 // 超管操作者
)

type adminGuardFixture struct {
	svc        *userService
	userRepo   repository.UserRepository
	adminRole  *model.SysRole
	viewerRole *model.SysRole
	adminUser  *model.SysUser // 持有 admin 角色
	plainUser  *model.SysUser // 无任何角色
	peerUser   *model.SysUser // 持有 viewer 角色（非 admin）
}

func newAdminGuardFixture(t *testing.T) *adminGuardFixture {
	t.Helper()

	testsupport.NewDB(t, &model.SysUser{}, &model.SysUserRole{}, &model.SysUserPost{},
		&model.SysRole{}, &model.SysMenu{}, &model.SysRoleMenu{}, &model.SysPost{},
		&middleware.CasbinRule{})

	// 角色解析器是中间件声明的窄接口，这里注入业务侧实现（与 cmd/server 一致）
	middleware.SetRoleResolver(NewRBACRoleResolver())

	roleRepo := repository.NewRoleRepository()
	userRepo := repository.NewUserRepository()

	adminRole := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
		Name:            "超级管理员",
		Code:            middleware.AdminRoleCode,
		Status:          1,
	}
	viewerRole := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
		Name:            "只读",
		Code:            "viewer",
		Status:          1,
	}
	for _, r := range []*model.SysRole{adminRole, viewerRole} {
		if err := roleRepo.Create(r); err != nil {
			t.Fatalf("创建角色 %s 失败: %v", r.Code, err)
		}
	}

	mkUser := func(name string) *model.SysUser {
		t.Helper()
		u := &model.SysUser{
			TenantBaseModel: common.TenantBaseModel{TenantID: testTenantID},
			Username:        name,
			Password:        "x",
			Status:          1,
		}
		if err := userRepo.Create(u); err != nil {
			t.Fatalf("创建用户 %s 失败: %v", name, err)
		}
		return u
	}

	adminUser := mkUser("super")
	plainUser := mkUser("plain")
	peerUser := mkUser("peer")
	mkUser("operator")
	mkUser("adminop")

	if err := userRepo.ReplaceRoles(adminUser.ID, []uint{adminRole.ID}); err != nil {
		t.Fatalf("绑定 admin 角色失败: %v", err)
	}
	if err := userRepo.ReplaceRoles(adminGuardOperatorID, []uint{viewerRole.ID}); err != nil {
		t.Fatalf("绑定 viewer 角色失败: %v", err)
	}
	if err := userRepo.ReplaceRoles(adminGuardAdminOpID, []uint{adminRole.ID}); err != nil {
		t.Fatalf("绑定超管操作者角色失败: %v", err)
	}
	// peerUser 持有**非 admin** 角色：用来验证收敛只针对超管目标，
	// 不会顺带把「操作另一个同权管理员」也拒掉
	if err := userRepo.ReplaceRoles(peerUser.ID, []uint{viewerRole.ID}); err != nil {
		t.Fatalf("绑定 peer 角色失败: %v", err)
	}

	// 角色缓存键是 rbac:roles:<tenant>:<user>，若测试环境恰好有 Redis，
	// 上一组用例的结果会串到下一组 —— 显式清掉，让判定只依赖本用例的数据
	for _, id := range []uint{adminGuardOperatorID, adminGuardAdminOpID, adminUser.ID, peerUser.ID} {
		middleware.ClearRoleCache(testTenantID, id)
	}

	return &adminGuardFixture{
		svc: &userService{
			userRepo:    userRepo,
			roleService: NewRoleService(),
		},
		userRepo:   userRepo,
		adminRole:  adminRole,
		viewerRole: viewerRole,
		adminUser:  adminUser,
		plainUser:  plainUser,
		peerUser:   peerUser,
	}
}

// TestNonAdminCannotClearAdminRoles 低权操作者不得用「提交空数组」清空超管角色。
//
// 这条路径正是 normalizeRoleIDs 的空数组提前返回漏掉的：授予方向有校验，
// 但「我不提交任何角色」绕过了它，ReplaceRoles(id, nil) 直接把超管清成无角色。
func TestNonAdminCannotClearAdminRoles(t *testing.T) {
	f := newAdminGuardFixture(t)

	err := f.svc.UpdateRoles(testTenantID, adminGuardOperatorID, &dto.UpdateUserRolesRequest{
		ID:      f.adminUser.ID,
		RoleIds: []uint{},
	})
	assertBizError(t, err, common.CodeForbidden)

	// 校验必须在写入之前：被拒绝后角色绑定必须原样保留
	ids, err := f.userRepo.FindRoleIDsByUserID(f.adminUser.ID)
	if err != nil {
		t.Fatalf("回查角色失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != f.adminRole.ID {
		t.Errorf("被拒绝的清空不应生效，实际角色绑定为 %v", ids)
	}
}

// TestNonAdminCannotDisableAdmin 低权操作者不得禁用超管。
func TestNonAdminCannotDisableAdmin(t *testing.T) {
	f := newAdminGuardFixture(t)

	assertBizError(t,
		f.svc.UpdateStatus(testTenantID, adminGuardOperatorID,
			&dto.StatusRequest{ID: f.adminUser.ID, Status: common.StatusDisabled}),
		common.CodeForbidden)

	// 必须在写库之前拦下：否则账号已被禁用，撤销不掉
	u, err := f.userRepo.FindByID(testTenantID, f.adminUser.ID)
	if err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if u.Status != common.StatusEnabled {
		t.Errorf("被拒绝的禁用不应生效，实际 status=%d", u.Status)
	}
}

// TestNonAdminCannotDeleteAdmin 低权操作者不得删除超管。
func TestNonAdminCannotDeleteAdmin(t *testing.T) {
	f := newAdminGuardFixture(t)

	assertBizError(t, f.svc.Delete(testTenantID, adminGuardOperatorID, f.adminUser.ID),
		common.CodeForbidden)

	if _, err := f.userRepo.FindByID(testTenantID, f.adminUser.ID); err != nil {
		t.Errorf("被拒绝的删除不应生效，实际查不到该用户: %v", err)
	}
}

// TestAdminOperatorCanStillOperateAdmin 收敛不能一刀切：超管操作者仍可操作超管账号。
//
// 没有这条，「一律拒绝」也会让上面三条通过 —— 那等于把功能改坏。
func TestAdminOperatorCanStillOperateAdmin(t *testing.T) {
	f := newAdminGuardFixture(t)

	if err := f.svc.UpdateStatus(testTenantID, adminGuardAdminOpID,
		&dto.StatusRequest{ID: f.adminUser.ID, Status: common.StatusDisabled}); err != nil {
		t.Fatalf("超管操作者禁用超管账号应被允许: %v", err)
	}

	u, err := f.userRepo.FindByID(testTenantID, f.adminUser.ID)
	if err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if u.Status != common.StatusDisabled {
		t.Errorf("状态应已更新为禁用，实际 status=%d", u.Status)
	}
}

// TestNonAdminCanStillOperatePlainUser 收敛不得波及普通账号。
//
// 若为了实现「目标持有 admin 才拦」而顺手对所有目标都做权限码比对，
// 「管理员停用一个无权普通账号」也会被拒 —— 那是把既有功能改坏。
func TestNonAdminCanStillOperatePlainUser(t *testing.T) {
	f := newAdminGuardFixture(t)

	if err := f.svc.Delete(testTenantID, adminGuardOperatorID, f.plainUser.ID); err != nil {
		t.Fatalf("操作无角色的普通账号不应被收敛拦截: %v", err)
	}
	if _, err := f.userRepo.FindByID(testTenantID, f.plainUser.ID); err == nil {
		t.Error("删除应已生效，实际仍能查到该用户")
	}
}

// TestNonAdminCanStillOperateNonAdminUser 覆盖「目标持有**非 admin** 角色」这条分支。
//
// 上一条用例的目标没有任何角色，会在 len(roleIDs)==0 处提前返回，
// 根本走不到「目标角色里有没有 admin」的判定 —— 只靠它，
// 把判定写成「一律要求超管操作者」也照样通过（变异验证实测）。
// 这里的目标持有 viewer（非 admin）角色，才能钉住「收敛只针对超管」。
func TestNonAdminCanStillOperateNonAdminUser(t *testing.T) {
	f := newAdminGuardFixture(t)

	if err := f.svc.Delete(testTenantID, adminGuardOperatorID, f.peerUser.ID); err != nil {
		t.Fatalf("操作持有非 admin 角色的账号不应被收敛拦截: %v", err)
	}
	if _, err := f.userRepo.FindByID(testTenantID, f.peerUser.ID); err == nil {
		t.Error("删除应已生效，实际仍能查到该用户")
	}
}

// TestUnknownOperatorCannotTouchAdmin 拿不到操作者身份时 fail-closed。
//
// operatorID 为 0 意味着无法判定调用方是谁（JWT 主体缺失 / 内部误调用）。
// 这是最高危的三条操作，宁可拒绝，也不能静默放行。
func TestUnknownOperatorCannotTouchAdmin(t *testing.T) {
	f := newAdminGuardFixture(t)

	assertBizError(t, f.svc.Delete(testTenantID, 0, f.adminUser.ID), common.CodeForbidden)
	assertBizError(t,
		f.svc.UpdateStatus(testTenantID, 0, &dto.StatusRequest{ID: f.adminUser.ID, Status: common.StatusDisabled}),
		common.CodeForbidden)

	if _, err := f.userRepo.FindByID(testTenantID, f.adminUser.ID); err != nil {
		t.Errorf("被拒绝的操作不应生效: %v", err)
	}
}
