package controller

import (
	"net/http"
	"strconv"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"

	"gorm.io/gorm"
)

// 用户 / 角色控制器的上下文透传测试（P2-1）。
//
// 为什么在 Controller 层测这些：Controller 是唯一一处「从 gin 上下文取
// tenantID / operatorID 再传给 Service」的地方，这一步没有类型层面的保护 ——
// 少传一个参数、传错顺序，编译照样通过，而后果分别是
// 「租户过滤被跳过」和「授权校验拿错操作者」。
//
// 现有覆盖：file / dept 两个控制器。这里补上改动最密集的 user / role：
// 它们承载 P0-1 的授权收敛校验，操作者身份传错会让校验失去意义。
//
// ⚠️ testsupport.NewDB 会改写包级 database.DB，因此不能 t.Parallel。

const ctrlOtherTenant uint = 2

// testDB 取当前测试库（testsupport.NewDB 已注入 database.DB）
func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	return database.DB
}

// newUserRoleControllerDB 建库。
// role_service 的 Create/Update 结尾会同步 Casbin 策略，故 casbin_rule 由
// InitCasbin 自行建表；这里只需业务表。
func newUserRoleControllerDB(t *testing.T) {
	t.Helper()
	testsupport.NewDB(t,
		&model.SysUser{}, &model.SysRole{}, &model.SysUserRole{}, &model.SysRoleMenu{},
		&model.SysMenu{}, &model.SysPost{}, &model.SysUserPost{},
	)
}

func seedCtrlUser(t *testing.T, tenantID uint, username string) *model.SysUser {
	t.Helper()
	u := &model.SysUser{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Username:        username,
		Password:        "x",
		Nickname:        username,
		Status:          1,
	}
	if err := testDB(t).Create(u).Error; err != nil {
		t.Fatalf("准备用户失败: %v", err)
	}
	return u
}

func seedCtrlRole(t *testing.T, tenantID uint, code string) *model.SysRole {
	t.Helper()
	r := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Name:            "角色-" + code,
		Code:            code,
		Status:          1,
	}
	if err := testDB(t).Create(r).Error; err != nil {
		t.Fatalf("准备角色失败: %v", err)
	}
	return r
}

func countUserRole(t *testing.T, userID uint) int64 {
	t.Helper()
	var n int64
	if err := testDB(t).Model(&model.SysUserRole{}).Where("user_id = ?", userID).Count(&n).Error; err != nil {
		t.Fatalf("统计角色关联失败: %v", err)
	}
	return n
}

func idStr(v uint) string { return strconv.FormatUint(uint64(v), 10) }

