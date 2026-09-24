package controller

import (
	"net/http"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"
	"go-admin/pkg/utils"

	"github.com/gin-gonic/gin"
)

// 用户 / 认证控制器的端到端测试。
//
// 这两个控制器此前 15 个方法**全是 0%**：登录链路是改造过最多的代码
// （限频、日志、token 吊销都从 Controller 下沉到了 Service），
// 而用户管理是权限落点最密集的模块。它们都不含业务逻辑了，
// 但「从上下文取 tenantID / operatorID 再传下去」这一步没有类型保护，
// 传错就分别退化成「租户过滤被跳过」和「越权判定拿错人」。
//
// ⚠️ testsupport.NewDB 会改写包级 database.DB，因此不能 t.Parallel。

// newFullSystemDB 建一套完整业务表。
// 之所以一次性全建：Controller 之间会互相构造 Service
// （如 UserController → NewDeptService/NewPostService），
// 缺一张表就会在某个看起来无关的接口上炸出 SQL 错误。
func newFullSystemDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	testsupport.NewDB(t,
		&model.SysConfig{}, &model.SysDictType{}, &model.SysDictData{},
		&model.SysPost{}, &model.SysUser{}, &model.SysUserRole{}, &model.SysUserPost{},
		&model.SysRole{}, &model.SysRoleMenu{}, &model.SysMenu{}, &model.SysDept{},
		&model.SysAgreement{}, &model.SysOperationLog{}, &model.SysLoginLog{},
		&model.SysFile{},
	)
}

// seedCtrlDeptRow 直接落一条部门（用户接口的 deptId 需要一个真实存在的部门）。
func seedCtrlDeptRow(t *testing.T, tenantID uint, name string) *model.SysDept {
	t.Helper()
	d := &model.SysDept{TenantBaseModel: common.TenantBaseModel{TenantID: tenantID}, Name: name, Status: 1}
	if err := testDB(t).Create(d).Error; err != nil {
		t.Fatalf("准备部门失败: %v", err)
	}
	return d
}

// seedCtrlUserWithPassword 落一个密码已知的用户（改密用例需要能校验旧密码）。
func seedCtrlUserWithPassword(t *testing.T, tenantID uint, username, password string) *model.SysUser {
	t.Helper()
	hash, err := utils.HashPassword(password)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	u := &model.SysUser{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Username:        username,
		Password:        hash,
		Nickname:        username,
		Status:          1,
	}
	if err := testDB(t).Create(u).Error; err != nil {
		t.Fatalf("准备用户失败: %v", err)
	}
	return u
}

