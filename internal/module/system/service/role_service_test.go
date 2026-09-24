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

// 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。

// roleTenantB 「另一个租户」的 ID。
const roleTenantB uint = 2

// newTestRoleService 构造带内存库的角色服务。
//
// 建 casbin_rule 表是为了让 Update 里的 syncPolicies 走通：
// Update 结尾一定会调 SyncPoliciesFromRoleMenus，表不存在会让它返回错误。
// 虽然该失败只记日志、不影响用例断言，但没必要让测试输出里充满无关报错。
//
// menuRepo 必须一并注入：授权收敛校验会用它查「这些菜单声明了哪些权限码」，
// 漏了会在勾选菜单时直接空指针。
func newTestRoleService(t *testing.T) *roleService {
	t.Helper()
	testsupport.NewDB(t, &model.SysRole{}, &model.SysRoleMenu{}, &model.SysMenu{},
		&middleware.CasbinRule{})
	return &roleService{
		roleRepo: repository.NewRoleRepository(),
		menuRepo: repository.NewMenuRepository(),
	}
}

func seedRole(t *testing.T, tenantID uint, code string, status int8) *model.SysRole {
	t.Helper()
	role := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Name:            "角色_" + code,
		Code:            code,
		Sort:            5,
		Status:          status,
		DataScope:       1,
	}
	role.Remark = "初始备注"
	if err := repository.NewRoleRepository().Create(role); err != nil {
		t.Fatalf("创建角色失败: %v", err)
	}
	return role
}

func int8p(v int8) *int8 { return &v }
func intptr(v int) *int  { return &v }

// TestRoleUpdatePartialKeepsFields 「只保存权限」的部分更新回归测试。
//
// 事故背景：前端「分配权限」只提交 {id, menuIds}，旧实现的无条件赋值会把
// 未提供的字段清成零值（当时被 binding 校验挡住变成了恒 400，功能整体不可用）。
// 修法是指针 + 逐字段判断。本测试锁定的正是「未提供的字段绝不能动」——
// 如果被改回无条件赋值，sort/status/dataScope/remark 会被清零，用例会红。
func TestRoleUpdatePartialKeepsFields(t *testing.T) {
	s := newTestRoleService(t)
	role := seedRole(t, testTenantID, "editor", 1)

	// 模拟前端「分配权限」：只带 id 和 menuIds
	err := s.Update(&dto.UpdateRoleRequest{ID: role.ID, MenuIds: []uint{7, 8}}, 1, testTenantID)
	if err != nil {
		t.Fatalf("只提交权限应更新成功: %v", err)
	}

	got, err := s.roleRepo.FindByID(testTenantID, role.ID)
	if err != nil {
		t.Fatalf("回读角色失败: %v", err)
	}
	if got.Status != 1 {
		t.Errorf("status 被部分更新清掉了：期望 1，实际 %d", got.Status)
	}
	if got.Sort != 5 {
		t.Errorf("sort 被部分更新清掉了：期望 5，实际 %d", got.Sort)
	}
	if got.DataScope != 1 {
		t.Errorf("dataScope 被部分更新清掉了：期望 1，实际 %d", got.DataScope)
	}
	if got.Remark != "初始备注" {
		t.Errorf("remark 被部分更新清掉了: %q", got.Remark)
	}
	if got.Name != role.Name || got.Code != role.Code {
		t.Errorf("名称/编码不应变化: %q/%q", got.Name, got.Code)
	}

	menuIDs, err := s.roleRepo.FindMenuIDsByRoleID(testTenantID, role.ID)
	if err != nil {
		t.Fatalf("查询角色菜单失败: %v", err)
	}
	if len(menuIDs) != 2 {
		t.Errorf("应绑定 2 个菜单，实际 %v", menuIDs)
	}
}

// TestRoleUpdateExplicitZeroStatus 「显式设为 0」必须能生效。
//
// 这是指针方案的另一半：放宽校验（去掉对零值生效的 oneof）之后，
// 用户仍然要能把状态改成「停用」。如果实现把「零值」当成「未提供」跳过，
// 停用角色就会静默失败。
func TestRoleUpdateExplicitZeroStatus(t *testing.T) {
	s := newTestRoleService(t)
	role := seedRole(t, testTenantID, "disableme", 1)

	err := s.Update(&dto.UpdateRoleRequest{
		ID:     role.ID,
		Status: int8p(0),
		Sort:   intptr(0),
	}, 1, testTenantID)
	if err != nil {
		t.Fatalf("完整编辑应更新成功: %v", err)
	}

	got, _ := s.roleRepo.FindByID(testTenantID, role.ID)
	if got.Status != 0 {
		t.Errorf("显式 status=0 未生效，实际 %d", got.Status)
	}
	if got.Sort != 0 {
		t.Errorf("显式 sort=0 未生效，实际 %d", got.Sort)
	}
}

// TestRoleUpdateEmptyMenuIdsClears 空切片与 nil 的语义必须不同：
// MenuIds: [] 表示「清空权限」，nil（字段缺省）才是「本次不涉及」。
func TestRoleUpdateEmptyMenuIdsClears(t *testing.T) {
	s := newTestRoleService(t)
	role := seedRole(t, testTenantID, "clearme", 1)
	if err := s.roleRepo.ReplaceMenus(testTenantID, role.ID, []uint{1, 2}); err != nil {
		t.Fatalf("准备菜单失败: %v", err)
	}

	if err := s.Update(&dto.UpdateRoleRequest{ID: role.ID, MenuIds: []uint{}}, 1, testTenantID); err != nil {
		t.Fatalf("清空权限应成功: %v", err)
	}

	menuIDs, _ := s.roleRepo.FindMenuIDsByRoleID(testTenantID, role.ID)
	if len(menuIDs) != 0 {
		t.Errorf("传空切片应清空菜单，实际剩余 %v", menuIDs)
	}
}

// TestRoleUpdateCrossTenantRejected 其他租户的角色不可改。
//
// 仓储层 FindByID/Update 都带 TenantScope，这里验证的是完整链路上
// 「拿 B 租户的 ID 用 A 租户身份改」会以 404 收场，而不是改成功。
func TestRoleUpdateCrossTenantRejected(t *testing.T) {
	s := newTestRoleService(t)
	other := seedRole(t, roleTenantB, "other-tenant", 1)

	err := s.Update(&dto.UpdateRoleRequest{ID: other.ID, Name: "劫持"}, 1, testTenantID)
	if err == nil {
		t.Fatal("修改其他租户的角色应失败")
	}

	// 原数据不能被改动
	got, _ := s.roleRepo.FindByID(roleTenantB, other.ID)
	if got.Name == "劫持" {
		t.Error("跨租户更新竟然生效了")
	}
}

// TestRoleUpdateDuplicateCodeOtherTenant 角色 code 是全局唯一（Casbin 主体是
// code，跨租户重名会造成策略合并）。改 code 撞名必须返回业务错误而不是 500。
func TestRoleUpdateDuplicateCodeOtherTenant(t *testing.T) {
	s := newTestRoleService(t)
	seedRole(t, testTenantID, "taken", 1)
	role := seedRole(t, testTenantID, "free", 1)

	err := s.Update(&dto.UpdateRoleRequest{ID: role.ID, Code: "taken"}, 1, testTenantID)
	if err == nil {
		t.Fatal("改成已存在的编码应报错")
	}
	assertBizError(t, err, common.CodeBadRequest)
}
