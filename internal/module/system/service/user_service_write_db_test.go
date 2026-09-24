package service

import (
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/middleware"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/pkg/utils"
)

// 本文件补的是「写路径」上此前没跑过的分支：
// 审计条目落库（0%）以及用户创建/更新时的引用归属校验。
//
// ⚠️ testsupport.NewDB 会改写包级 database.DB，因此不能 t.Parallel。

// TestOperationLogWriterMapsEntry 审计条目与 sys_operation_log 之间的唯一映射点。
//
// 字段漏映射不会报错，只会让审计记录缺列 —— 等真出事去查日志时才发现
// 「有记录但看不到是谁操作的」。所以这里逐字段钉住。
func TestOperationLogWriterMapsEntry(t *testing.T) {
	db := newServiceDB(t)
	w := NewOperationLogWriter()

	// nil 条目静默忽略，不应 panic
	if err := w.WriteOperationLog(nil); err != nil {
		t.Errorf("nil 条目应静默忽略: %v", err)
	}

	entry := &middleware.OperationLogEntry{
		TenantID:      7,
		Title:         "用户管理",
		Action:        "新增",
		RequestMethod: "POST",
		RequestURL:    "/api/v1/system/user",
		RequestParam:  `{"username":"alice"}`,
		Status:        1,
		IP:            "203.0.113.9",
		UserAgent:     "curl/8.0",
		OperatorID:    3,
		OperatorName:  "admin",
		CostTime:      12,
		ErrorMsg:      "",
	}
	if err := w.WriteOperationLog(entry); err != nil {
		t.Fatalf("写入审计条目失败: %v", err)
	}

	var got model.SysOperationLog
	if err := db.Where("request_url = ?", entry.RequestURL).First(&got).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.TenantID != 7 || got.Title != "用户管理" || got.Action != "新增" {
		t.Errorf("基础字段映射不符: %+v", got)
	}
	if got.RequestMethod != "POST" || got.RequestParam != entry.RequestParam {
		t.Errorf("请求字段映射不符: %+v", got)
	}
	if got.IP != entry.IP || got.UserAgent != entry.UserAgent {
		t.Errorf("来源字段映射不符: %+v", got)
	}
	if got.OperatorID != 3 || got.OperatorName != "admin" || got.CostTime != 12 || got.Status != 1 {
		t.Errorf("操作人/耗时字段映射不符: %+v", got)
	}
}

// TestUserServiceCreateValidatesReferencesWithRealRepo 用户创建时的三处归属校验。
//
// 角色 / 岗位 / 部门都是「指向租户内表的 ID」，且 sys_user_role、sys_user_post
// 是**没有 tenant_id 的纯关联表** —— 隔离只能靠 Service 层校验。
// 不校验就能把用户绑到别的租户的角色上（ID 可枚举），而 Casbin 策略按角色 code
// 生成，绑上即继承对方权限，构成跨租户提权。
func TestUserServiceCreateValidatesReferencesWithRealRepo(t *testing.T) {
	db := newServiceDB(t)
	svc := NewUserService()

	role := &model.SysRole{TenantBaseModel: common.TenantBaseModel{TenantID: 1}, Name: "编辑", Code: "editor", Status: 1}
	post := &model.SysPost{TenantBaseModel: common.TenantBaseModel{TenantID: 1}, Code: "dev", Name: "开发", Status: 1}
	dept := &model.SysDept{TenantBaseModel: common.TenantBaseModel{TenantID: 1}, Name: "研发部", Status: 1}
	foreignRole := &model.SysRole{TenantBaseModel: common.TenantBaseModel{TenantID: 2}, Name: "外租户", Code: "foreign", Status: 1}
	for _, row := range []any{role, post, dept, foreignRole} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("准备数据失败: %v", err)
		}
	}

	base := func(username string) *dto.CreateUserRequest {
		return &dto.CreateUserRequest{
			Username: username,
			Password: "Abc12345",
			Nickname: username,
			Status:   1,
			DeptID:   dept.ID,
			RoleIds:  []uint{role.ID},
			PostIds:  []uint{post.ID},
		}
	}

	// 正常路径：三处引用都合法
	if err := svc.Create(1, base("alice"), 9); err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	var alice model.SysUser
	if err := db.Where("username = ?", "alice").First(&alice).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if alice.TenantID != 1 || alice.CreateBy != 9 || alice.DeptID != dept.ID {
		t.Errorf("用户归属不符: %+v", alice)
	}
	var linkCount int64
	if err := db.Model(&model.SysUserRole{}).Where("user_id = ?", alice.ID).Count(&linkCount).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if linkCount != 1 {
		t.Errorf("应写入 1 条角色关联，实际 %d", linkCount)
	}

	// 用户名全局唯一（登录接口不带租户字段，只能全局定位）
	assertBizError(t, svc.Create(1, base("alice"), 9), common.CodeBadRequest)

	// 引用别的租户的角色 → 拒绝，且不能留下半成品用户
	req := base("bob")
	req.RoleIds = []uint{foreignRole.ID}
	assertBizError(t, svc.Create(1, req, 9), common.CodeBadRequest)
	var bobCount int64
	if err := db.Model(&model.SysUser{}).Where("username = ?", "bob").Count(&bobCount).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if bobCount != 0 {
		t.Error("校验失败不应留下半成品用户")
	}

	// 引用不存在的岗位
	req = base("carol")
	req.PostIds = []uint{99999}
	assertBizError(t, svc.Create(1, req, 9), common.CodeBadRequest)

	// 引用不存在的部门（与「属于别的租户」对调用方不可区分，刻意如此）
	req = base("dave")
	req.DeptID = 99999
	assertBizError(t, svc.Create(1, req, 9), common.CodeBadRequest)

	// 弱密码
	req = base("erin")
	req.Password = "123"
	assertBizError(t, svc.Create(1, req, 9), common.CodeBadRequest)
}