func TestUserControllerLifecycle(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewUserController()
	dept := seedCtrlDeptRow(t, ctrlTenant, "研发部")

	// 参数非法 → 400（缺 deptId / password）
	c, w := newCtx(http.MethodPost, "/api/v1/system/user", ctrlTenant, 9)
	withJSONBody(c, `{"username":"x"}`)
	ctl.Create(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("缺字段应返回 400，实际 %v", resp["code"])
	}

	c, w = newCtx(http.MethodPost, "/api/v1/system/user", ctrlTenant, 9)
	withJSONBody(c, `{"username":"alice","password":"Abc12345","nickname":"爱丽丝","deptId":`+idStr(dept.ID)+`,"status":1}`)
	ctl.Create(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("创建应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	var u model.SysUser
	if err := testDB(t).Where("username = ?", "alice").First(&u).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if u.TenantID != ctrlTenant || u.CreateBy != 9 {
		t.Errorf("租户/创建者透传错误: %+v", u)
	}
	if u.Password == "Abc12345" {
		t.Error("密码不能明文入库")
	}

	// 详情
	c, w = newCtx(http.MethodGet, "/api/v1/system/user/1", ctrlTenant, 9)
	withIDParam(c, u.ID)
	ctl.FindByID(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("详情应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 详情：非法 ID → 400
	c, w = newCtx(http.MethodGet, "/api/v1/system/user/x", ctrlTenant, 9)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.FindByID(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}

	// 详情：跨租户 → 404（不是 500，也不能返回数据）
	c, w = newCtx(http.MethodGet, "/api/v1/system/user/1", ctrlOtherTenant, 9)
	withIDParam(c, u.ID)
	ctl.FindByID(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("跨租户应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 列表：按租户隔离
	c, w = newCtx(http.MethodGet, "/api/v1/system/user/list?page=1&pageSize=10", ctrlTenant, 9)
	ctl.FindList(c)
	resp := decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("列表应成功，实际 %v", resp["code"])
	}
	if data := resp["data"].(map[string]interface{}); data["total"].(float64) != 1 {
		t.Errorf("应只看到本租户 1 个用户，实际 %v", data["total"])
	}

	// 更新
	c, w = newCtx(http.MethodPut, "/api/v1/system/user", ctrlTenant, 9)
	withJSONBody(c, `{"id":`+idStr(u.ID)+`,"nickname":"新昵称"}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("更新应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	var fresh model.SysUser
	if err := testDB(t).First(&fresh, u.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if fresh.Nickname != "新昵称" || fresh.UpdateBy != 9 {
		t.Errorf("更新未生效: %+v", fresh)
	}

	// 更新不存在的用户 → 404
	c, w = newCtx(http.MethodPut, "/api/v1/system/user", ctrlTenant, 9)
	withJSONBody(c, `{"id":99999,"nickname":"x"}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 改状态
	c, w = newCtx(http.MethodPut, "/api/v1/system/user/status", ctrlTenant, 9)
	withJSONBody(c, `{"id":`+idStr(u.ID)+`,"status":0}`)
	ctl.UpdateStatus(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("改状态应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	if err := testDB(t).First(&fresh, u.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if fresh.Status != 0 {
		t.Errorf("状态应为 0，实际 %d", fresh.Status)
	}

	// 改部门
	c, w = newCtx(http.MethodPut, "/api/v1/system/user/dept", ctrlTenant, 9)
	withJSONBody(c, `{"id":`+idStr(u.ID)+`,"deptId":`+idStr(dept.ID)+`}`)
	ctl.UpdateDept(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("改部门应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 导出：走文件下载而非统一 JSON Response，因此断言响应头与正文
	c, w = newCtx(http.MethodGet, "/api/v1/system/user/export?page=1&pageSize=10", ctrlTenant, 9)
	ctl.Export(c)
	if w.Code != http.StatusOK {
		t.Fatalf("导出应返回 200，实际 %d", w.Code)
	}
	if w.Header().Get("Content-Disposition") == "" {
		t.Error("导出应设置下载响应头")
	}
	if rows := w.Header().Get("X-Export-Rows"); rows != "1" {
		t.Errorf("应导出 1 行，实际 %q", rows)
	}
	if w.Body.Len() == 0 {
		t.Error("导出正文不应为空")
	}

	// 删除
	c, w = newCtx(http.MethodDelete, "/api/v1/system/user/1", ctrlTenant, 9)
	withIDParam(c, u.ID)
	ctl.Delete(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("删除应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 删除：非法 ID → 400
	c, w = newCtx(http.MethodDelete, "/api/v1/system/user/x", ctrlTenant, 9)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.Delete(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}
}

// TestUserControllerPasswordOperations 重置密码 / 自助改密两条路径。
//
// 「重置他人密码」在 Controller 层有一道前置检查（只有自己才能重置自己），
// 它是 403 而不是业务码 —— 属于权限拒绝，用真实 HTTP 状态。
func TestUserControllerPasswordOperations(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewUserController()

	u := seedCtrlUserWithPassword(t, ctrlTenant, "alice", "Abc12345")

	// 重置他人的密码 → 403，且不能有任何副作用
	c, w := newCtx(http.MethodPut, "/api/v1/system/user/resetPwd", ctrlTenant, 99)
	withJSONBody(c, `{"id":`+idStr(u.ID)+`,"password":"Zxc98765"}`)
	ctl.ResetPassword(c)
	if w.Code != http.StatusForbidden {
		t.Errorf("重置他人密码应返回 403，实际 %d（%s）", w.Code, w.Body.String())
	}
	var fresh model.SysUser
	if err := testDB(t).First(&fresh, u.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if !utils.CheckPassword("Abc12345", fresh.Password) {
		t.Error("越权请求不应改动密码")
	}

	// 弱密码 → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/user/resetPwd", ctrlTenant, u.ID)
	withJSONBody(c, `{"id":`+idStr(u.ID)+`,"password":"123"}`)
	ctl.ResetPassword(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("弱密码应返回 400，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 重置自己的密码 → 成功
	c, w = newCtx(http.MethodPut, "/api/v1/system/user/resetPwd", ctrlTenant, u.ID)
	withJSONBody(c, `{"id":`+idStr(u.ID)+`,"password":"Zxc98765"}`)
	ctl.ResetPassword(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("重置自己的密码应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	if err := testDB(t).First(&fresh, u.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if !utils.CheckPassword("Zxc98765", fresh.Password) {
		t.Error("新密码应生效")
	}

	// 自助改密：旧密码错误 → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/user/changePwd", ctrlTenant, u.ID)
	withJSONBody(c, `{"oldPassword":"WrongPass1","newPassword":"Qwe12345"}`)
	ctl.ChangePassword(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("旧密码错误应返回 400，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 自助改密：成功
	c, w = newCtx(http.MethodPut, "/api/v1/system/user/changePwd", ctrlTenant, u.ID)
	withJSONBody(c, `{"oldPassword":"Zxc98765","newPassword":"Qwe12345"}`)
	ctl.ChangePassword(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("改密应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	if err := testDB(t).First(&fresh, u.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if !utils.CheckPassword("Qwe12345", fresh.Password) {
		t.Error("新密码应生效")
	}
}

// ---- 认证 ----

func TestAuthControllerLoginAndTokenEndpoints(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewAuthController()

	// 参数缺失 → 400
	c, w := newCtx(http.MethodPost, "/api/v1/auth/login", 0, 0)
	withJSONBody(c, `{"username":"alice"}`)
	ctl.Login(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("缺字段应返回 400，实际 %v", resp["code"])
	}

	// 人机校验过不去：必须是可理解的业务错误，而不是 500
	c, w = newCtx(http.MethodPost, "/api/v1/auth/login", 0, 0)
	withJSONBody(c, `{"username":"alice","password":"Abc12345","captchaToken":"forged"}`)
	ctl.Login(c)
	resp := decodeResp(t, w)
	if resp["code"].(float64) == 0 {
		t.Error("伪造的验证码凭证必须被拒绝")
	}
	if resp["code"].(float64) == float64(common.CodeInternalError) {
		t.Errorf("应是业务错误而不是 500：%v", resp["message"])
	}

	// 刷新：缺参数 → 400
	c, w = newCtx(http.MethodPost, "/api/v1/auth/refresh", 0, 0)
	withJSONBody(c, `{}`)
	ctl.RefreshToken(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("缺 refreshToken 应返回 400，实际 %v", resp["code"])
	}

	// 刷新：非法 token → 401 业务码（前端据此清会话跳登录页）
	c, w = newCtx(http.MethodPost, "/api/v1/auth/refresh", 0, 0)
	withJSONBody(c, `{"refreshToken":"not-a-token"}`)
	ctl.RefreshToken(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeUnauthorized) {
		t.Errorf("非法 refresh token 应返回 401，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 登出：旧客户端既不带 body 也不带 Authorization → 无副作用，成功返回
	c, w = newCtx(http.MethodPost, "/api/v1/auth/logout", ctrlTenant, 1)
	ctl.Logout(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Errorf("空凭据登出应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
}

func TestAuthControllerGetUserInfo(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewAuthController()

	// 未登录 → 401（真实 HTTP 状态）
	c, w := newCtx(http.MethodGet, "/api/v1/auth/userInfo", 0, 0)
	ctl.GetUserInfo(c)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("未登录应返回 401，实际 %d", w.Code)
	}

	u := seedCtrlUser(t, ctrlTenant, "alice")
	c, w = newCtx(http.MethodGet, "/api/v1/auth/userInfo", ctrlTenant, u.ID)
	ctl.GetUserInfo(c)
	resp := decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("取用户信息应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data 应为对象，实际 %T", resp["data"])
	}
	if data["username"] != "alice" {
		t.Errorf("应返回当前登录用户的信息，实际 %v", data["username"])
	}
}
