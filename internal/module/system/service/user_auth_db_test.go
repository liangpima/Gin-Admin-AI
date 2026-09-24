package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go-admin/config"
	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/pkg/auth"
	"go-admin/pkg/utils"
)

// 用户 / 认证 / 登录守卫的 service 层补测。
//
// 这一批此前几乎全是 0%：老用例只覆盖了「授权收敛」这条安全主线
// （role_grant_test.go / user_service_test.go），而 CRUD、列表装配、
// 登录链路、日志落库这些「日常路径」一行都没跑过 —— 恰恰是它们
// 承载着租户隔离与 404 语义。
//
// ⚠️ testsupport.NewDB 会改写包级 database.DB，因此不能 t.Parallel。

// listReq 构造用户列表请求。分页字段来自内嵌的 PageQuery，复合字面量里写不了。
func listReq(page, pageSize int) *dto.UserListRequest {
	req := &dto.UserListRequest{}
	req.Page, req.PageSize = page, pageSize
	return req
}

func TestUserServiceListAssemblesRoles(t *testing.T) {
	db := newServiceDB(t)
	svc := NewUserService()

	hash, err := utils.HashPassword("Abc12345")
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}

	u := &model.SysUser{
		TenantBaseModel: common.TenantBaseModel{TenantID: 1},
		Username:        "alice", Password: hash, Nickname: "爱丽丝", Status: 1,
	}
	foreign := &model.SysUser{
		TenantBaseModel: common.TenantBaseModel{TenantID: 2},
		Username:        "bob", Password: hash, Nickname: "鲍勃", Status: 1,
	}
	role := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: 1},
		Name:            "编辑", Code: "editor", Status: 1,
	}
	for _, row := range []any{u, foreign, role} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("准备数据失败: %v", err)
		}
	}
	if err := db.Create(&model.SysUserRole{UserID: u.ID, RoleID: role.ID}).Error; err != nil {
		t.Fatalf("准备角色关联失败: %v", err)
	}

	// 列表：批量装配角色（把 2N+1 次查询压成固定 3 次的那条路径）
	list, total, err := svc.FindList(1, listReq(1, 10))
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("应只看到本租户 1 个用户，实际 total=%d len=%d", total, len(list))
	}
	row, ok := list[0].(UserWithRoles)
	if !ok {
		t.Fatalf("列表元素应为 UserWithRoles，实际 %T", list[0])
	}
	if row.Username != "alice" {
		t.Errorf("列表内容不符: %+v", row.SysUser)
	}
	if len(row.Roles) != 1 || row.Roles[0].Code != "editor" {
		t.Errorf("应装配出 1 个角色，实际 %+v", row.Roles)
	}

	// 空结果必须返回非 nil 切片：前端表格直接吃这个字段
	empty, emptyTotal, err := svc.FindList(1, &dto.UserListRequest{Username: "nobody"})
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if empty == nil || len(empty) != 0 || emptyTotal != 0 {
		t.Errorf("空结果应为非 nil 空切片，实际 %v（total=%d）", empty, emptyTotal)
	}

	// 导出：同样的筛选条件，但不做分页截断
	rows, err := svc.ExportList(1, listReq(1, 1))
	if err != nil {
		t.Fatalf("导出查询失败: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("导出应返回 1 行，实际 %d", len(rows))
	}

	// 详情：带角色信息
	raw, err := svc.FindByID(1, u.ID)
	if err != nil {
		t.Fatalf("详情查询失败: %v", err)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !strings.Contains(string(b), "editor") {
		t.Errorf("详情应带角色编码，实际 %s", b)
	}
	// 跨租户查不到 → 404，不是 500
	_, err = svc.FindByID(2, u.ID)
	assertBizError(t, err, common.CodeNotFound)
}

func TestUserServiceStatusResetAndDelete(t *testing.T) {
	db := newServiceDB(t)
	svc := NewUserService()

	hash, err := utils.HashPassword("Abc12345")
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	u := &model.SysUser{
		TenantBaseModel: common.TenantBaseModel{TenantID: 1},
		Username:        "alice", Password: hash, Status: 1,
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("准备用户失败: %v", err)
	}

	// 禁用：同时会吊销该用户 token（Redis 不可用时只记日志，不能影响主流程）
	if err := svc.UpdateStatus(1, &dto.StatusRequest{ID: u.ID, Status: 0}); err != nil {
		t.Fatalf("改状态失败: %v", err)
	}
	var fresh model.SysUser
	if err := db.First(&fresh, u.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if fresh.Status != 0 {
		t.Errorf("状态应被更新为 0，实际 %d", fresh.Status)
	}

	// 重置密码：弱密码必须在落库前被拒
	assertBizError(t, svc.ResetPassword(1, &dto.ResetPasswordRequest{ID: u.ID, Password: "123"}),
		common.CodeBadRequest)
	if err := db.First(&fresh, u.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if !utils.CheckPassword("Abc12345", fresh.Password) {
		t.Error("弱密码被拒时不应改动原密码")
	}

	if err := svc.ResetPassword(1, &dto.ResetPasswordRequest{ID: u.ID, Password: "Zxc98765"}); err != nil {
		t.Fatalf("重置密码失败: %v", err)
	}
	if err := db.First(&fresh, u.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if fresh.Password == "Zxc98765" {
		t.Error("密码绝不能明文入库")
	}
	if !utils.CheckPassword("Zxc98765", fresh.Password) {
		t.Error("新密码的哈希应可校验通过")
	}

	if err := svc.Delete(1, u.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := svc.FindByID(1, u.ID); err == nil {
		t.Error("删除后应查不到该用户")
	}
}

// TestAuthServiceGetUserInfoBuildsMenus 用户信息接口要把「角色 + 按钮权限 + 菜单树」装配齐。
//
// 前端靠它渲染侧边栏与按钮级权限；菜单树少一层，用户就会看到空白的导航。
func TestAuthServiceGetUserInfoBuildsMenus(t *testing.T) {
	db := newServiceDB(t)
	svc := NewAuthService()

	u := &model.SysUser{
		TenantBaseModel: common.TenantBaseModel{TenantID: 1},
		Username:        "alice", Nickname: "爱丽丝", Status: 1,
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("准备用户失败: %v", err)
	}

	// 必须逐个建：子菜单的 parent_id 依赖父菜单落库后才有真实 ID，
	// 一次性构造会让 parent_id 全是 0，整棵树被拍平成并列的根节点
	dir := &model.SysMenu{Name: "system", Title: "系统管理", Type: 0, Sort: 1, Status: 1}
	if err := db.Create(dir).Error; err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	page := &model.SysMenu{ParentID: dir.ID, Name: "user", Title: "用户管理", Type: 1, Status: 1}
	if err := db.Create(page).Error; err != nil {
		t.Fatalf("准备菜单失败: %v", err)
	}
	btn := &model.SysMenu{ParentID: page.ID, Name: "user:add", Type: 2, Permission: "system:user:add", Status: 1}
	if err := db.Create(btn).Error; err != nil {
		t.Fatalf("准备按钮失败: %v", err)
	}
	// 停用菜单不应出现在导航里
	off := &model.SysMenu{Name: "hidden", Title: "停用菜单", Type: 1, Status: 0}
	role := &model.SysRole{TenantBaseModel: common.TenantBaseModel{TenantID: 1}, Name: "管理员", Code: "mgr", Status: 1}
	for _, row := range []any{off, role} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("准备数据失败: %v", err)
		}
	}
	links := []any{
		&model.SysUserRole{UserID: u.ID, RoleID: role.ID},
		&model.SysRoleMenu{RoleID: role.ID, MenuID: dir.ID},
		&model.SysRoleMenu{RoleID: role.ID, MenuID: page.ID},
		&model.SysRoleMenu{RoleID: role.ID, MenuID: btn.ID},
		&model.SysRoleMenu{RoleID: role.ID, MenuID: off.ID},
	}
	for _, row := range links {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("准备关联失败: %v", err)
		}
	}

	info, err := svc.GetUserInfo(u.ID)
	if err != nil {
		t.Fatalf("取用户信息失败: %v", err)
	}
	if info.Username != "alice" || info.Nickname != "爱丽丝" {
		t.Errorf("基础字段不符: %+v", info)
	}
	if len(info.Roles) != 1 || info.Roles[0].Code != "mgr" {
		t.Errorf("应带 1 个角色，实际 %+v", info.Roles)
	}
	if len(info.Buttons) != 1 || info.Buttons[0] != "system:user:add" {
		t.Errorf("按钮权限应只含按钮型菜单的 permission，实际 %v", info.Buttons)
	}
	if len(info.Menus) != 1 {
		t.Fatalf("应只有 1 个根菜单（停用项被过滤），实际 %d", len(info.Menus))
	}
	if len(info.Menus[0].Children) != 1 {
		t.Fatalf("根菜单应有 1 个子节点，实际 %d", len(info.Menus[0].Children))
	}
	if got := info.Menus[0].Children[0].Children; len(got) != 1 || got[0].Name != "user:add" {
		t.Errorf("三级菜单树装配不符: %+v", got)
	}

	if _, err := svc.GetUserInfo(99999); err == nil {
		t.Error("不存在的用户应报错")
	}
}

// withJWTTTL 给测试配置一个正常的 token 有效期。
//
// 测试进程没有加载 config.yaml，AccessExpire/RefreshExpire 都是 0，
// 签发出来的 token 立即过期，parseToken 会直接判为无效 ——
// 那样就只能覆盖「token 无效」这一条分支，走不到后面的吊销判定。
func withJWTTTL(t *testing.T) {
	t.Helper()
	prevAccess, prevRefresh := config.Cfg.JWT.AccessExpire, config.Cfg.JWT.RefreshExpire
	config.Cfg.JWT.AccessExpire, config.Cfg.JWT.RefreshExpire = 3600, 7200
	t.Cleanup(func() {
		config.Cfg.JWT.AccessExpire, config.Cfg.JWT.RefreshExpire = prevAccess, prevRefresh
	})
}

func TestAuthServiceAuthenticate(t *testing.T) {
	db := newServiceDB(t)
	withJWTTTL(t)
	svc := NewAuthService().(*authService)

	// 用户不存在：对外文案必须与「密码错误」完全一致，否则可用来枚举用户名
	_, err := svc.authenticate(&dto.LoginRequest{Username: "nobody", Password: "Abc12345"})
	assertBizError(t, err, common.CodeBadRequest)
	notFoundMsg := err.Error()

	hash, err := utils.HashPassword("Abc12345")
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	u := &model.SysUser{
		TenantBaseModel: common.TenantBaseModel{TenantID: 1},
		Username:        "alice", Password: hash, Status: 1,
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("准备用户失败: %v", err)
	}

	_, err = svc.authenticate(&dto.LoginRequest{Username: "alice", Password: "WrongPass1"})
	assertBizError(t, err, common.CodeBadRequest)
	if err.Error() != notFoundMsg {
		t.Errorf("密码错误与用户不存在的提示必须一致（防枚举），实际 %q vs %q", err.Error(), notFoundMsg)
	}

	// 账号被禁用：在密码校验通过之后才暴露状态
	if err := db.Model(u).Update("status", 0).Error; err != nil {
		t.Fatalf("更新状态失败: %v", err)
	}
	_, err = svc.authenticate(&dto.LoginRequest{Username: "alice", Password: "Abc12345"})
	assertBizError(t, err, common.CodeBadRequest)
	if !strings.Contains(err.Error(), "禁用") {
		t.Errorf("应提示账号已被禁用，实际 %v", err)
	}

	// 凭据正确：签发成功，但测试环境没有 Redis → 存 refresh token 失败，
	// 必须明确报错，绝不能返回一个「签得出来却吊销不掉」的 token
	if err := db.Model(u).Update("status", 1).Error; err != nil {
		t.Fatalf("更新状态失败: %v", err)
	}
	_, err = svc.authenticate(&dto.LoginRequest{Username: "alice", Password: "Abc12345"})
	if err == nil {
		t.Fatal("Redis 不可用时必须报错，不能返回无法吊销的 token")
	}
	if !strings.Contains(err.Error(), "refresh token") {
		t.Errorf("应指出是 refresh token 存储失败，实际 %v", err)
	}
}

func TestAuthServiceLoginRejectsInvalidCaptcha(t *testing.T) {
	newServiceDB(t)
	svc := NewAuthService().(*authService)

	// 人机校验是链路第一道闸：拿不到一次性凭证就直接拒绝
	_, err := svc.Login(
		&dto.LoginRequest{Username: "alice", Password: "Abc12345", CaptchaToken: "forged"},
		&dto.LoginContext{IP: "203.0.113.9", UserAgent: "Mozilla/5.0 Chrome/120"},
	)
	assertBizError(t, err, common.CodeBadRequest)
}

func TestAuthServiceRefreshToken(t *testing.T) {
	newServiceDB(t)
	withJWTTTL(t)
	svc := NewAuthService().(*authService)

	// 非法 token → 401 业务错误（前端据此清会话跳登录页）
	_, err := svc.RefreshToken(&dto.RefreshTokenRequest{RefreshToken: "not-a-token"})
	assertBizError(t, err, common.CodeUnauthorized)

	// 合法 token：解析通过后要查吊销状态，Redis 不可用必须拒绝（fail-closed）。
	// 若这里放行，已停用账号就能继续换发新 access token。
	tok, err := auth.GenerateRefreshToken(1, "alice", 1, 0)
	if err != nil {
		t.Fatalf("签发 refresh token 失败: %v", err)
	}
	_, err = svc.RefreshToken(&dto.RefreshTokenRequest{RefreshToken: tok})
	if err == nil {
		t.Fatal("吊销状态查不出来时必须拒绝续期")
	}
	if common.IsBizError(err) {
		t.Errorf("设施故障应表现为系统错误，而不是业务错误: %v", err)
	}
}

func TestAuthServiceLogout(t *testing.T) {
	newServiceDB(t)
	withJWTTTL(t)
	svc := NewAuthService().(*authService)

	// 没有任何凭据：无副作用，成功返回（旧客户端不带 body 的情形）
	if err := svc.LogoutByToken("", ""); err != nil {
		t.Errorf("空凭据登出应成功: %v", err)
	}

	// 带 refresh token：吊销失败必须报错。
	// 静默成功会让用户以为已登出，实际手里的 refresh token 仍能换发 access token。
	if err := svc.LogoutByToken("", "some-refresh-token"); err == nil {
		t.Error("refresh token 吊销失败必须报错")
	}

	// 带 access token：拉黑失败只记日志（access token 本就短命），
	// 但 refresh token 那一步失败仍要上抛
	access, err := auth.GenerateAccessToken(5, "alice", 1, 0)
	if err != nil {
		t.Fatalf("签发 access token 失败: %v", err)
	}
	if err := svc.LogoutByToken(access, "rt"); err == nil {
		t.Error("refresh token 吊销失败必须报错")
	}

	// 未携带 refresh token（旧客户端）：退化为吊销该用户全部 refresh token，
	// 读不到用户 token 集合时必须报错而不是假装成功
	if err := svc.Logout(5, ""); err == nil {
		t.Error("读取用户 refresh token 集合失败时应报错")
	}
}

// TestLoginGuardHelpers 登录守卫里的纯函数与日志落库。
func TestLoginGuardHelpers(t *testing.T) {
	db := newServiceDB(t)
	svc := NewAuthService().(*authService)

	// parseUA：命中关键字即返回，都不命中返回 Unknown
	if got := parseUA("Mozilla/5.0 (Windows NT 10.0) Chrome/120 Safari/537.36", []string{"Chrome", "Firefox"}); got != "Chrome" {
		t.Errorf("应识别出 Chrome，实际 %q", got)
	}
	if got := parseUA("curl/8.0", []string{"Chrome", "Firefox"}); got != "Unknown" {
		t.Errorf("都不命中应返回 Unknown，实际 %q", got)
	}

	// loginIP 必须空指针安全：调用方到处传上下文，判空散落各处迟早漏
	if got := loginIP(nil); got != "" {
		t.Errorf("nil 上下文应返回空串，实际 %q", got)
	}
	if got := loginIP(&dto.LoginContext{IP: "1.2.3.4"}); got != "1.2.3.4" {
		t.Errorf("应取出 IP，实际 %q", got)
	}

	// 锁定提示要带上窗口时长，用户才知道等多久
	if msg := loginLockedError().Error(); !strings.Contains(msg, "15") {
		t.Errorf("锁定提示应包含窗口时长，实际 %q", msg)
	}

	// 登录日志：无论成功失败都要留痕，审计价值一半在失败记录里
	svc.saveLoginLog(1, "alice", 1, "登录成功", &dto.LoginContext{
		IP: "203.0.113.9", UserAgent: "Mozilla/5.0 (Windows NT 10.0) Chrome/120",
	})
	var row model.SysLoginLog
	if err := db.Where("username = ?", "alice").First(&row).Error; err != nil {
		t.Fatalf("应写入登录日志: %v", err)
	}
	if row.Browser != "Chrome" || row.OS != "Windows" {
		t.Errorf("UA 解析不符: browser=%q os=%q", row.Browser, row.OS)
	}
	if row.IP != "203.0.113.9" || row.TenantID != 1 || row.Status != 1 {
		t.Errorf("登录日志内容不符: %+v", row)
	}

	// 上下文为 nil 也不能 panic（日志写失败不应把登录一起拖垮）
	svc.saveLoginLog(0, "bob", 0, "登录失败", nil)

	// 失败计数：Redis 不可用时只记日志、不 panic —— 限频失效必须可见
	recordLoginFailure(context.Background(), "login:fail:ip:203.0.113.9")
	clearLoginFailure(context.Background(), "login:fail:ip:203.0.113.9")
}