// TestUserControllerUpdateRolesThreadsContext 「给用户分配角色」必须用
// **当前登录者**的身份与租户去做校验。
//
// 若 Controller 传的是别的值（例如把 tenantID 传成 0），
// 租户校验会退化为「不过滤」、授权收敛会退化为「平台级放行」——
// 一条接口就把两个安全校验同时架空。
func TestUserControllerUpdateRolesThreadsContext(t *testing.T) {
	newUserRoleControllerDB(t)
	ctl := NewUserController()

	t.Run("本租户内分配成功", func(t *testing.T) {
		user := seedCtrlUser(t, ctrlTenant, "alice")
		role := seedCtrlRole(t, ctrlTenant, "editor")

		c, w := newCtx(http.MethodPut, "/api/v1/system/user/roles", ctrlTenant, 1)
		withJSONBody(c, `{"id":`+idStr(user.ID)+`,"roleIds":[`+idStr(role.ID)+`]}`)
		ctl.UpdateRoles(c)

		if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
			t.Fatalf("应分配成功，实际 %v（%v）", resp["code"], resp["message"])
		}
		if got := countUserRole(t, user.ID); got != 1 {
			t.Errorf("应写入 1 条角色关联，实际 %d", got)
		}
	})

	t.Run("跨租户用户返回 404", func(t *testing.T) {
		other := seedCtrlUser(t, ctrlOtherTenant, "bob")
		role := seedCtrlRole(t, ctrlTenant, "editor2")

		c, w := newCtx(http.MethodPut, "/api/v1/system/user/roles", ctrlTenant, 1)
		withJSONBody(c, `{"id":`+idStr(other.ID)+`,"roleIds":[`+idStr(role.ID)+`]}`)
		ctl.UpdateRoles(c)

		resp := decodeResp(t, w)
		if resp["code"].(float64) != float64(common.CodeNotFound) {
			t.Errorf("跨租户应返回 404，实际 %v（%v）", resp["code"], resp["message"])
		}
		if got := countUserRole(t, other.ID); got != 0 {
			t.Errorf("跨租户分配不应写入关联，实际 %d 条", got)
		}
	})

	t.Run("跨租户角色被拒", func(t *testing.T) {
		user := seedCtrlUser(t, ctrlTenant, "carol")
		foreignRole := seedCtrlRole(t, ctrlOtherTenant, "foreign")

		c, w := newCtx(http.MethodPut, "/api/v1/system/user/roles", ctrlTenant, 1)
		withJSONBody(c, `{"id":`+idStr(user.ID)+`,"roleIds":[`+idStr(foreignRole.ID)+`]}`)
		ctl.UpdateRoles(c)

		resp := decodeResp(t, w)
		if resp["code"].(float64) == 0 {
			t.Error("绑定其他租户的角色必须失败")
		}
		if got := countUserRole(t, user.ID); got != 0 {
			t.Errorf("被拒绝的分配不应写入关联，实际 %d 条", got)
		}
	})

	t.Run("请求体非法返回 400 而不是 500", func(t *testing.T) {
		c, w := newCtx(http.MethodPut, "/api/v1/system/user/roles", ctrlTenant, 1)
		withJSONBody(c, `{"id":"不是数字"}`)
		ctl.UpdateRoles(c)

		resp := decodeResp(t, w)
		if resp["code"].(float64) != float64(common.CodeBadRequest) {
			t.Errorf("参数绑定失败应为 400，实际 %v", resp["code"])
		}
	})
}

// TestRoleControllerCreateStampsContext 新建角色必须落在操作者所属租户下。
//
// 租户只能来自登录上下文（请求体里没有这个字段），传错就等于
// 「把角色建到别人租户里」或「建到平台级（tenant=0）」。
func TestRoleControllerCreateStampsContext(t *testing.T) {
	newUserRoleControllerDB(t)
	ctl := NewRoleController()

	c, w := newCtx(http.MethodPost, "/api/v1/system/role", ctrlOtherTenant, 7)
	withJSONBody(c, `{"name":"新角色","code":"newrole","status":1,"dataScope":1}`)
	ctl.Create(c)

	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("创建应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	var got model.SysRole
	if err := testDB(t).Where("code = ?", "newrole").First(&got).Error; err != nil {
		t.Fatalf("回查角色失败: %v", err)
	}
	if got.TenantID != ctrlOtherTenant {
		t.Errorf("角色应归属租户 %d，实际 %d", ctrlOtherTenant, got.TenantID)
	}
	if got.CreateBy != 7 {
		t.Errorf("创建者应记录为操作者 7，实际 %d", got.CreateBy)
	}
}

// TestRoleControllerUpdateNotFound 改不存在的角色返回 404 而不是 500。
func TestRoleControllerUpdateNotFound(t *testing.T) {
	newUserRoleControllerDB(t)
	ctl := NewRoleController()

	c, w := newCtx(http.MethodPut, "/api/v1/system/role", ctrlTenant, 1)
	withJSONBody(c, `{"id":99999,"name":"不存在"}`)
	ctl.Update(c)

	resp := decodeResp(t, w)
	if resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}
}

// TestRoleControllerDeleteCrossTenant 跨租户删除角色应失败且不落库。
func TestRoleControllerDeleteCrossTenant(t *testing.T) {
	newUserRoleControllerDB(t)
	ctl := NewRoleController()
	foreign := seedCtrlRole(t, ctrlOtherTenant, "foreign-del")

	c, w := newCtx(http.MethodDelete, "/api/v1/system/role/1", ctrlTenant, 1)
	withIDParam(c, foreign.ID)
	ctl.Delete(c)

	resp := decodeResp(t, w)
	if resp["code"].(float64) == 0 {
		t.Error("跨租户删除必须失败")
	}
	var still model.SysRole
	if err := testDB(t).Where("id = ?", foreign.ID).First(&still).Error; err != nil {
		t.Errorf("角色不应被删除: %v", err)
	}
}