// TestUserServiceUpdateWritesEveryProvidedField 部分更新：提供了就改，没提供就保持。
func TestUserServiceUpdateWritesEveryProvidedField(t *testing.T) {
	db := newServiceDB(t)
	svc := NewUserService()

	hash, err := utils.HashPassword("Abc12345")
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	role := &model.SysRole{TenantBaseModel: common.TenantBaseModel{TenantID: 1}, Name: "编辑", Code: "editor", Status: 1}
	post := &model.SysPost{TenantBaseModel: common.TenantBaseModel{TenantID: 1}, Code: "dev", Name: "开发", Status: 1}
	dept := &model.SysDept{TenantBaseModel: common.TenantBaseModel{TenantID: 1}, Name: "研发部", Status: 1}
	u := &model.SysUser{
		TenantBaseModel: common.TenantBaseModel{TenantID: 1},
		Username:        "alice", Password: hash, Nickname: "旧昵称", Email: "old@example.com", Status: 1,
	}
	for _, row := range []any{role, post, dept, u} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("准备数据失败: %v", err)
		}
	}

	nickname, email, phone, remark := "新昵称", "new@example.com", "13800138000", "备注"
	status := int8(0)
	req := &dto.UpdateUserRequest{
		ID:       u.ID,
		Nickname: &nickname,
		Email:    &email,
		Phone:    &phone,
		Status:   &status,
		DeptID:   &dept.ID,
		Remark:   &remark,
		RoleIds:  []uint{role.ID},
		PostIds:  []uint{post.ID},
	}
	if err := svc.Update(1, req, 9); err != nil {
		t.Fatalf("更新用户失败: %v", err)
	}

	var got model.SysUser
	if err := db.First(&got, u.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.Nickname != nickname || got.Email != email || got.Phone != phone {
		t.Errorf("基础字段未更新: %+v", got)
	}
	if got.Status != 0 || got.DeptID != dept.ID || got.Remark != remark {
		t.Errorf("状态/部门/备注未更新: %+v", got)
	}
	if got.UpdateBy != 9 {
		t.Errorf("更新者应记录为 9，实际 %d", got.UpdateBy)
	}
	// 用户名不开放修改
	if got.Username != "alice" {
		t.Errorf("用户名不应被改动，实际 %q", got.Username)
	}

	var roleLinks, postLinks int64
	if err := db.Model(&model.SysUserRole{}).Where("user_id = ?", u.ID).Count(&roleLinks).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if err := db.Model(&model.SysUserPost{}).Where("user_id = ?", u.ID).Count(&postLinks).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if roleLinks != 1 || postLinks != 1 {
		t.Errorf("角色/岗位关联应各 1 条，实际 %d / %d", roleLinks, postLinks)
	}

	// 更新不存在的用户 → 404
	missing := &dto.UpdateUserRequest{ID: 99999, Nickname: &nickname}
	assertBizError(t, svc.Update(1, missing, 9), common.CodeNotFound)

	// 更新时引用别的租户的岗位同样要拒绝，且资料改动不能落地（不能留半成品状态）
	foreignPost := &model.SysPost{TenantBaseModel: common.TenantBaseModel{TenantID: 2}, Code: "dev2", Name: "别人的", Status: 1}
	if err := db.Create(foreignPost).Error; err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}
	other := "改一半"
	bad := &dto.UpdateUserRequest{ID: u.ID, Nickname: &other, PostIds: []uint{foreignPost.ID}}
	assertBizError(t, svc.Update(1, bad, 9), common.CodeBadRequest)
	if err := db.First(&got, u.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.Nickname == other {
		t.Error("岗位校验失败时不应把昵称改掉（不能留下半成品状态）")
	}
}
