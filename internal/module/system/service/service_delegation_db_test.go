package service

import (
	"context"
	"mime/multipart"
	"strings"
	"testing"
	"time"

	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"

	"gorm.io/gorm"
)

// 本文件用「真实仓储 + 内存库」补齐 service 层此前 0% 的方法。
//
// 为什么不用手写桩：这里大多是薄封装（透传到 Repository），桩会把
// 「参数有没有正确透传」「租户条件有没有带上」「软删除有没有释放唯一键」
// 这些真正会出错的地方一起替换掉，测了等于没测。走真实仓储顺带把
// 构造器（NewXxxService）也覆盖上 —— 它们此前全是 0%，正是因为
// 老用例一律 `&xxxService{repo: mock}` 直接构造。
//
// ⚠️ testsupport.NewDB 会改写包级 database.DB，因此不能 t.Parallel。
// ⚠️ 必须先建库再构造 Service：仓储在构造时捕获 database.DB，顺序反了拿到 nil。

func newServiceDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testsupport.NewDB(t,
		&model.SysConfig{}, &model.SysDictType{}, &model.SysDictData{},
		&model.SysPost{}, &model.SysUser{}, &model.SysUserRole{}, &model.SysUserPost{},
		&model.SysRole{}, &model.SysRoleMenu{}, &model.SysMenu{}, &model.SysDept{},
		&model.SysAgreement{}, &model.SysOperationLog{}, &model.SysLoginLog{},
		&model.SysFile{},
	)
}

// ---- 配置 ----

func TestConfigServiceCRUD(t *testing.T) {
	db := newServiceDB(t)
	svc := NewConfigService()

	if err := svc.Create("站点名称", "site.name", "Gin-Admin", 1, 7); err != nil {
		t.Fatalf("创建配置失败: %v", err)
	}
	// config_key 上有全局唯一索引：冲突必须是可读的业务错误，不能落到 500
	assertBizError(t, svc.Create("重复键", "site.name", "x", 1, 7), common.CodeBadRequest)

	raw, err := svc.FindByKey("site.name")
	if err != nil {
		t.Fatalf("按 key 查询失败: %v", err)
	}
	cfg := raw.(*model.SysConfig)
	if cfg.Value != "Gin-Admin" || cfg.CreateBy != 7 {
		t.Errorf("配置内容不符: %+v", cfg)
	}

	raw, err = svc.FindByID(cfg.ID)
	if err != nil {
		t.Fatalf("按 ID 查询失败: %v", err)
	}
	if got := raw.(*model.SysConfig).ConfigKey; got != "site.name" {
		t.Errorf("按 ID 查到的是 %q", got)
	}

	// 单条查询必须转成 404 而不是 500，否则用户不知道是自己传的 ID 不对
	_, err = svc.FindByID(99999)
	assertBizError(t, err, common.CodeNotFound)

	// 过滤条件打在 name 上（不是 config_key）
	list, total, err := svc.FindList("站点", 1, 10)
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Errorf("应查到 1 条，实际 total=%d len=%d", total, len(list))
	}

	if err := svc.Delete(cfg.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	var left int64
	if err := db.Model(&model.SysConfig{}).Count(&left).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if left != 0 {
		t.Errorf("删除后不应还有记录，实际 %d", left)
	}
}

