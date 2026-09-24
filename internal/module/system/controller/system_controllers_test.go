package controller

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"

	"github.com/gin-gonic/gin"
)

// 配置 / 字典 / 岗位 / 仪表盘 / 协议控制器的测试（P2-1 第三批）。
//
// Controller 层此前只测了 file / dept / user / role 四个（约 12 个控制器里的一半）。
// 这一批把剩下的「有真实逻辑」的控制器补上，重点是三类容易出错的地方：
//   1. **租户透传**：post / agreement / dashboard 都是租户内数据，漏传会静默全表
//   2. **公开接口的边界**：SiteInfo 不需要登录，不能因为拿不到租户上下文就报错
//   3. **敏感数据的读法**：config 的敏感项必须打码后才返回
//
// ⚠️ testsupport.NewDB 会改写包级 database.DB，因此不能 t.Parallel。

func newSystemControllersDB(t *testing.T) {
	t.Helper()
	testsupport.NewDB(t,
		&model.SysConfig{}, &model.SysDictType{}, &model.SysDictData{},
		&model.SysPost{}, &model.SysUser{}, &model.SysRole{}, &model.SysMenu{},
		&model.SysDept{}, &model.SysAgreement{},
		&model.SysOperationLog{}, &model.SysLoginLog{},
	)
}

// ---- 配置 ----

// TestConfigControllerCreateAndFindList 新增配置 + 分页列表。
func TestConfigControllerCreateAndFindList(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewConfigController()

	c, w := newCtx(http.MethodPost, "/api/v1/system/config", ctrlTenant, 1)
	withJSONBody(c, `{"name":"站点名称","key":"site.name","value":"Gin-Admin","type":0}`)
	ctl.Create(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("新增应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 重复 key 必须被拒（config_key 上有唯一索引，走到 INSERT 才报错就是 500）
	c2, w2 := newCtx(http.MethodPost, "/api/v1/system/config", ctrlTenant, 1)
	withJSONBody(c2, `{"name":"重复","key":"site.name","value":"x","type":0}`)
	ctl.Create(c2)
	resp := decodeResp(t, w2)
	if resp["code"].(float64) == 0 {
		t.Error("重复的配置键必须被拒绝")
	}
	if resp["code"].(float64) == float64(common.CodeInternalError) {
		t.Errorf("重复键属业务错误，不该是 500：%v", resp["message"])
	}

	c3, w3 := newCtx(http.MethodGet, "/api/v1/system/config/list?page=1&pageSize=10", ctrlTenant, 1)
	ctl.FindList(c3)
	if resp := decodeResp(t, w3); resp["code"].(float64) != 0 {
		t.Fatalf("列表应成功，实际 %v", resp["code"])
	}
}

// TestConfigControllerSiteInfoIsPublicAndMapsPrefix 公开的站点信息接口。
//
// 它是**公开路由**（无需登录），因此上下文里没有租户/用户 ——
// 实现不能依赖这两者，否则公开页面会直接 500。
// 同时它只返回 site. 前缀的项：把 pay.* / oss.* 一起吐出去等于泄漏凭据。
func TestConfigControllerSiteInfoIsPublicAndMapsPrefix(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewConfigController()

	seedConfig(t, "site.name", "站点名")
	seedConfig(t, "site.logo", "/uploads/logo.png")
	seedConfig(t, "pay.wechat_key", "SENSITIVE-SECRET")
	seedConfig(t, "oss.secret_key", "SENSITIVE-OSS")

	// 无租户、无用户上下文（模拟公开请求）
	c, w := newCtx(http.MethodGet, "/api/v1/site/info", 0, 0)
	ctl.SiteInfo(c)

	if w.Code != http.StatusOK {
		t.Fatalf("公开接口不应因缺少上下文而失败，实际 %d（%s）", w.Code, w.Body.String())
	}
	resp := decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data 应为对象，实际 %T", resp["data"])
	}
	if data["site.name"] != "站点名" {
		t.Errorf("应返回 site. 前缀的配置，实际 %v", data)
	}
	for _, leaked := range []string{"pay.wechat_key", "oss.secret_key"} {
		if _, exists := data[leaked]; exists {
			t.Errorf("公开接口不得返回 %s（泄漏平台凭据）", leaked)
		}
	}
}

// TestConfigControllerFindByPrefixRequiresPrefix 缺 prefix 参数应 400 而不是返回全部。
//
// 返回全部会把 pay.* / oss.* 的密钥一起带出去。
func TestConfigControllerFindByPrefixRequiresPrefix(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewConfigController()
	seedConfig(t, "pay.wechat_key", "SECRET")

	c, w := newCtx(http.MethodGet, "/api/v1/system/config/prefix", ctrlTenant, 1)
	ctl.FindByPrefix(c)

	resp := decodeResp(t, w)
	if resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("缺 prefix 应返回 400，实际 %v（%v）", resp["code"], resp["message"])
	}
}

