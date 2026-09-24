package repository

import (
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"
)

// 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。
//
// 仪表盘统计的关键在于「哪些表按租户过滤、哪些是全局表」必须逐张对齐，
// 而这类错误不会报错：数字只是偏大，看起来像「数据比较多」而已。

const (
	dashTenantA uint = 1
	dashTenantB uint = 2
)

func newDashboardRepoWithDB(t *testing.T) DashboardRepository {
	t.Helper()
	// 先建库（注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	testsupport.NewDB(t,
		&model.SysUser{}, &model.SysRole{}, &model.SysMenu{}, &model.SysDept{},
		&model.SysPost{}, &model.SysConfig{}, &model.SysOperationLog{},
	)
	return NewDashboardRepository()
}

// seedDashboardData 造一份「两个租户各有数据、全局表也有数据」的样本。
//
// 返回各表的期望计数，避免测试里再数一遍（数错就没意义了）。
func seedDashboardData(t *testing.T) {
	t.Helper()

	// 租户内表：用户名/角色编码/岗位编码的唯一索引是**全局**的，两租户要用不同值
	users := []model.SysUser{
		{TenantBaseModel: common.TenantBaseModel{TenantID: dashTenantA}, Username: "dash-a1"},
		{TenantBaseModel: common.TenantBaseModel{TenantID: dashTenantA}, Username: "dash-a2"},
		{TenantBaseModel: common.TenantBaseModel{TenantID: dashTenantB}, Username: "dash-b1"},
	}
	roles := []model.SysRole{
		{TenantBaseModel: common.TenantBaseModel{TenantID: dashTenantA}, Name: "A角色", Code: "dash_role_a"},
		{TenantBaseModel: common.TenantBaseModel{TenantID: dashTenantA}, Name: "A角色2", Code: "dash_role_a2"},
		{TenantBaseModel: common.TenantBaseModel{TenantID: dashTenantB}, Name: "B角色", Code: "dash_role_b"},
	}
	depts := []model.SysDept{
		{TenantBaseModel: common.TenantBaseModel{TenantID: dashTenantA}, Name: "A部门1"},
		{TenantBaseModel: common.TenantBaseModel{TenantID: dashTenantA}, Name: "A部门2"},
		{TenantBaseModel: common.TenantBaseModel{TenantID: dashTenantB}, Name: "B部门"},
	}
	posts := []model.SysPost{
		{TenantBaseModel: common.TenantBaseModel{TenantID: dashTenantA}, Name: "A岗位", Code: "dash_post_a"},
		{TenantBaseModel: common.TenantBaseModel{TenantID: dashTenantB}, Name: "B岗位1", Code: "dash_post_b1"},
		{TenantBaseModel: common.TenantBaseModel{TenantID: dashTenantB}, Name: "B岗位2", Code: "dash_post_b2"},
	}
	logs := []model.SysOperationLog{
		{TenantID: dashTenantA, Title: "A日志1"},
		{TenantID: dashTenantA, Title: "A日志2"},
		{TenantID: dashTenantA, Title: "A日志3"},
		{TenantID: dashTenantB, Title: "B日志1"},
		{TenantID: dashTenantB, Title: "B日志2"},
	}

	// 全局表：菜单与配置（无 tenant_id，所有租户共用一套）
	menus := []model.SysMenu{
		{Name: "菜单1"}, {Name: "菜单2"}, {Name: "菜单3"}, {Name: "菜单4"},
	}
	configs := []model.SysConfig{
		{Name: "配置1", ConfigKey: "dash_cfg_1"},
		{Name: "配置2", ConfigKey: "dash_cfg_2"},
	}

	for _, group := range []struct {
		rows any
		name string
	}{
		{users, "用户"}, {roles, "角色"}, {depts, "部门"},
		{posts, "岗位"}, {logs, "操作日志"}, {menus, "菜单"}, {configs, "配置"},
	} {
		if err := database.DB.Create(group.rows).Error; err != nil {
			t.Fatalf("准备%s数据失败: %v", group.name, err)
		}
	}
}