// TestConfigServiceMasksSensitiveValues 敏感项对外必须打码，内部读取必须拿到真值。
//
// 两个方向都要钉住：
//   - 对外（FindList / FindByPrefix）返回 "******"，否则密钥会回传到浏览器
//   - 对内（FindByPrefixRaw）返回真实值，否则上传模块拿不到凭据，
//     现象是「配置明明填了却没用上」
func TestConfigServiceMasksSensitiveValues(t *testing.T) {
	newServiceDB(t)
	svc := NewConfigService()

	seeds := []struct{ key, value string }{
		{"pay.wechat_key", "REAL-WECHAT-KEY"},
		{"pay.alipay_key", "REAL-ALIPAY-KEY"},
		{"oss.secret_key", "REAL-OSS-SECRET"},
		{"pay.empty_key", ""}, // 空值不打码：没配置的项应显示空，而不是 ******
		{"site.name", "站点名"},
	}
	for _, s := range seeds {
		if err := svc.Create(s.key, s.key, s.value, 1, 1); err != nil {
			t.Fatalf("准备配置 %s 失败: %v", s.key, err)
		}
	}

	list, _, err := svc.FindList("", 1, 50)
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	byKey := map[string]string{}
	for _, item := range list {
		c := item.(model.SysConfig)
		byKey[c.ConfigKey] = c.Value
	}
	for _, k := range []string{"pay.wechat_key", "pay.alipay_key", "oss.secret_key"} {
		if byKey[k] != maskedValue {
			t.Errorf("列表里 %s 应被打码，实际 %q", k, byKey[k])
		}
	}
	if byKey["site.name"] != "站点名" {
		t.Errorf("非敏感项不应被打码，实际 %q", byKey["site.name"])
	}
	if v, ok := byKey["pay.empty_key"]; !ok || v != "" {
		t.Errorf("空值不应被打码，实际 %q", v)
	}

	prefixed, err := svc.FindByPrefix("pay.")
	if err != nil {
		t.Fatalf("前缀查询失败: %v", err)
	}
	for _, item := range prefixed {
		c := item.(model.SysConfig)
		if c.ConfigKey != "pay.empty_key" && c.Value != maskedValue {
			t.Errorf("前缀查询里 %s 应被打码，实际 %q", c.ConfigKey, c.Value)
		}
	}

	raws, err := svc.FindByPrefixRaw("pay.")
	if err != nil {
		t.Fatalf("原始前缀查询失败: %v", err)
	}
	rawByKey := map[string]string{}
	for _, item := range raws {
		c := item.(model.SysConfig)
		rawByKey[c.ConfigKey] = c.Value
	}
	if rawByKey["pay.wechat_key"] != "REAL-WECHAT-KEY" {
		t.Errorf("内部读取必须拿到真实密钥，实际 %q", rawByKey["pay.wechat_key"])
	}
}

// TestConfigServiceBatchSaveSkipsMaskedAndUpserts 批量保存的两个语义。
//
//   - 原样回传 "******" = 该项未修改 → 跳过，真实密钥保留（否则会被写成字面量）
//   - 库里没有的 key → 插入；已有 key → 只改值，不覆盖名称
func TestConfigServiceBatchSaveSkipsMaskedAndUpserts(t *testing.T) {
	db := newServiceDB(t)
	svc := NewConfigService()

	if err := svc.Create("微信密钥", "pay.wechat_key", "REAL-KEY", 1, 1); err != nil {
		t.Fatalf("准备配置失败: %v", err)
	}
	if err := svc.Create("站点名称", "site.name", "旧名", 1, 1); err != nil {
		t.Fatalf("准备配置失败: %v", err)
	}

	err := svc.BatchSave("pay.", []ConfigItem{
		{Key: "wechat_key", Value: maskedValue}, // 未修改 → 跳过
		{Key: "alipay_key", Value: "NEW-ALIPAY"},
	}, 9)
	if err != nil {
		t.Fatalf("批量保存失败: %v", err)
	}

	var wechat model.SysConfig
	if err := db.Where("config_key = ?", "pay.wechat_key").First(&wechat).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if wechat.Value != "REAL-KEY" {
		t.Errorf("打码占位符不应覆盖真实密钥，实际 %q", wechat.Value)
	}
	if wechat.UpdateBy != 1 {
		t.Errorf("被跳过的项不应更新操作人，实际 %d", wechat.UpdateBy)
	}

	var alipay model.SysConfig
	if err := db.Where("config_key = ?", "pay.alipay_key").First(&alipay).Error; err != nil {
		t.Fatalf("批量保存应插入库中不存在的项: %v", err)
	}
	if alipay.Value != "NEW-ALIPAY" || alipay.UpdateBy != 9 {
		t.Errorf("新项内容不符: %+v", alipay)
	}

	if err := svc.BatchSave("site.", []ConfigItem{{Key: "name", Value: "新名"}}, 9); err != nil {
		t.Fatalf("批量保存失败: %v", err)
	}
	var site model.SysConfig
	if err := db.Where("config_key = ?", "site.name").First(&site).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if site.Value != "新名" {
		t.Errorf("已有项的值应被更新，实际 %q", site.Value)
	}
	if site.Name != "站点名称" {
		t.Errorf("批量保存不应覆盖名称，实际 %q", site.Name)
	}
}

// TestLoadOSSConfigStripsPrefix LoadOSSConfig 剥掉 oss. 前缀供上传模块直接取用。
func TestLoadOSSConfigStripsPrefix(t *testing.T) {
	newServiceDB(t)
	svc := NewConfigService()

	for _, k := range []string{"oss.access_key", "oss.secret_key", "oss.bucket", "site.name"} {
		if err := svc.Create(k, k, "v:"+k, 1, 1); err != nil {
			t.Fatalf("准备配置 %s 失败: %v", k, err)
		}
	}

	got := LoadOSSConfig()
	if got["access_key"] != "v:oss.access_key" {
		t.Errorf("应剥掉 oss. 前缀，实际 %v", got)
	}
	if got["secret_key"] != "v:oss.secret_key" {
		t.Errorf("密钥必须能读到真实值，实际 %v", got)
	}
	if _, exists := got["oss.access_key"]; exists {
		t.Error("不应保留带前缀的键")
	}
	if _, exists := got["name"]; exists {
		t.Error("只应返回 oss. 前缀的配置")
	}
	if len(got) != 3 {
		t.Errorf("应返回 3 项，实际 %d（%v）", len(got), got)
	}
}

