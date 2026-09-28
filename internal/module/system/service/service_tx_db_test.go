package service

import (
	"fmt"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/repository"

	"gorm.io/gorm"
)

// 本文件验证「一次业务操作 = 一个事务」这条不变量（P1-5 / M2·M3）。
//
// 覆盖的三处此前都是「主表落库后再分别写关联表」：
//   - userService.Create / Update：sys_user + sys_user_role + sys_user_post
//   - roleService.Create / Update：sys_role + sys_role_menu
//   - configService.BatchSave：一批 sys_config
//
// 断言方式：**注入一次中途失败**，再检查先前的写入是否被回滚。
// 只断言「返回了错误」是不够的 —— 那样即使把事务整个删掉也能通过。
//
// ⚠️ 做变异检查（把事务改成不生效）时会看到本文件的部分用例**卡住而不是报错**：
// 内存库被 testsupport 限制为单连接，事务持有它之后，回调里若误用 `s.userRepo`
// （而非参数 txRepo）去写库，就会在连接池上无限等待。
// 这是「被测代码确实在事务里」的一种表现，不是环境故障；
// 想要快速失败的信号请看 repository/transaction_test.go —— 那里没有外层事务占连接，
// 失效时会直接断言失败。
//
// ⚠️ testsupport.NewDB 会改写包级 database.DB，因此不能 t.Parallel。

var failWriteSeq int

// failNthWriteTo 让对指定表的第 n 次写入失败（n <= 0 表示每一次都失败）。
//
// 为什么用 GORM 回调而不是「把表删掉」：回调不改 schema，不波及其他用例，
// 而且能精确指定「第几步失败」—— 批量保存这类场景需要「第一项成功、第二项失败」，
// 只让整张表不可写是造不出这个中间态的。
func failNthWriteTo(t *testing.T, db *gorm.DB, table string, n int) {
	t.Helper()

	failWriteSeq++
	cbName := fmt.Sprintf("test:fail_nth_write_%d", failWriteSeq)

	seen := 0
	_ = db.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != table {
			return
		}
		seen++
		if n <= 0 || seen == n {
			// AddError 的返回值刻意忽略：注入失败后事务注定回滚，
			// 这里没有可恢复的动作；lint 的 errcheck 由本注释说明
			_ = tx.AddError(fmt.Errorf("注入的写入失败: %s（第 %d 次）", table, seen))
		}
	})
	t.Cleanup(func() {
		// Remove 只在回调已注册时返回 nil；清理路径无动作可做
		_ = db.Callback().Create().Remove(cbName)
	})
}

// seedRoleAndPost 造出一对可用的角色/岗位。
// normalizeRoleIDs / normalizePostIDs 会按租户校验归属，必须真实存在。
func seedRoleAndPost(t *testing.T, tenantID uint) (*model.SysRole, *model.SysPost) {
	t.Helper()

	role := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Name:            "编辑", Code: "editor", Status: 1,
	}
	if err := repository.NewRoleRepository().Create(role); err != nil {
		t.Fatalf("准备角色失败: %v", err)
	}

	post := &model.SysPost{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Code:            "dev", Name: "研发", Status: 1,
	}
	if err := repository.NewPostRepository().Create(post); err != nil {
		t.Fatalf("准备岗位失败: %v", err)
	}

	return role, post
}

// TestUserServiceCreateIsAtomic 建用户必须整体成功或整体失败。
//
// 修复前：Create / ReplaceRoles / ReplacePosts 各写各的，
// 岗位写入失败时用户已经落库 —— 调用方拿到 500 以为整次操作失败，
// 实际那个账号已经存在且能登录，只是没有任何角色/岗位。
// 界面上完全看不出来，只能靠事务保证不出现。
func TestUserServiceCreateIsAtomic(t *testing.T) {
	db := newServiceDB(t)
	role, post := seedRoleAndPost(t, 1)

	// 岗位关联表写入失败：此时用户与角色关联都已写入
	failNthWriteTo(t, db, "sys_user_post", 0)

	svc := NewUserService()
	err := svc.Create(1, &dto.CreateUserRequest{
		Username: "atomic",
		Password: "Abc12345!",
		Nickname: "原子",
		Status:   1,
		RoleIds:  []uint{role.ID},
		PostIds:  []uint{post.ID},
	}, 1)
	if err == nil {
		t.Fatal("岗位写入失败时应返回错误")
	}

	var users, rels int64
	if err := db.Model(&model.SysUser{}).Count(&users).Error; err != nil {
		t.Fatalf("统计用户失败: %v", err)
	}
	if err := db.Model(&model.SysUserRole{}).Count(&rels).Error; err != nil {
		t.Fatalf("统计角色关联失败: %v", err)
	}
	if users != 0 {
		t.Errorf("回滚后不应残留用户，实际 %d 条 —— 主表与关联表写入不在同一事务", users)
	}
	if rels != 0 {
		t.Errorf("回滚后不应残留角色关联，实际 %d 条", rels)
	}
}

