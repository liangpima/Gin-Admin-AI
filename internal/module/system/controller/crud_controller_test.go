package controller

import (
	"net/http"
	"strings"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"

	"github.com/gin-gonic/gin"
)

// 其余系统控制器的「写路径」补测。
//
// 此前这一批的 Update / Delete 方法基本都是 0%：读接口有覆盖，
// 写接口没有 —— 而写接口恰恰是租户透传（tenantID 漏传 → 静默全表操作）
// 与错误语义（404 被写成 500）出问题的地方。
//
// ⚠️ testsupport.NewDB 会改写包级 database.DB，因此不能 t.Parallel。

// seedCtrlMenuRow 直接落一条菜单（菜单是全局表，不带租户）。
func seedCtrlMenuRow(t *testing.T, name string, parentID uint, typ int8, status int8) *model.SysMenu {
	t.Helper()
	m := &model.SysMenu{Name: name, Title: name, ParentID: parentID, Type: typ, Status: status}
	if err := testDB(t).Create(m).Error; err != nil {
		t.Fatalf("准备菜单失败: %v", err)
	}
	return m
}

func TestRoleControllerReadEndpoints(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewRoleController()

	mine := seedCtrlRole(t, ctrlTenant, "editor")
	seedCtrlRole(t, ctrlOtherTenant, "foreign")

	// 详情
	c, w := newCtx(http.MethodGet, "/api/v1/system/role/1", ctrlTenant, 1)
	withIDParam(c, mine.ID)
	ctl.FindByID(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("详情应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 详情：非法 ID → 400
	c, w = newCtx(http.MethodGet, "/api/v1/system/role/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.FindByID(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}

	// 详情：跨租户 → 404
	c, w = newCtx(http.MethodGet, "/api/v1/system/role/1", ctrlOtherTenant, 1)
	withIDParam(c, mine.ID)
	ctl.FindByID(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("跨租户应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 列表：按租户隔离
	c, w = newCtx(http.MethodGet, "/api/v1/system/role/list?page=1&pageSize=10", ctrlTenant, 1)
	ctl.FindList(c)
	resp := decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("列表应成功，实际 %v", resp["code"])
	}
	if data := resp["data"].(map[string]interface{}); data["total"].(float64) != 1 {
		t.Errorf("应只看到本租户 1 个角色，实际 %v", data["total"])
	}

	// 下拉用全量列表
	c, w = newCtx(http.MethodGet, "/api/v1/system/role/all", ctrlTenant, 1)
	ctl.FindAll(c)
	resp = decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("全量列表应成功，实际 %v", resp["code"])
	}
	if list, ok := resp["data"].([]interface{}); !ok || len(list) != 1 {
		t.Errorf("应返回本租户 1 个角色，实际 %v", resp["data"])
	}

	// 改状态
	c, w = newCtx(http.MethodPut, "/api/v1/system/role/status", ctrlTenant, 1)
	withJSONBody(c, `{"id":`+idStr(mine.ID)+`,"status":0}`)
	ctl.UpdateStatus(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("改状态应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	var fresh model.SysRole
	if err := testDB(t).First(&fresh, mine.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if fresh.Status != 0 {
		t.Errorf("状态应为 0，实际 %d", fresh.Status)
	}
}

func TestDictControllerTypeAndDataWrites(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewDictController()

	seedDictType(t, "sys_user_status")
	var dt model.SysDictType
	if err := testDB(t).Where("type = ?", "sys_user_status").First(&dt).Error; err != nil {
		t.Fatalf("回查字典类型失败: %v", err)
	}

	// 更新类型
	c, w := newCtx(http.MethodPut, "/api/v1/system/dict/type/1", ctrlTenant, 1)
	withIDParam(c, dt.ID)
	withJSONBody(c, `{"name":"用户状态（改）","remark":"备注"}`)
	ctl.UpdateType(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("更新类型应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	if err := testDB(t).First(&dt, dt.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if dt.Name != "用户状态（改）" {
		t.Errorf("类型名称未更新，实际 %q", dt.Name)
	}

	// 更新类型：不存在的 ID → 404
	c, w = newCtx(http.MethodPut, "/api/v1/system/dict/type/1", ctrlTenant, 1)
	withIDParam(c, 99999)
	withJSONBody(c, `{"name":"x"}`)
	ctl.UpdateType(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 更新类型：非法 ID → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/dict/type/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	withJSONBody(c, `{"name":"x"}`)
	ctl.UpdateType(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}

	// 删除类型：有子级数据时必须拒绝（否则数据变成取不到的孤儿）
	seedDictData(t, "sys_user_status", "正常", "1", 1, 1)
	c, w = newCtx(http.MethodDelete, "/api/v1/system/dict/type/1", ctrlTenant, 1)
	withIDParam(c, dt.ID)
	ctl.DeleteType(c)
	if resp := decodeResp(t, w); resp["code"].(float64) == 0 {
		t.Error("存在字典数据时必须拒绝删除类型")
	}

	// 字典数据：更新
	var dd model.SysDictData
	if err := testDB(t).Where("value = ?", "1").First(&dd).Error; err != nil {
		t.Fatalf("回查字典数据失败: %v", err)
	}
	c, w = newCtx(http.MethodPut, "/api/v1/system/dict/data/1", ctrlTenant, 1)
	withIDParam(c, dd.ID)
	withJSONBody(c, `{"label":"启用","value":"1","sort":2}`)
	ctl.UpdateData(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("更新数据应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	if err := testDB(t).First(&dd, dd.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if dd.Label != "启用" || dd.Sort != 2 {
		t.Errorf("数据未更新: %+v", dd)
	}

	// 字典数据：更新不存在的 ID → 404
	c, w = newCtx(http.MethodPut, "/api/v1/system/dict/data/1", ctrlTenant, 1)
	withIDParam(c, 99999)
	withJSONBody(c, `{"label":"x","value":"y"}`)
	ctl.UpdateData(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 字典数据：删除
	c, w = newCtx(http.MethodDelete, "/api/v1/system/dict/data/1", ctrlTenant, 1)
	withIDParam(c, dd.ID)
	ctl.DeleteData(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("删除数据应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	var left int64
	if err := testDB(t).Model(&model.SysDictData{}).Count(&left).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if left != 0 {
		t.Errorf("删除后不应还有字典数据，实际 %d", left)
	}

	// 数据删完后类型可删
	c, w = newCtx(http.MethodDelete, "/api/v1/system/dict/type/1", ctrlTenant, 1)
	withIDParam(c, dt.ID)
	ctl.DeleteType(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("无子数据时应可删类型，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 删除不存在的类型 → 404
	c, w = newCtx(http.MethodDelete, "/api/v1/system/dict/type/1", ctrlTenant, 1)
	withIDParam(c, 99999)
	ctl.DeleteType(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}
}

func TestConfigControllerUpdateAndDelete(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewConfigController()

	seedConfig(t, "site.name", "旧名")
	var cfg model.SysConfig
	if err := testDB(t).Where("config_key = ?", "site.name").First(&cfg).Error; err != nil {
		t.Fatalf("回查配置失败: %v", err)
	}

	// 更新
	c, w := newCtx(http.MethodPut, "/api/v1/system/config", ctrlTenant, 5)
	withJSONBody(c, `{"id":`+idStr(cfg.ID)+`,"name":"站点名称","key":"site.name","value":"新名","type":1}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("更新应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	if err := testDB(t).First(&cfg, cfg.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if cfg.Value != "新名" || cfg.Name != "站点名称" {
		t.Errorf("配置未更新: %+v", cfg)
	}
	if cfg.UpdateBy != 5 {
		t.Errorf("操作人应记录为 5，实际 %d", cfg.UpdateBy)
	}

	// 更新：参数缺失 → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/config", ctrlTenant, 5)
	withJSONBody(c, `{"id":`+idStr(cfg.ID)+`}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("缺字段应返回 400，实际 %v", resp["code"])
	}

	// 更新：不存在的 ID → 404
	c, w = newCtx(http.MethodPut, "/api/v1/system/config", ctrlTenant, 5)
	withJSONBody(c, `{"id":99999,"name":"x","key":"y"}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 删除
	c, w = newCtx(http.MethodDelete, "/api/v1/system/config/1", ctrlTenant, 5)
	withIDParam(c, cfg.ID)
	ctl.Delete(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("删除应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	var left int64
	if err := testDB(t).Model(&model.SysConfig{}).Count(&left).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if left != 0 {
		t.Errorf("删除后不应还有配置，实际 %d", left)
	}

	// 删除：非法 ID → 400
	c, w = newCtx(http.MethodDelete, "/api/v1/system/config/x", ctrlTenant, 5)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.Delete(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}
}

func TestAgreementControllerUpdate(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewAgreementController()

	mine := seedAgreementRow(t, ctrlTenant, "用户协议", "user")
	foreign := seedAgreementRow(t, ctrlOtherTenant, "别人的协议", "user")

	// 更新：内容必须净化后落库
	c, w := newCtx(http.MethodPut, "/api/v1/system/agreement", ctrlTenant, 3)
	withJSONBody(c, `{"id":`+idStr(mine.ID)+`,"title":"用户协议（改）","content":"<p>正文</p><script>alert(1)</script>","type":"user","status":1}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("更新应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	var got model.SysAgreement
	if err := testDB(t).First(&got, mine.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.Title != "用户协议（改）" {
		t.Errorf("标题未更新，实际 %q", got.Title)
	}
	if got.UpdateBy != 3 {
		t.Errorf("更新者应记录为 3，实际 %d", got.UpdateBy)
	}
	if containsIgnoreCase(got.Content, "<script") {
		t.Errorf("更新路径必须净化富文本，实际 %q", got.Content)
	}

	// 更新：参数缺失 → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/agreement", ctrlTenant, 3)
	withJSONBody(c, `{"id":`+idStr(mine.ID)+`}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("缺字段应返回 400，实际 %v", resp["code"])
	}

	// 更新：跨租户 → 404，且别人的数据不能被改动
	c, w = newCtx(http.MethodPut, "/api/v1/system/agreement", ctrlTenant, 3)
	withJSONBody(c, `{"id":`+idStr(foreign.ID)+`,"title":"被改了","content":"x","type":"user"}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("跨租户应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}
	var still model.SysAgreement
	if err := testDB(t).First(&still, foreign.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if still.Title != "别人的协议" {
		t.Errorf("跨租户更新不应生效，实际 %q", still.Title)
	}
}

func TestDeptControllerUpdate(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewDeptController()

	mine := seedCtrlDeptRow(t, ctrlTenant, "研发部")
	foreign := seedCtrlDeptRow(t, ctrlOtherTenant, "别人的部门")

	// 更新
	c, w := newCtx(http.MethodPut, "/api/v1/system/dept", ctrlTenant, 7)
	withJSONBody(c, `{"id":`+idStr(mine.ID)+`,"name":"研发中心","leader":"张三"}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("更新应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	var got model.SysDept
	if err := testDB(t).First(&got, mine.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.Name != "研发中心" || got.Leader != "张三" || got.UpdateBy != 7 {
		t.Errorf("部门未正确更新: %+v", got)
	}

	// 更新：不存在的 ID → 404
	c, w = newCtx(http.MethodPut, "/api/v1/system/dept", ctrlTenant, 7)
	withJSONBody(c, `{"id":99999,"name":"x"}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 更新：挂到别的租户的部门之下 → 拒绝（父节点在本租户树里不可达）
	c, w = newCtx(http.MethodPut, "/api/v1/system/dept", ctrlTenant, 7)
	withJSONBody(c, `{"id":`+idStr(mine.ID)+`,"parentId":`+idStr(foreign.ID)+`}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) == 0 {
		t.Error("跨租户的父部门必须被拒绝")
	}
	if err := testDB(t).First(&got, mine.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.ParentID != 0 {
		t.Errorf("被拒绝的移动不应生效，实际 parentId=%d", got.ParentID)
	}
}

func TestPostControllerUpdate(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewPostController()

	mine := seedPost(t, ctrlTenant, "dev", "开发")

	// 更新
	c, w := newCtx(http.MethodPut, "/api/v1/system/post", ctrlTenant, 5)
	withJSONBody(c, `{"id":`+idStr(mine.ID)+`,"code":"dev","name":"开发工程师","sort":3,"status":1}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("更新应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	var got model.SysPost
	if err := testDB(t).First(&got, mine.ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.Name != "开发工程师" || got.Sort != 3 || got.UpdateBy != 5 {
		t.Errorf("岗位未正确更新: %+v", got)
	}

	// 更新：参数缺失 → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/post", ctrlTenant, 5)
	withJSONBody(c, `{"id":`+idStr(mine.ID)+`}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("缺字段应返回 400，实际 %v", resp["code"])
	}

	// 更新：不存在的 ID → 404
	c, w = newCtx(http.MethodPut, "/api/v1/system/post", ctrlTenant, 5)
	withJSONBody(c, `{"id":99999,"code":"x","name":"y"}`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}
}

func TestMenuControllerDelete(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewMenuController()

	parent := seedCtrlMenuRow(t, "system", 0, 0, 1)
	child := seedCtrlMenuRow(t, "user", parent.ID, 1, 1)

	// 有子菜单时拒绝删除
	c, w := newCtx(http.MethodDelete, "/api/v1/system/menu/1", ctrlTenant, 1)
	withIDParam(c, parent.ID)
	ctl.Delete(c)
	if resp := decodeResp(t, w); resp["code"].(float64) == 0 {
		t.Error("存在下级菜单时必须拒绝删除")
	}
	var still model.SysMenu
	if err := testDB(t).First(&still, parent.ID).Error; err != nil {
		t.Errorf("被拒绝的删除不应生效: %v", err)
	}

	// 非法 ID → 400
	c, w = newCtx(http.MethodDelete, "/api/v1/system/menu/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.Delete(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}

	// 叶子菜单可删
	c, w = newCtx(http.MethodDelete, "/api/v1/system/menu/1", ctrlTenant, 1)
	withIDParam(c, child.ID)
	ctl.Delete(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("叶子菜单应可删除，实际 %v（%v）", resp["code"], resp["message"])
	}
	var left int64
	if err := testDB(t).Model(&model.SysMenu{}).Where("id = ?", child.ID).Count(&left).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if left != 0 {
		t.Errorf("子菜单应被删除，实际剩 %d", left)
	}
}

func TestFileControllerFindByID(t *testing.T) {
	ctl := newFileControllerWithDB(t)
	mine := seedFile(t, ctl, ctrlTenant, "mine.txt")
	foreign := seedFile(t, ctl, ctrlOtherTenant, "others.txt")

	// 详情
	c, w := newCtx(http.MethodGet, "/api/v1/system/file/1", ctrlTenant, 1)
	withIDParam(c, mine.ID)
	ctl.FindByID(c)
	resp := decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("详情应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	data := resp["data"].(map[string]interface{})
	if data["name"] != "mine.txt" {
		t.Errorf("应返回本租户的文件，实际 %v", data["name"])
	}

	// 非法 ID → 400
	c, w = newCtx(http.MethodGet, "/api/v1/system/file/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.FindByID(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}

	// 跨租户 → 404
	c, w = newCtx(http.MethodGet, "/api/v1/system/file/1", ctrlTenant, 1)
	withIDParam(c, foreign.ID)
	ctl.FindByID(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("跨租户应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}
}

// containsIgnoreCase 大小写无关的子串判断（净化结果里标签大小写不保证）。
func containsIgnoreCase(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