// ---- 日志 ----

func TestLogServiceListsAndClear(t *testing.T) {
	db := newServiceDB(t)
	svc := NewLogService()

	if err := svc.CreateOperationLog(&model.SysOperationLog{
		TenantID: 1, Title: "用户管理", Action: "新增", Status: 1, OperatorID: 1,
	}); err != nil {
		t.Fatalf("写操作日志失败: %v", err)
	}
	if err := svc.CreateOperationLog(&model.SysOperationLog{TenantID: 2, Title: "用户管理", Status: 1}); err != nil {
		t.Fatalf("写操作日志失败: %v", err)
	}
	if err := svc.CreateLoginLog(&model.SysLoginLog{
		TenantID: 1, Username: "alice", Status: 1, LoginTime: time.Now(),
	}); err != nil {
		t.Fatalf("写登录日志失败: %v", err)
	}
	if err := svc.CreateLoginLog(&model.SysLoginLog{
		TenantID: 2, Username: "bob", Status: 1, LoginTime: time.Now(),
	}); err != nil {
		t.Fatalf("写登录日志失败: %v", err)
	}

	list, total, err := svc.FindOperationLogList(1, "", nil, 1, 10)
	if err != nil {
		t.Fatalf("操作日志列表失败: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Errorf("应只看到本租户 1 条操作日志，实际 total=%d len=%d", total, len(list))
	}

	llist, ltotal, err := svc.FindLoginLogList(1, "ali", nil, 1, 10)
	if err != nil {
		t.Fatalf("登录日志列表失败: %v", err)
	}
	if ltotal != 1 || len(llist) != 1 {
		t.Errorf("应只看到本租户 1 条登录日志，实际 total=%d len=%d", ltotal, len(llist))
	}

	if err := svc.ClearOperationLogs(1); err != nil {
		t.Fatalf("清空操作日志失败: %v", err)
	}
	if err := svc.ClearLoginLogs(1); err != nil {
		t.Fatalf("清空登录日志失败: %v", err)
	}

	// 其他租户的审计记录不能被连带清掉
	var otherOp, otherLogin int64
	if err := db.Model(&model.SysOperationLog{}).Where("tenant_id = ?", 2).Count(&otherOp).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if err := db.Model(&model.SysLoginLog{}).Where("tenant_id = ?", 2).Count(&otherLogin).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if otherOp != 1 || otherLogin != 1 {
		t.Errorf("其他租户日志不应被清除（操作 %d，登录 %d）", otherOp, otherLogin)
	}

	// tenantID=0 会退化成「不过滤」= 清空全平台，必须直接拒绝。
	//
	// 这里断言的是「仓库层显式护栏」而不是「有 error 就行」：GORM 对不带条件的
	// Delete 本身会返回 "WHERE conditions required"，把仓库层的 if tenantID == 0
	// 整段删掉后，这个兜底仍会报错 —— 只断言 err != nil 根本区分不出护栏在不在
	// （变异验证实测转绿）。所以必须钉住错误来源。
	if err := svc.ClearOperationLogs(0); err == nil || !strings.Contains(err.Error(), "租户上下文") {
		t.Errorf("缺少租户上下文时必须由仓库层显式拒绝，实际 err=%v", err)
	}
	if err := svc.ClearLoginLogs(0); err == nil || !strings.Contains(err.Error(), "租户上下文") {
		t.Errorf("缺少租户上下文时必须由仓库层显式拒绝（登录日志），实际 err=%v", err)
	}
}

// TestLogServiceCleanExpiredLogs 定时清理：按保留期删除，且保留期 <= 0 时不做事。
func TestLogServiceCleanExpiredLogs(t *testing.T) {
	db := newServiceDB(t)
	svc := NewLogService()

	old := time.Now().AddDate(0, 0, -40)
	fresh := time.Now().AddDate(0, 0, -1)

	for _, row := range []*model.SysOperationLog{
		{TenantID: 1, Title: "旧", CreatedAt: old},
		{TenantID: 1, Title: "新", CreatedAt: fresh},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("准备操作日志失败: %v", err)
		}
	}
	for _, row := range []*model.SysLoginLog{
		{TenantID: 1, Username: "旧", LoginTime: old},
		{TenantID: 1, Username: "新", LoginTime: fresh},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("准备登录日志失败: %v", err)
		}
	}

	// 保留期 <= 0 表示不清理（误配成 0 时绝不能变成「清空全部」）
	n, err := svc.CleanExpiredLogs(context.Background(), 0)
	if err != nil {
		t.Fatalf("保留期为 0 不应报错: %v", err)
	}
	if n != 0 {
		t.Errorf("保留期为 0 不应删除任何日志，实际删了 %d 条", n)
	}

	n, err = svc.CleanExpiredLogs(context.Background(), 30)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if n != 2 {
		t.Errorf("应删除 2 条超期日志（操作 + 登录各 1），实际 %d", n)
	}

	var opLeft, loginLeft int64
	if err := db.Model(&model.SysOperationLog{}).Count(&opLeft).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if err := db.Model(&model.SysLoginLog{}).Count(&loginLeft).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if opLeft != 1 || loginLeft != 1 {
		t.Errorf("未超期的日志应保留（操作剩 %d，登录剩 %d）", opLeft, loginLeft)
	}
}