// TestUserServiceCreateCommitsWhenAllSucceed 成功路径不能被事务改坏。
func TestUserServiceCreateCommitsWhenAllSucceed(t *testing.T) {
	db := newServiceDB(t)
	role, post := seedRoleAndPost(t, 1)

	svc := NewUserService()
	if err := svc.Create(1, &dto.CreateUserRequest{
		Username: "ok",
		Password: "Abc12345!",
		Nickname: "正常",
		Status:   1,
		RoleIds:  []uint{role.ID},
		PostIds:  []uint{post.ID},
	}, 1); err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	var users, rels, posts int64
	db.Model(&model.SysUser{}).Count(&users)
	db.Model(&model.SysUserRole{}).Count(&rels)
	db.Model(&model.SysUserPost{}).Count(&posts)
	if users != 1 || rels != 1 || posts != 1 {
		t.Errorf("应各写入 1 条，实际 user=%d role=%d post=%d", users, rels, posts)
	}
}

// TestUserServiceUpdateIsAtomic 改用户同理：资料改了、岗位没写成功 → 整体回滚。
//
// 这里同时钉住「缓存清理/Token 吊销必须在提交之后」这条约束的**可见部分**：
// 提交失败时不应有任何副作用留下。
func TestUserServiceUpdateIsAtomic(t *testing.T) {
	db := newServiceDB(t)
	role, post := seedRoleAndPost(t, 1)

	svc := NewUserService()
	if err := svc.Create(1, &dto.CreateUserRequest{
		Username: "target",
		Password: "Abc12345!",
		Nickname: "原名",
		Status:   1,
	}, 1); err != nil {
		t.Fatalf("准备用户失败: %v", err)
	}

	var user model.SysUser
	if err := db.Where("username = ?", "target").First(&user).Error; err != nil {
		t.Fatalf("读取用户失败: %v", err)
	}

	failNthWriteTo(t, db, "sys_user_post", 0)

	newName := "改过的名字"
	err := svc.Update(1, &dto.UpdateUserRequest{
		ID:      user.ID,
		Nickname: &newName,
		RoleIds: []uint{role.ID},
		PostIds: []uint{post.ID},
	}, 1)
	if err == nil {
		t.Fatal("岗位写入失败时应返回错误")
	}

	var after model.SysUser
	if err := db.Where("id = ?", user.ID).First(&after).Error; err != nil {
		t.Fatalf("重新读取用户失败: %v", err)
	}
	if after.Nickname != "原名" {
		t.Errorf("回滚后昵称应保持 %q，实际 %q —— 主表更新未被回滚", "原名", after.Nickname)
	}
	var rels int64
	db.Model(&model.SysUserRole{}).Count(&rels)
	if rels != 0 {
		t.Errorf("回滚后不应残留角色关联，实际 %d 条", rels)
	}
}