// TestConfigControllerSensitiveValuesAreMasked 敏感项的值必须打码后才返回。
func TestConfigControllerSensitiveValuesAreMasked(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewConfigController()
	seedConfig(t, "pay.wechat_key", "REAL-SECRET-KEY")
	seedConfig(t, "site.name", "站点名")

	c, w := newCtx(http.MethodGet, "/api/v1/system/config/prefix?prefix=pay.", ctrlTenant, 1)
	ctl.FindByPrefix(c)

	resp := decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("查询应成功，实际 %v", resp["code"])
	}
	if body := w.Body.String(); strings.Contains(body, "REAL-SECRET-KEY") {
		t.Errorf("敏感配置的真实值不应出现在响应里: %s", body)
	}
}

// TestConfigControllerBatchSaveUpdatesValues 批量保存按 key 更新已有项。
func TestConfigControllerBatchSaveUpdatesValues(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewConfigController()
	seedConfig(t, "site.name", "旧名字")
	seedConfig(t, "site.logo", "/old.png")

	c, w := newCtx(http.MethodPut, "/api/v1/system/config/batch", ctrlTenant, 1)
	withJSONBody(c, `{"prefix":"site.","items":[{"key":"name","value":"新名字"},{"key":"logo","value":"/new.png"}]}`)
	ctl.BatchSave(c)

	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("批量保存应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	var got model.SysConfig
	if err := testDB(t).Where("config_key = ?", "site.name").First(&got).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.Value != "新名字" {
		t.Errorf("值应被更新为「新名字」，实际 %q", got.Value)
	}
}

// TestConfigControllerUploadCertRejectsBadInput 证书上传的输入校验。
//
// 证书/私钥必须拒绝非法扩展名与超大文件；且它们**不能落在 uploads/ 下** ——
// 那个目录是匿名可读的静态服务目录，放进去等于公开商户私钥。
func TestConfigControllerUploadCertRejectsBadInput(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewConfigController()

	t.Run("没有文件", func(t *testing.T) {
		c, w := newCtx(http.MethodPost, "/api/v1/system/config/upload", ctrlTenant, 1)
		ctl.UploadCert(c)
		resp := decodeResp(t, w)
		if resp["code"].(float64) != float64(common.CodeBadRequest) {
			t.Errorf("缺文件应返回 400，实际 %v", resp["code"])
		}
	})

	t.Run("扩展名不在白名单", func(t *testing.T) {
		c, w := newCtx(http.MethodPost, "/api/v1/system/config/upload", ctrlTenant, 1)
		withMultipartFile(c, "cert.pem.exe", []byte("x"))
		ctl.UploadCert(c)
		resp := decodeResp(t, w)
		if resp["code"].(float64) != float64(common.CodeBadRequest) {
			t.Errorf("非法扩展名应返回 400，实际 %v（%v）", resp["code"], resp["message"])
		}
	})

	t.Run("超过 2MB", func(t *testing.T) {
		c, w := newCtx(http.MethodPost, "/api/v1/system/config/upload", ctrlTenant, 1)
		withMultipartFile(c, "cert.pem", make([]byte, 2*1024*1024+1))
		ctl.UploadCert(c)
		resp := decodeResp(t, w)
		if resp["code"].(float64) != float64(common.CodeBadRequest) {
			t.Errorf("超大文件应返回 400，实际 %v（%v）", resp["code"], resp["message"])
		}
	})
}

// ---- 字典 ----

// TestDictControllerTypeLifecycle 字典类型的增删查。
func TestDictControllerTypeLifecycle(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewDictController()

	c, w := newCtx(http.MethodPost, "/api/v1/system/dict/type", ctrlTenant, 1)
	withJSONBody(c, `{"name":"用户状态","type":"sys_user_status"}`)
	ctl.CreateType(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("新增类型应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 类型编码是代码里的字面量，格式非法必须在写入前拦下
	c2, w2 := newCtx(http.MethodPost, "/api/v1/system/dict/type", ctrlTenant, 1)
	withJSONBody(c2, `{"name":"非法","type":"Sys-User-Status!"}`)
	ctl.CreateType(c2)
	if resp := decodeResp(t, w2); resp["code"].(float64) == 0 {
		t.Error("非法类型编码必须被拒绝（它会被写进代码字面量）")
	}

	c3, w3 := newCtx(http.MethodGet, "/api/v1/system/dict/type/list?page=1&pageSize=10", ctrlTenant, 1)
	ctl.FindTypeList(c3)
	resp := decodeResp(t, w3)
	if resp["code"].(float64) != 0 {
		t.Fatalf("列表应成功，实际 %v", resp["code"])
	}
	data := resp["data"].(map[string]interface{})
	if data["total"].(float64) != 1 {
		t.Errorf("应只有 1 个类型（非法那条没写进去），实际 %v", data["total"])
	}
}

// TestDictControllerFindDataByTypeReturnsEnabledOnly 业务页面取选项：只返回启用项、按 sort 排序。
//
// 这条接口只要求登录态（不卡 dict:list），因为普通操作员的页面也要用它渲染下拉。
func TestDictControllerFindDataByTypeReturnsEnabledOnly(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewDictController()

	seedDictType(t, "sys_user_status")
	seedDictData(t, "sys_user_status", "停用", "0", 2, 1)
	seedDictData(t, "sys_user_status", "正常", "1", 1, 1)
	seedDictData(t, "sys_user_status", "已删除", "9", 3, 0) // 停用项

	c, w := newCtx(http.MethodGet, "/api/v1/system/dict/data/type/sys_user_status", ctrlTenant, 1)
	c.Params = paramsOf("type", "sys_user_status")
	ctl.FindDataByType(c)

	resp := decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	list := resp["data"].([]interface{})
	if len(list) != 2 {
		t.Fatalf("应只返回 2 个启用项，实际 %d", len(list))
	}
	first := list[0].(map[string]interface{})
	if first["label"] != "正常" {
		t.Errorf("应按 sort 升序，第一项应为「正常」，实际 %v", first["label"])
	}
}

// TestDictControllerDataLifecycle 字典数据的增删。
func TestDictControllerDataLifecycle(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewDictController()
	seedDictType(t, "sys_pay_channel")

	c, w := newCtx(http.MethodPost, "/api/v1/system/dict/data", ctrlTenant, 1)
	withJSONBody(c, `{"dictType":"sys_pay_channel","label":"微信支付","value":"wechat","sort":1,"listClass":"success"}`)
	ctl.CreateData(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("新增数据应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 往不存在的类型下塞数据必须被拒（否则产生取不到的孤儿数据）
	c2, w2 := newCtx(http.MethodPost, "/api/v1/system/dict/data", ctrlTenant, 1)
	withJSONBody(c2, `{"dictType":"not_exist","label":"孤儿","value":"x"}`)
	ctl.CreateData(c2)
	if resp := decodeResp(t, w2); resp["code"].(float64) == 0 {
		t.Error("字典类型不存在时必须拒绝，否则会留下取不到的孤儿数据")
	}

	c3, w3 := newCtx(http.MethodGet, "/api/v1/system/dict/data/list?page=1&pageSize=10", ctrlTenant, 1)
	ctl.FindDataList(c3)
	if resp := decodeResp(t, w3); resp["code"].(float64) != 0 {
		t.Errorf("列表应成功，实际 %v", resp["code"])
	}
}

// ---- 岗位 ----

// TestPostControllerCreateStampsTenant 新建岗位必须落在操作者租户下。
func TestPostControllerCreateStampsTenant(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewPostController()

	c, w := newCtx(http.MethodPost, "/api/v1/system/post", ctrlOtherTenant, 5)
	withJSONBody(c, `{"code":"dev","name":"开发","sort":1,"status":1}`)
	ctl.Create(c)

	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("创建应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	var got model.SysPost
	if err := testDB(t).Where("code = ?", "dev").First(&got).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.TenantID != ctrlOtherTenant {
		t.Errorf("岗位应归属租户 %d，实际 %d", ctrlOtherTenant, got.TenantID)
	}
	if got.CreateBy != 5 {
		t.Errorf("创建者应为 5，实际 %d", got.CreateBy)
	}
}

// TestPostControllerFindListIsTenantScoped 岗位列表按租户隔离。
func TestPostControllerFindListIsTenantScoped(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewPostController()

	seedPost(t, ctrlTenant, "dev-a", "开发A")
	seedPost(t, ctrlOtherTenant, "dev-b", "开发B")

	c, w := newCtx(http.MethodGet, "/api/v1/system/post/list?page=1&pageSize=10", ctrlTenant, 1)
	ctl.FindList(c)

	resp := decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("列表应成功，实际 %v", resp["code"])
	}
	data := resp["data"].(map[string]interface{})
	if data["total"].(float64) != 1 {
		t.Errorf("应只看到本租户的 1 个岗位，实际 %v", data["total"])
	}
}

// TestPostControllerDeleteCrossTenant 跨租户删不掉岗位。
func TestPostControllerDeleteCrossTenant(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewPostController()
	foreign := seedPost(t, ctrlOtherTenant, "dev-b", "开发B")

	c, w := newCtx(http.MethodDelete, "/api/v1/system/post/1", ctrlTenant, 1)
	withIDParam(c, foreign.ID)
	ctl.Delete(c)

	if resp := decodeResp(t, w); resp["code"].(float64) == 0 {
		t.Error("跨租户删除必须失败")
	}
	var still model.SysPost
	if err := testDB(t).Where("id = ?", foreign.ID).First(&still).Error; err != nil {
		t.Errorf("岗位不应被删除: %v", err)
	}
}

// ---- 仪表盘 ----

// TestDashboardControllerStatsAreTenantScoped 仪表盘统计必须按租户。
//
// 不隔离时租户 A 的首页会显示租户 B 的用户数/订单量 —— 属于跨租户信息泄漏，
// 而且从界面上看完全正常（就是个数字）。
func TestDashboardControllerStatsAreTenantScoped(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewDashboardController()

	seedCtrlUser(t, ctrlTenant, "u1")
	seedCtrlUser(t, ctrlTenant, "u2")
	seedCtrlUser(t, ctrlOtherTenant, "u3")

	c, w := newCtx(http.MethodGet, "/api/v1/dashboard/stats", ctrlTenant, 1)
	ctl.GetStats(c)

	resp := decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("统计应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	data := resp["data"].(map[string]interface{})
	if data["userCount"].(float64) != 2 {
		t.Errorf("应统计本租户的 2 个用户，实际 %v", data["userCount"])
	}
}

// ---- 协议 ----

// TestAgreementControllerTenantScoping 协议的租户归属与隔离。
func TestAgreementControllerTenantScoping(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewAgreementController()

	t.Run("创建时打上租户", func(t *testing.T) {
		c, w := newCtx(http.MethodPost, "/api/v1/system/agreement", ctrlOtherTenant, 3)
		withJSONBody(c, `{"title":"用户协议","content":"<p>正文</p>","type":"user","status":1}`)
		ctl.Create(c)

		if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
			t.Fatalf("创建应成功，实际 %v（%v）", resp["code"], resp["message"])
		}
		var got model.SysAgreement
		if err := testDB(t).Where("title = ?", "用户协议").First(&got).Error; err != nil {
			t.Fatalf("回查失败: %v", err)
		}
		if got.TenantID != ctrlOtherTenant {
			t.Errorf("协议应归属租户 %d，实际 %d", ctrlOtherTenant, got.TenantID)
		}
	})

	t.Run("按类型取协议只取本租户", func(t *testing.T) {
		seedAgreementRow(t, ctrlTenant, "A-隐私政策", "privacy")
		seedAgreementRow(t, ctrlOtherTenant, "B-隐私政策", "privacy")

		c, w := newCtx(http.MethodGet, "/api/v1/system/agreement/type/privacy", ctrlTenant, 1)
		c.Params = paramsOf("type", "privacy")
		ctl.FindByType(c)

		resp := decodeResp(t, w)
		if resp["code"].(float64) != 0 {
			t.Fatalf("应成功，实际 %v（%v）", resp["code"], resp["message"])
		}
		data := resp["data"].(map[string]interface{})
		if data["title"] != "A-隐私政策" {
			t.Errorf("应取到本租户的协议，实际 %v", data["title"])
		}
	})

	t.Run("列表按租户隔离", func(t *testing.T) {
		c, w := newCtx(http.MethodGet, "/api/v1/system/agreement/list?page=1&pageSize=10", ctrlTenant, 1)
		ctl.FindList(c)

		resp := decodeResp(t, w)
		data := resp["data"].(map[string]interface{})
		if data["total"].(float64) != 1 {
			t.Errorf("应只看到本租户的 1 条协议，实际 %v", data["total"])
		}
	})

	t.Run("跨租户删除失败", func(t *testing.T) {
		foreign := seedAgreementRow(t, ctrlOtherTenant, "B-待删", "other")

		c, w := newCtx(http.MethodDelete, "/api/v1/system/agreement/1", ctrlTenant, 1)
		withIDParam(c, foreign.ID)
		ctl.Delete(c)

		if resp := decodeResp(t, w); resp["code"].(float64) == 0 {
			t.Error("跨租户删除必须失败")
		}
	})
}

// ---- 测试辅助 ----

// withMultipartFile 把上下文改造成「带一个上传文件」的 multipart 请求。
// c.FormFile 依赖请求体的 multipart 解析，因此必须构造真实请求。
func withMultipartFile(c *gin.Context, filename string, content []byte) {
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("file", filename)
	if err == nil {
		_, _ = fw.Write(content)
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, c.Request.URL.Path, body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	c.Request = req
}

// paramsOf 构造路径参数（gin 的 c.Param 由此读取）
func paramsOf(key, value string) gin.Params {
	return gin.Params{{Key: key, Value: value}}
}

func seedConfig(t *testing.T, key, value string) {
	t.Helper()
	row := &model.SysConfig{Name: key, ConfigKey: key, Value: value, Type: 1}
	if err := testDB(t).Create(row).Error; err != nil {
		t.Fatalf("准备配置失败: %v", err)
	}
}

func seedDictType(t *testing.T, code string) {
	t.Helper()
	row := &model.SysDictType{Name: code, Type: code, Status: 1}
	if err := testDB(t).Create(row).Error; err != nil {
		t.Fatalf("准备字典类型失败: %v", err)
	}
}

func seedDictData(t *testing.T, dictType, label, value string, sort int, status int8) {
	t.Helper()
	row := &model.SysDictData{DictType: dictType, Label: label, Value: value, Sort: sort, Status: status}
	if err := testDB(t).Create(row).Error; err != nil {
		t.Fatalf("准备字典数据失败: %v", err)
	}
}

func seedPost(t *testing.T, tenantID uint, code, name string) *model.SysPost {
	t.Helper()
	p := &model.SysPost{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Code:            code,
		Name:            name,
		Status:          1,
	}
	if err := testDB(t).Create(p).Error; err != nil {
		t.Fatalf("准备岗位失败: %v", err)
	}
	return p
}

func seedAgreementRow(t *testing.T, tenantID uint, title, typ string) *model.SysAgreement {
	t.Helper()
	a := &model.SysAgreement{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Title:           title,
		Content:         "<p>" + title + "</p>",
		Type:            typ,
		Status:          1,
	}
	if err := testDB(t).Create(a).Error; err != nil {
		t.Fatalf("准备协议失败: %v", err)
	}
	return a
}

// ---- 菜单 ----

// TestMenuControllerLifecycle 菜单的增删改查（菜单是全局表，不带租户）。
func TestMenuControllerLifecycle(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewMenuController()

	t.Run("创建目录型菜单", func(t *testing.T) {
		c, w := newCtx(http.MethodPost, "/api/v1/system/menu", ctrlTenant, 1)
		withJSONBody(c, `{"name":"系统管理","title":"系统管理","type":0,"path":"/system","sort":1,"status":1}`)
		ctl.Create(c)
		if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
			t.Fatalf("创建应成功，实际 %v（%v）", resp["code"], resp["message"])
		}
	})

	// 回归：type=0（目录）是**合法取值**，不能被 required 挡住。
	// validator 的 required 判的是「不等于零值」，加在 int8 的 Type 上会让
	// 「新增目录型菜单」直接 400，报错还是看不出所以然的 validation 文案。
	t.Run("目录型菜单（type=0）必须能建", func(t *testing.T) {
		c, w := newCtx(http.MethodPost, "/api/v1/system/menu", ctrlTenant, 1)
		withJSONBody(c, `{"name":"仅目录","title":"仅目录","type":0,"path":"/dir","status":1}`)
		ctl.Create(c)

		if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
			t.Fatalf("type=0 是合法的目录类型，不应被拒绝，实际 %v（%v）", resp["code"], resp["message"])
		}
		var got model.SysMenu
		if err := testDB(t).Where("name = ?", "仅目录").First(&got).Error; err != nil {
			t.Fatalf("回查失败: %v", err)
		}
		if got.Type != 0 {
			t.Errorf("落库类型应为 0（目录），实际 %d", got.Type)
		}
	})

	t.Run("非法类型应 400", func(t *testing.T) {
		c, w := newCtx(http.MethodPost, "/api/v1/system/menu", ctrlTenant, 1)
		withJSONBody(c, `{"name":"非法类型","type":3}`)
		ctl.Create(c)
		resp := decodeResp(t, w)
		if resp["code"].(float64) != float64(common.CodeBadRequest) {
			t.Errorf("type 只允许 0/1/2，应返回 400，实际 %v", resp["code"])
		}
	})

	t.Run("查不存在的菜单返回 404", func(t *testing.T) {
		c, w := newCtx(http.MethodGet, "/api/v1/system/menu/1", ctrlTenant, 1)
		withIDParam(c, 99999)
		ctl.FindByID(c)
		resp := decodeResp(t, w)
		if resp["code"].(float64) != float64(common.CodeNotFound) {
			t.Errorf("应返回 404，实际 %v（%v）", resp["code"], resp["message"])
		}
	})

	t.Run("管理端树与下拉列表", func(t *testing.T) {
		c, w := newCtx(http.MethodGet, "/api/v1/system/menu/tree", ctrlTenant, 1)
		ctl.FindTree(c)
		if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
			t.Errorf("树应成功，实际 %v", resp["code"])
		}

		c2, w2 := newCtx(http.MethodGet, "/api/v1/system/menu/all", ctrlTenant, 1)
		ctl.FindAll(c2)
		if resp := decodeResp(t, w2); resp["code"].(float64) != 0 {
			t.Errorf("列表应成功，实际 %v", resp["code"])
		}
	})

	t.Run("更新不存在的菜单返回 404", func(t *testing.T) {
		c, w := newCtx(http.MethodPut, "/api/v1/system/menu", ctrlTenant, 1)
		withJSONBody(c, `{"id":99999,"title":"改名"}`)
		ctl.Update(c)
		resp := decodeResp(t, w)
		if resp["code"].(float64) != float64(common.CodeNotFound) {
			t.Errorf("应返回 404，实际 %v（%v）", resp["code"], resp["message"])
		}
	})
}

// ---- 日志 ----

// TestLogControllerFindOperationLogList 操作日志列表 + 关键字搜索。
func TestLogControllerFindOperationLogList(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewLogController()

	seedOperationLog(t, ctrlTenant, "用户管理")
	seedOperationLog(t, ctrlTenant, "角色管理")
	seedOperationLog(t, ctrlOtherTenant, "用户管理") // 别的租户

	c, w := newCtx(http.MethodGet, "/api/v1/system/log/operation?page=1&pageSize=10", ctrlTenant, 1)
	ctl.FindOperationLogList(c)

	resp := decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("列表应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	data := resp["data"].(map[string]interface{})
	if data["total"].(float64) != 2 {
		t.Errorf("应只看到本租户的 2 条日志，实际 %v", data["total"])
	}
}

// TestLogControllerFindLoginLogList 登录日志列表按租户过滤。
func TestLogControllerFindLoginLogList(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewLogController()

	seedLoginLog(t, ctrlTenant, "alice")
	seedLoginLog(t, ctrlOtherTenant, "bob")

	c, w := newCtx(http.MethodGet, "/api/v1/system/log/login?page=1&pageSize=10", ctrlTenant, 1)
	ctl.FindLoginLogList(c)

	resp := decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("列表应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	data := resp["data"].(map[string]interface{})
	if data["total"].(float64) != 1 {
		t.Errorf("应只看到本租户的 1 条登录日志，实际 %v", data["total"])
	}
}

// TestLogControllerClearIsTenantScoped 清空日志只能清本租户的。
//
// 不隔离时租户 A 的一次「清空」会删掉所有租户的审计记录 ——
// 既是数据破坏，也等于抹掉别人的操作痕迹。
func TestLogControllerClearIsTenantScoped(t *testing.T) {
	newSystemControllersDB(t)
	ctl := NewLogController()

	seedOperationLog(t, ctrlTenant, "本租户")
	seedOperationLog(t, ctrlOtherTenant, "别租户")
	seedLoginLog(t, ctrlTenant, "本租户")
	seedLoginLog(t, ctrlOtherTenant, "别租户")

	c, w := newCtx(http.MethodDelete, "/api/v1/system/log/operation", ctrlTenant, 1)
	ctl.ClearOperationLogs(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("清空应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	c2, w2 := newCtx(http.MethodDelete, "/api/v1/system/log/login", ctrlTenant, 1)
	ctl.ClearLoginLogs(c2)
	if resp := decodeResp(t, w2); resp["code"].(float64) != 0 {
		t.Fatalf("清空应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	var opLeft, loginLeft int64
	if err := testDB(t).Model(&model.SysOperationLog{}).Where("tenant_id = ?", ctrlOtherTenant).Count(&opLeft).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if err := testDB(t).Model(&model.SysLoginLog{}).Where("tenant_id = ?", ctrlOtherTenant).Count(&loginLeft).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if opLeft != 1 || loginLeft != 1 {
		t.Errorf("其他租户的日志不应被清除（操作日志剩 %d，登录日志剩 %d）", opLeft, loginLeft)
	}

	var opMine int64
	if err := testDB(t).Model(&model.SysOperationLog{}).Where("tenant_id = ?", ctrlTenant).Count(&opMine).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if opMine != 0 {
		t.Errorf("本租户的操作日志应被清空，实际剩 %d", opMine)
	}
}

func seedOperationLog(t *testing.T, tenantID uint, title string) {
	t.Helper()
	row := &model.SysOperationLog{
		TenantID:     tenantID,
		Title:        title,
		Action:       "查询",
		RequestURL:   "/api/v1/system/user/list",
		Status:       1,
		OperatorID:   1,
		OperatorName: "admin",
	}
	if err := testDB(t).Create(row).Error; err != nil {
		t.Fatalf("准备操作日志失败: %v", err)
	}
}

func seedLoginLog(t *testing.T, tenantID uint, username string) {
	t.Helper()
	row := &model.SysLoginLog{
		TenantID: tenantID,
		Username: username,
		IP:       "127.0.0.1",
		Status:   1,
		Msg:      "登录成功",
	}
	if err := testDB(t).Create(row).Error; err != nil {
		t.Fatalf("准备登录日志失败: %v", err)
	}
}