// ---- 仪表盘 ----

// TestDashboardServiceGetStats 统计口径必须逐表对齐租户模型。
//
// sys_dept 曾经被当成全局表统计，于是每个租户的首页都显示全平台的部门数 ——
// 既不准确，也顺带泄漏平台规模。这条用例把这个口径钉住。
func TestDashboardServiceGetStats(t *testing.T) {
	db := newServiceDB(t)
	svc := NewDashboardService()

	seed := []any{
		&model.SysUser{TenantBaseModel: common.TenantBaseModel{TenantID: 1}, Username: "u1", Status: 1},
		&model.SysUser{TenantBaseModel: common.TenantBaseModel{TenantID: 2}, Username: "u2", Status: 1},
		&model.SysDept{TenantBaseModel: common.TenantBaseModel{TenantID: 1}, Name: "A-研发"},
		&model.SysDept{TenantBaseModel: common.TenantBaseModel{TenantID: 2}, Name: "B-市场"},
		&model.SysPost{TenantBaseModel: common.TenantBaseModel{TenantID: 1}, Code: "dev", Name: "开发", Status: 1},
		&model.SysRole{TenantBaseModel: common.TenantBaseModel{TenantID: 1}, Name: "编辑", Code: "editor", Status: 1},
		&model.SysMenu{Name: "系统管理", Type: 0, Status: 1},                    // 全局表
		&model.SysConfig{Name: "站点名", ConfigKey: "site.name", Value: "x"},     // 全局表
		&model.SysOperationLog{TenantID: 1, Title: "用户管理", CreatedAt: time.Now()},
	}
	for _, row := range seed {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("准备数据失败: %v", err)
		}
	}

	stats, err := svc.GetStats(1)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if stats.UserCount != 1 {
		t.Errorf("用户数应只算本租户，实际 %d", stats.UserCount)
	}
	if stats.DeptCount != 1 {
		t.Errorf("部门数应只算本租户，实际 %d（按全局口径会得到 2）", stats.DeptCount)
	}
	if stats.PostCount != 1 {
		t.Errorf("岗位数应只算本租户，实际 %d", stats.PostCount)
	}
	if stats.RoleCount != 1 {
		t.Errorf("角色数应只算本租户，实际 %d", stats.RoleCount)
	}
	if stats.LogCount != 1 {
		t.Errorf("日志数应只算本租户，实际 %d", stats.LogCount)
	}
	// 全局表：所有租户看到的是同一份，不做租户过滤
	if stats.MenuCount != 1 {
		t.Errorf("菜单是全局表，应统计全部，实际 %d", stats.MenuCount)
	}
	if stats.ConfigCount != 1 {
		t.Errorf("配置是全局表，应统计全部，实际 %d", stats.ConfigCount)
	}
}

// ---- 文件 ----