// TestRoleServiceCreateIsAtomic 建角色同理：授权写入失败时角色不能留下。
//
// 修复前还有第二个问题：ReplaceMenus 失败会直接 return，
// 连 syncPolicies() 都被跳过 —— 于是库里多出一个「已创建、无授权」的角色，
// 而 Casbin 策略仍是旧快照，权限状态无人同步。
func TestRoleServiceCreateIsAtomic(t *testing.T) {
	db := newServiceDB(t)

	menu := &model.SysMenu{Name: "仪表盘", Title: "仪表盘", Type: 1, Status: 1}
	if err := db.Create(menu).Error; err != nil {
		t.Fatalf("准备菜单失败: %v", err)
	}

	// 菜单关联表写入失败
	failNthWriteTo(t, db, "sys_role_menu", 0)

	svc := NewRoleService()
	err := svc.Create(&dto.CreateRoleRequest{
		Name:    "编辑",
		Code:    "editor",
		Status:  1,
		MenuIds: []uint{menu.ID},
	}, 1, 1)
	if err == nil {
		t.Fatal("菜单关联写入失败时应返回错误")
	}

	var roles, rels int64
	db.Model(&model.SysRole{}).Count(&roles)
	db.Model(&model.SysRoleMenu{}).Count(&rels)
	if roles != 0 {
		t.Errorf("回滚后不应残留角色，实际 %d 条 —— 角色本体与授权不在同一事务", roles)
	}
	if rels != 0 {
		t.Errorf("回滚后不应残留菜单关联，实际 %d 条", rels)
	}
}

// TestRoleServiceUpdateIsAtomic 改角色同理。
func TestRoleServiceUpdateIsAtomic(t *testing.T) {
	db := newServiceDB(t)

	menu := &model.SysMenu{Name: "仪表盘", Title: "仪表盘", Type: 1, Status: 1}
	if err := db.Create(menu).Error; err != nil {
		t.Fatalf("准备菜单失败: %v", err)
	}
	role := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: 1},
		Name:            "原名", Code: "editor", Status: 1,
	}
	if err := repository.NewRoleRepository().Create(role); err != nil {
		t.Fatalf("准备角色失败: %v", err)
	}

	failNthWriteTo(t, db, "sys_role_menu", 0)

	svc := NewRoleService()
	err := svc.Update(&dto.UpdateRoleRequest{
		ID:      role.ID,
		Name:    "改过的名字",
		MenuIds: []uint{menu.ID},
	}, 1, 1)
	if err == nil {
		t.Fatal("菜单关联写入失败时应返回错误")
	}

	var after model.SysRole
	if err := db.Where("id = ?", role.ID).First(&after).Error; err != nil {
		t.Fatalf("重新读取角色失败: %v", err)
	}
	if after.Name != "原名" {
		t.Errorf("回滚后角色名应保持 %q，实际 %q —— 主表更新未被回滚", "原名", after.Name)
	}
}

// TestConfigServiceBatchSaveIsAtomic 一批配置必须一次提交。
//
// 逐条提交时，中途失败会留下「前几项已生效、后面几项没写」的混合状态。
// 对支付/OSS 这类成组配置尤其危险：密钥写了一半的现象是「签名失败」，
// 完全指不到是配置没存全。
func TestConfigServiceBatchSaveIsAtomic(t *testing.T) {
	db := newServiceDB(t)

	// 第 1 项成功、第 2 项失败 —— 第 1 项也必须一起回滚
	failNthWriteTo(t, db, "sys_config", 2)

	svc := NewConfigService()
	err := svc.BatchSave("pay.", []ConfigItem{
		{Key: "app_id", Value: "APPID"},
		{Key: "mch_id", Value: "MCHID"},
	}, 1)
	if err == nil {
		t.Fatal("第 2 项写入失败时应返回错误")
	}

	var count int64
	if err := db.Model(&model.SysConfig{}).Count(&count).Error; err != nil {
		t.Fatalf("统计配置失败: %v", err)
	}
	if count != 0 {
		t.Errorf("整批应回滚，实际残留 %d 条配置 —— 批量保存未包在一个事务里", count)
	}
}

// TestConfigServiceBatchSaveCommits 成功路径不能被事务改坏。
func TestConfigServiceBatchSaveCommits(t *testing.T) {
	db := newServiceDB(t)

	svc := NewConfigService()
	if err := svc.BatchSave("pay.", []ConfigItem{
		{Key: "app_id", Value: "APPID"},
		{Key: "mch_id", Value: "MCHID"},
	}, 1); err != nil {
		t.Fatalf("批量保存失败: %v", err)
	}

	var count int64
	db.Model(&model.SysConfig{}).Count(&count)
	if count != 2 {
		t.Errorf("应写入 2 条配置，实际 %d 条", count)
	}
}