// TestDashboardGetStatsRespectsTenantScope 逐项校验统计口径。
//
// 这张表是「哪些表按租户过滤」的唯一权威说明，改动前请先看清每一项：
//
//	按租户过滤：sys_user / sys_role / sys_post / sys_operation_log / sys_dept
//	全局表：    sys_menu / sys_config
//
// 之所以把 sys_dept 也列进「按租户过滤」：sys_dept 的模型继承
// TenantBaseModel、表里有 tenant_id，deptRepository 的每个方法也都施加了
// TenantScope（部门是租户内数据，见 2026-09-25-dept-tenant.sql）。
// 把它当成全局表统计，会让每个租户的仪表盘都显示**全平台**的部门数量 ——
// 既不准确，也顺带泄漏了平台规模。
func TestDashboardGetStatsRespectsTenantScope(t *testing.T) {
	repo := newDashboardRepoWithDB(t)
	seedDashboardData(t)

	statsA, err := repo.GetStats(dashTenantA)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	statsB, err := repo.GetStats(dashTenantB)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}

	// 租户内表：每个租户只应看到自己的数量
	perTenant := []struct {
		name     string
		gotA     int64
		wantA    int64
		gotB     int64
		wantB    int64
		scopeDoc string
	}{
		{"用户数", statsA.UserCount, 2, statsB.UserCount, 1, "sys_user 是租户内表"},
		{"角色数", statsA.RoleCount, 2, statsB.RoleCount, 1, "sys_role 是租户内表"},
		{"部门数", statsA.DeptCount, 2, statsB.DeptCount, 1, "sys_dept 是租户内表（表内有 tenant_id）"},
		{"岗位数", statsA.PostCount, 1, statsB.PostCount, 2, "sys_post 是租户内表"},
		{"操作日志数", statsA.LogCount, 3, statsB.LogCount, 2, "sys_operation_log 是租户内表"},
	}
	for _, tc := range perTenant {
		if tc.gotA != tc.wantA {
			t.Errorf("租户A 的%s 应为 %d，实际 %d（%s）", tc.name, tc.wantA, tc.gotA, tc.scopeDoc)
		}
		if tc.gotB != tc.wantB {
			t.Errorf("租户B 的%s 应为 %d，实际 %d（%s）", tc.name, tc.wantB, tc.gotB, tc.scopeDoc)
		}
	}

	// 全局表：两个租户看到的必须是同一个数（且等于全表条数）
	global := []struct {
		name  string
		gotA  int64
		gotB  int64
		want  int64
		scope string
	}{
		{"菜单数", statsA.MenuCount, statsB.MenuCount, 4, "sys_menu 是全局表（无 tenant_id）"},
		{"配置数", statsA.ConfigCount, statsB.ConfigCount, 2, "sys_config 是全局表（平台级凭据）"},
	}
	for _, tc := range global {
		if tc.gotA != tc.want || tc.gotB != tc.want {
			t.Errorf("%s 应为 %d（%s），实际 A=%d B=%d", tc.name, tc.want, tc.scope, tc.gotA, tc.gotB)
		}
	}
}

// TestDashboardGetStatsEmptyTenant 新租户（还没有任何数据）应全为 0 而不是报错。
//
// 仪表盘是登录后的第一个页面，若新租户打开就 500，整个系统看起来是坏的。
func TestDashboardGetStatsEmptyTenant(t *testing.T) {
	repo := newDashboardRepoWithDB(t)

	stats, err := repo.GetStats(999)
	if err != nil {
		t.Fatalf("空租户统计不应报错: %v", err)
	}
	if stats == nil {
		t.Fatal("统计结果不应为 nil")
	}

	if stats.UserCount != 0 || stats.RoleCount != 0 || stats.PostCount != 0 || stats.LogCount != 0 {
		t.Errorf("空租户的租户内表计数应全为 0，实际 %+v", stats)
	}
}