func TestFileServiceCRUD(t *testing.T) {
	db := newServiceDB(t)
	svc := NewFileService()

	// 仓储的 Create 不接收 tenantID，归属由 Service 赋值
	f := &model.SysFile{Name: "a.txt", Path: "uploads/a.txt", URL: "/uploads/a.txt", Size: 10, MimeType: "text/plain"}
	if err := svc.Create(3, f); err != nil {
		t.Fatalf("创建文件记录失败: %v", err)
	}
	if f.TenantID != 3 {
		t.Errorf("Service 必须给文件打上租户，实际 %d", f.TenantID)
	}

	got, err := svc.FindByID(3, f.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got.Name != "a.txt" {
		t.Errorf("内容不符: %+v", got)
	}

	// 跨租户查不到 → 404 而不是 500
	_, err = svc.FindByID(4, f.ID)
	assertBizError(t, err, common.CodeNotFound)

	list, total, err := svc.FindList(3, "", "", "desc", 1, 10)
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Errorf("应查到 1 条，实际 total=%d len=%d", total, len(list))
	}

	if err := svc.Delete(3, f.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	var left int64
	if err := db.Model(&model.SysFile{}).Where("tenant_id = ?", 3).Count(&left).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if left != 0 {
		t.Errorf("删除后不应还有记录，实际 %d", left)
	}
}

// TestFileServiceUploadRejectsOversize 上传的大小上限必须在落盘之前拦住。
func TestFileServiceUploadRejectsOversize(t *testing.T) {
	newServiceDB(t)
	svc := NewFileService()

	_, err := svc.Upload(1, 1, &multipart.FileHeader{
		Filename: "big.png",
		Size:     defaultMaxUploadSize + 1,
	})
	assertBizError(t, err, common.CodeFileTooLarge)

	// 存储层失败要包一层上下文（%w 保留错误链），供日志定位根因
	_, err = svc.Upload(1, 1, &multipart.FileHeader{Filename: "a.png", Size: 10})
	if err == nil {
		t.Fatal("存储失败时应报错")
	}
	if !strings.Contains(err.Error(), "保存上传文件失败") {
		t.Errorf("应带上「保存上传文件失败」上下文，实际 %v", err)
	}
}

// ---- 协议 ----

// TestAgreementServiceSanitizesRichText 富文本的净化必须在写入口完成。
//
// 内容会原样渲染到页面上，而 token 存在非 httpOnly Cookie 里，
// 一次 XSS 即可接管账号。创建与更新两条路径都要过滤 ——
// 只过滤创建的话，可以先存干净内容再改成恶意内容绕过。
func TestAgreementServiceSanitizesRichText(t *testing.T) {
	db := newServiceDB(t)
	svc := NewAgreementService()

	dirty := `<p>正文</p><script>alert(1)</script><img src="x" onerror="alert(2)">`
	if err := svc.Create("用户协议", dirty, "user", 1, 1, 3, 7); err != nil {
		t.Fatalf("创建协议失败: %v", err)
	}

	var got model.SysAgreement
	if err := db.Where("title = ?", "用户协议").First(&got).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.TenantID != 7 {
		t.Errorf("租户应取自操作者上下文，实际 %d", got.TenantID)
	}
	if got.CreateBy != 3 {
		t.Errorf("创建者应记录为 3，实际 %d", got.CreateBy)
	}
	lower := strings.ToLower(got.Content)
	if strings.Contains(lower, "<script") {
		t.Errorf("写入路径必须净化 <script>，实际 %q", got.Content)
	}
	if strings.Contains(lower, "onerror") {
		t.Errorf("写入路径必须清除事件处理器，实际 %q", got.Content)
	}
	if !strings.Contains(got.Content, "正文") {
		t.Errorf("正常内容不应被误删，实际 %q", got.Content)
	}

	if err := svc.Update(got.ID, "用户协议", "<script>alert(3)</script>改后", "user", 1, 1, 3, 7); err != nil {
		t.Fatalf("更新协议失败: %v", err)
	}
	if err := db.First(&got, got.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if strings.Contains(strings.ToLower(got.Content), "<script") {
		t.Errorf("更新路径也必须净化，实际 %q", got.Content)
	}
	if got.UpdateBy != 3 {
		t.Errorf("更新者应记录为 3，实际 %d", got.UpdateBy)
	}
}

func TestAgreementServiceReadsAndDelete(t *testing.T) {
	newServiceDB(t)
	svc := NewAgreementService()

	if err := svc.Create("隐私政策", "<p>p</p>", "privacy", 1, 1, 1, 1); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if err := svc.Create("别的租户", "<p>q</p>", "privacy", 1, 1, 1, 2); err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	a, err := svc.FindByType(1, "privacy")
	if err != nil {
		t.Fatalf("按类型查询失败: %v", err)
	}
	if a.Title != "隐私政策" {
		t.Errorf("应取到本租户的协议，实际 %q", a.Title)
	}

	// 不存在的类型：仓储返回 ErrRecordNotFound，Service 原样透出
	if _, err := svc.FindByType(1, "not-exist"); err == nil {
		t.Error("不存在的类型应报错")
	}

	list, total, err := svc.FindList(1, "", "", nil, 1, 10)
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Errorf("应只看到本租户 1 条，实际 total=%d", total)
	}

	got, err := svc.FindByID(1, a.ID)
	if err != nil {
		t.Fatalf("按 ID 查询失败: %v", err)
	}
	if got.Title != "隐私政策" {
		t.Errorf("内容不符: %+v", got)
	}
	_, err = svc.FindByID(1, 99999)
	assertBizError(t, err, common.CodeNotFound)

	if err := svc.Delete(1, a.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	// 跨租户删除：仓储按 tenant 过滤 → 命中 0 行 → ErrRecordNotFound。
	// 关键是「删不到」必须体现为失败，而不是静默返回成功。
	other, err := svc.FindByType(2, "privacy")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if err := svc.Delete(1, other.ID); err == nil {
		t.Error("跨租户删除必须报错，不能静默成功")
	}
	if _, err := svc.FindByType(2, "privacy"); err != nil {
		t.Errorf("跨租户删除不应影响别人的数据: %v", err)
	}
}

// ---- 岗位 ----

func TestPostServiceCRUD(t *testing.T) {
	db := newServiceDB(t)
	svc := NewPostService()

	if err := svc.Create(1, "开发", "dev", 1, 1, 9); err != nil {
		t.Fatalf("创建岗位失败: %v", err)
	}
	// 编码在同一租户内唯一
	assertBizError(t, svc.Create(1, "开发2", "dev", 1, 1, 9), common.CodeBadRequest)
	// 不同租户可以重名（隔离靠 tenant_id 而非全局唯一）
	if err := svc.Create(2, "开发", "dev", 1, 1, 9); err != nil {
		t.Errorf("不同租户应可用相同编码: %v", err)
	}

	all, err := svc.FindAll(1)
	if err != nil {
		t.Fatalf("查询全部失败: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("应只看到本租户 1 个岗位，实际 %d", len(all))
	}
	post := all[0]

	raw, err := svc.FindByID(1, post.ID)
	if err != nil {
		t.Fatalf("按 ID 查询失败: %v", err)
	}
	if p := raw.(*model.SysPost); p.TenantID != 1 || p.CreateBy != 9 {
		t.Errorf("岗位归属不符: %+v", p)
	}
	// 跨租户查不到
	_, err = svc.FindByID(2, post.ID)
	assertBizError(t, err, common.CodeNotFound)

	ids, err := svc.FindByIDs(1, []uint{post.ID})
	if err != nil || len(ids) != 1 {
		t.Errorf("FindByIDs 应返回本租户岗位，实际 %v（err=%v）", ids, err)
	}
	if got, err := svc.FindByIDs(2, []uint{post.ID}); err != nil || len(got) != 0 {
		t.Errorf("跨租户 FindByIDs 应为空，实际 %v（err=%v）", got, err)
	}

	// 更新：改编码撞唯一索引要转成业务错误
	if err := svc.Create(1, "测试", "qa", 2, 1, 9); err != nil {
		t.Fatalf("创建岗位失败: %v", err)
	}
	qa, err := svc.FindAll(1)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	var qaID uint
	for _, p := range qa {
		if p.Code == "qa" {
			qaID = p.ID
		}
	}
	assertBizError(t, svc.Update(1, qaID, "测试", "dev", 2, 1, 9), common.CodeBadRequest)

	if err := svc.Update(1, qaID, "质量", "qa", 3, 1, 9); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	var fresh model.SysPost
	if err := db.First(&fresh, qaID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if fresh.Name != "质量" || fresh.Sort != 3 || fresh.UpdateBy != 9 {
		t.Errorf("更新结果不符: %+v", fresh)
	}

	// 改不存在的岗位 → 404
	_, err = svc.FindByID(1, 99999)
	assertBizError(t, err, common.CodeNotFound)

	if err := svc.UpdateStatus(1, qaID, 0); err != nil {
		t.Fatalf("改状态失败: %v", err)
	}
	if err := db.First(&fresh, qaID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if fresh.Status != 0 {
		t.Errorf("状态应被更新为 0，实际 %d", fresh.Status)
	}
	assertBizError(t, svc.UpdateStatus(1, 99999, 0), common.CodeNotFound)

	list, total, err := svc.FindList(1, "", nil, 1, 10)
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Errorf("应看到本租户 2 个岗位，实际 total=%d", total)
	}

	if err := svc.Delete(1, qaID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	// 跨租户删除：仓储返回 ErrRecordNotFound → 404
	otherTenantPost := all[0]
	assertBizError(t, svc.Delete(2, otherTenantPost.ID), common.CodeNotFound)
}

// ---- 菜单 ----

func TestMenuServiceCRUD(t *testing.T) {
	db := newServiceDB(t)
	svc := NewMenuService()

	// 挂到不存在的父级必须被拒：否则 INSERT 成功但树遍历不到，
	// 表现为「提示创建成功，列表里却找不到」
	assertBizError(t, svc.Create(&dto.CreateMenuRequest{
		Name: "孤儿", ParentID: 999, Type: 1, Status: 1,
	}, 1), common.CodeBadRequest)

	if err := svc.Create(&dto.CreateMenuRequest{
		Name: "system", Title: "系统管理", Type: 0, Sort: 1, Status: 1,
	}, 1); err != nil {
		t.Fatalf("创建根菜单失败: %v", err)
	}
	var root model.SysMenu
	if err := db.Where("name = ?", "system").First(&root).Error; err != nil {
		t.Fatalf("回查根菜单失败: %v", err)
	}
	if root.CreateBy != 1 {
		t.Errorf("创建者应记录为 1，实际 %d", root.CreateBy)
	}

	if err := svc.Create(&dto.CreateMenuRequest{
		Name: "user", Title: "用户管理", ParentID: root.ID, Type: 1, Status: 1,
	}, 1); err != nil {
		t.Fatalf("创建子菜单失败: %v", err)
	}
	var child model.SysMenu
	if err := db.Where("name = ?", "user").First(&child).Error; err != nil {
		t.Fatalf("回查子菜单失败: %v", err)
	}

	tree, err := svc.FindTree()
	if err != nil {
		t.Fatalf("取树失败: %v", err)
	}
	if len(tree) != 1 || len(tree[0].Children) != 1 {
		t.Fatalf("树结构不符: 根 %d 个，子 %d 个", len(tree), len(tree[0].Children))
	}

	manage, err := svc.FindTreeForManage()
	if err != nil {
		t.Fatalf("取管理树失败: %v", err)
	}
	if len(manage) != 1 {
		t.Errorf("管理树应有 1 个根，实际 %d", len(manage))
	}

	all, err := svc.FindAll()
	if err != nil {
		t.Fatalf("查询全部失败: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("应有 2 个菜单，实际 %d", len(all))
	}

	raw, err := svc.FindByID(child.ID)
	if err != nil {
		t.Fatalf("按 ID 查询失败: %v", err)
	}
	if raw.(*model.SysMenu).Title != "用户管理" {
		t.Errorf("内容不符: %+v", raw)
	}
	_, err = svc.FindByID(99999)
	assertBizError(t, err, common.CodeNotFound)

	// 存在子菜单时不允许删除：直接删父节点会让子菜单 parent_id 悬空，
	// 整棵子树从界面上消失却仍在库里，既看不见也删不掉
	assertBizError(t, svc.Delete(root.ID), common.CodeBadRequest)

	// 不能把菜单挂到自己的下级之下（成环后子树不可达）
	cycleParent := child.ID
	assertBizError(t, svc.Update(&dto.UpdateMenuRequest{ID: root.ID, ParentID: &cycleParent}, 1),
		common.CodeBadRequest)

	assertBizError(t, svc.Update(&dto.UpdateMenuRequest{ID: 99999, Name: "不存在"}, 1), common.CodeNotFound)

	if err := svc.Update(&dto.UpdateMenuRequest{ID: child.ID, Title: "用户管理（改）"}, 1); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	var fresh model.SysMenu
	if err := db.First(&fresh, child.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if fresh.Title != "用户管理（改）" {
		t.Errorf("标题应被更新，实际 %q", fresh.Title)
	}

	// 未分配角色时不应报错，返回空
	menus, err := svc.FindMenusByRoleIDs(nil)
	if err != nil || len(menus) != 0 {
		t.Errorf("空角色列表应返回空，实际 %v（err=%v）", menus, err)
	}

	if err := svc.Delete(child.ID); err != nil {
		t.Fatalf("删除子菜单失败: %v", err)
	}
	if err := svc.Delete(root.ID); err != nil {
		t.Fatalf("子菜单删完后应可删父菜单: %v", err)
	}
}

// ---- 字典 ----

func TestDictServiceReadsAndUpdateType(t *testing.T) {
	db := newServiceDB(t)
	svc := NewDictService()

	if err := svc.CreateType(&dto.CreateDictTypeRequest{Name: "用户状态", Type: "sys_user_status"}, 1); err != nil {
		t.Fatalf("创建字典类型失败: %v", err)
	}
	var dt model.SysDictType
	if err := db.Where("type = ?", "sys_user_status").First(&dt).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}

	list, total, err := svc.FindTypeList("用户", 1, 10)
	if err != nil {
		t.Fatalf("类型列表失败: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Errorf("应查到 1 个类型，实际 total=%d", total)
	}

	raw, err := svc.FindTypeByID(dt.ID)
	if err != nil {
		t.Fatalf("按 ID 查询失败: %v", err)
	}
	if raw.(*model.SysDictType).Type != "sys_user_status" {
		t.Errorf("内容不符: %+v", raw)
	}
	_, err = svc.FindTypeByID(99999)
	assertBizError(t, err, common.CodeNotFound)

	// 状态指针为 nil 表示本次不改状态，必须保留原值
	if err := svc.UpdateType(dt.ID, &dto.UpdateDictTypeRequest{Name: "用户状态（改）", Remark: "备注"}, 9); err != nil {
		t.Fatalf("更新类型失败: %v", err)
	}
	var fresh model.SysDictType
	if err := db.First(&fresh, dt.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if fresh.Name != "用户状态（改）" || fresh.Remark != "备注" || fresh.UpdateBy != 9 {
		t.Errorf("更新结果不符: %+v", fresh)
	}
	if fresh.Status != 1 {
		t.Errorf("未提供 status 时应保留原值 1，实际 %d", fresh.Status)
	}

	off := int8(0)
	if err := svc.UpdateType(dt.ID, &dto.UpdateDictTypeRequest{Name: "用户状态", Status: &off}, 9); err != nil {
		t.Fatalf("更新状态失败: %v", err)
	}
	if err := db.First(&fresh, dt.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if fresh.Status != 0 {
		t.Errorf("显式传 status 应生效，实际 %d", fresh.Status)
	}

	assertBizError(t, svc.UpdateType(99999, &dto.UpdateDictTypeRequest{Name: "x"}, 9), common.CodeNotFound)

	// 字典数据
	if err := svc.CreateData(&dto.CreateDictDataRequest{
		DictType: "sys_user_status", Label: "正常", Value: "1", Sort: 1,
	}, 1); err != nil {
		t.Fatalf("创建字典数据失败: %v", err)
	}
	data, err := svc.FindDataByType("sys_user_status")
	if err != nil {
		t.Fatalf("按类型取数据失败: %v", err)
	}
	if len(data) != 1 || data[0].Label != "正常" {
		t.Errorf("按类型取数据不符: %+v", data)
	}

	dlist, dtotal, err := svc.FindDataList("sys_user_status", 1, 10)
	if err != nil {
		t.Fatalf("数据列表失败: %v", err)
	}
	if dtotal != 1 || len(dlist) != 1 {
		t.Errorf("应查到 1 条数据，实际 total=%d", dtotal)
	}
}

// ---- 角色 ----

func TestRoleServiceReadsAndDelete(t *testing.T) {
	db := newServiceDB(t)
	svc := NewRoleService()

	mine := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: 1},
		Name:            "编辑", Code: "editor", Sort: 1, Status: 1,
	}
	foreign := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: 2},
		Name:            "别的", Code: "other", Status: 1,
	}
	for _, r := range []*model.SysRole{mine, foreign} {
		if err := db.Create(r).Error; err != nil {
			t.Fatalf("准备角色失败: %v", err)
		}
	}

	raw, err := svc.FindByID(1, mine.ID)
	if err != nil {
		t.Fatalf("按 ID 查询失败: %v", err)
	}
	if raw.(*model.SysRole).Code != "editor" {
		t.Errorf("内容不符: %+v", raw)
	}
	_, err = svc.FindByID(1, 99999)
	assertBizError(t, err, common.CodeNotFound)
	_, err = svc.FindByID(2, mine.ID)
	assertBizError(t, err, common.CodeNotFound)

	// 分页字段是内嵌结构体的提升字段，复合字面量里写不了，只能逐个赋值
	roleReq := &dto.RoleListRequest{}
	roleReq.Page, roleReq.PageSize = 1, 10
	list, total, err := svc.FindList(1, roleReq)
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Errorf("应只看到本租户 1 个角色，实际 total=%d", total)
	}

	all, err := svc.FindAll(1)
	if err != nil {
		t.Fatalf("查询全部失败: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("应只看到本租户 1 个角色，实际 %d", len(all))
	}

	if err := svc.UpdateStatus(1, &dto.StatusRequest{ID: mine.ID, Status: 0}); err != nil {
		t.Fatalf("改状态失败: %v", err)
	}
	var fresh model.SysRole
	if err := db.First(&fresh, mine.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if fresh.Status != 0 {
		t.Errorf("状态应被更新为 0，实际 %d", fresh.Status)
	}

	if err := svc.Delete(1, mine.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	var left int64
	if err := db.Model(&model.SysRole{}).Where("tenant_id = ?", 1).Count(&left).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if left != 0 {
		t.Errorf("删除后本租户不应还有角色，实际 %d", left)
	}
	// 别人的角色必须还在
	var foreignLeft int64
	if err := db.Model(&model.SysRole{}).Where("tenant_id = ?", 2).Count(&foreignLeft).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if foreignLeft != 1 {
		t.Errorf("不应影响其他租户的角色，实际剩 %d", foreignLeft)
	}
}
