package controller

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"

	"github.com/gin-gonic/gin"
)

// 各控制器的**错误路径**补测。
//
// 上一批只跑了「成功路径」，于是大量方法停在 55%~80% ——
// 缺的恰好是两条分支：
//   - 参数绑定失败 → 400（`?page=abc` 曾被当成「没传」而静默用默认分页）
//   - Service 返回业务错误 → FailWith 按其语义给 400/404，而不是笼统 500
//
// 这两条分支正是「用户看不懂的报错」的来源，值得逐条钉住。
//
// ⚠️ testsupport.NewDB 会改写包级 database.DB，因此不能 t.Parallel。

// ---- 角色 ----

func TestRoleControllerErrorPaths(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewRoleController()
	seedCtrlRole(t, ctrlTenant, "editor")

	// 参数绑定失败 → 400
	c, w := newCtx(http.MethodPost, "/api/v1/system/role", ctrlTenant, 1)
	withJSONBody(c, `{"name":`)
	ctl.Create(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 JSON 应返回 400，实际 %v", resp["code"])
	}

	// 编码重复 → 业务错误，不能落到 500
	c, w = newCtx(http.MethodPost, "/api/v1/system/role", ctrlTenant, 1)
	withJSONBody(c, `{"name":"编辑","code":"editor","status":1,"dataScope":1}`)
	ctl.Create(c)
	resp := decodeResp(t, w)
	if resp["code"].(float64) == 0 {
		t.Error("重复的角色编码必须被拒绝")
	}
	if resp["code"].(float64) == float64(common.CodeInternalError) {
		t.Errorf("重复编码是业务错误，不该是 500：%v", resp["message"])
	}

	// 改状态：绑定失败 → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/role/status", ctrlTenant, 1)
	withJSONBody(c, `{"id":`)
	ctl.UpdateStatus(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 JSON 应返回 400，实际 %v", resp["code"])
	}

	// 列表：page 不是数字 → 400（不能静默按第一页返回）
	c, w = newCtx(http.MethodGet, "/api/v1/system/role/list?page=abc", ctrlTenant, 1)
	ctl.FindList(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法分页参数应返回 400，实际 %v", resp["code"])
	}

	// 删除：非法 ID → 400
	c, w = newCtx(http.MethodDelete, "/api/v1/system/role/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.Delete(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}
}

// ---- 字典 ----

func TestDictControllerErrorPaths(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewDictController()

	seedDictType(t, "sys_user_status")

	// 类型编码重复
	c, w := newCtx(http.MethodPost, "/api/v1/system/dict/type", ctrlTenant, 1)
	withJSONBody(c, `{"name":"用户状态","type":"sys_user_status"}`)
	ctl.CreateType(c)
	if resp := decodeResp(t, w); resp["code"].(float64) == 0 {
		t.Error("重复的字典类型必须被拒绝")
	}

	// 类型列表：非法分页参数
	c, w = newCtx(http.MethodGet, "/api/v1/system/dict/type/list?pageSize=abc", ctrlTenant, 1)
	ctl.FindTypeList(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法分页参数应返回 400，实际 %v", resp["code"])
	}

	// 新增数据：缺必填字段
	c, w = newCtx(http.MethodPost, "/api/v1/system/dict/data", ctrlTenant, 1)
	withJSONBody(c, `{"dictType":"sys_user_status"}`)
	ctl.CreateData(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("缺字段应返回 400，实际 %v", resp["code"])
	}

	// 按类型取选项：类型为空 → 400
	c, w = newCtx(http.MethodGet, "/api/v1/system/dict/data/type/", ctrlTenant, 1)
	ctl.FindDataByType(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("类型为空应返回 400，实际 %v", resp["code"])
	}

	// 数据列表：非法分页参数
	c, w = newCtx(http.MethodGet, "/api/v1/system/dict/data/list?page=abc", ctrlTenant, 1)
	ctl.FindDataList(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法分页参数应返回 400，实际 %v", resp["code"])
	}

	// 更新数据：非法 ID → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/dict/data/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	withJSONBody(c, `{"label":"x","value":"y"}`)
	ctl.UpdateData(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}

	// 删除数据：非法 ID → 400
	c, w = newCtx(http.MethodDelete, "/api/v1/system/dict/data/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.DeleteData(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}

	// 删除数据：不存在的 ID → 404
	c, w = newCtx(http.MethodDelete, "/api/v1/system/dict/data/1", ctrlTenant, 1)
	withIDParam(c, 99999)
	ctl.DeleteData(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 删除类型：非法 ID → 400
	c, w = newCtx(http.MethodDelete, "/api/v1/system/dict/type/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.DeleteType(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}
}

// ---- 配置 ----

func TestConfigControllerErrorPaths(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewConfigController()
	seedConfig(t, "site.name", "值")

	// 参数绑定失败 → 400
	c, w := newCtx(http.MethodPost, "/api/v1/system/config", ctrlTenant, 1)
	withJSONBody(c, `{"name":`)
	ctl.Create(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 JSON 应返回 400，实际 %v", resp["code"])
	}

	// 列表：非法分页参数
	c, w = newCtx(http.MethodGet, "/api/v1/system/config/list?page=abc", ctrlTenant, 1)
	ctl.FindList(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法分页参数应返回 400，实际 %v", resp["code"])
	}

	// 批量保存：缺 prefix → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/config/batch", ctrlTenant, 1)
	withJSONBody(c, `{"items":[{"key":"name","value":"x"}]}`)
	ctl.BatchSave(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("缺 prefix 应返回 400，实际 %v", resp["code"])
	}

	// 批量保存：缺 items → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/config/batch", ctrlTenant, 1)
	withJSONBody(c, `{"prefix":"site."}`)
	ctl.BatchSave(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("缺 items 应返回 400，实际 %v", resp["code"])
	}

	// 前缀查询：不存在的配置返回空列表而不是报错
	c, w = newCtx(http.MethodGet, "/api/v1/system/config/prefix?prefix=nope.", ctrlTenant, 1)
	ctl.FindByPrefix(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Errorf("查不到配置应返回空列表，实际 %v（%v）", resp["code"], resp["message"])
	}
}

// TestConfigControllerUploadCertSavesOutsideUploads 合法证书的落盘位置。
//
// 证书/私钥**不能**放在 uploads/ 下：那个目录由 r.Static("/uploads") 对外
// 匿名可读，放进去等于把商户私钥公开（GET /uploads/certs/xxx.key 即可下载）。
func TestConfigControllerUploadCertSavesOutsideUploads(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewConfigController()

	c, w := newCtx(http.MethodPost, "/api/v1/system/config/upload", ctrlTenant, 1)
	withMultipartFile(c, "cert.pem", []byte("-----BEGIN CERTIFICATE-----\nMIIB\n"))
	ctl.UploadCert(c)

	resp := decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("合法证书应上传成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data 应为对象，实际 %T", resp["data"])
	}
	path, _ := data["path"].(string)
	if path == "" {
		t.Fatal("应返回保存路径")
	}
	defer func() { _ = os.Remove(path) }()

	if strings.Contains(filepath.ToSlash(path), "uploads/") {
		t.Errorf("证书不应存放在匿名可读的 uploads/ 下: %s", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("文件应真实落盘: %v", err)
	}
}

// ---- 协议 ----

func TestAgreementControllerErrorPaths(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewAgreementController()
	seedAgreementRow(t, ctrlTenant, "用户协议", "user")

	// 新增：缺必填字段 → 400
	c, w := newCtx(http.MethodPost, "/api/v1/system/agreement", ctrlTenant, 1)
	withJSONBody(c, `{"title":"只有标题"}`)
	ctl.Create(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("缺字段应返回 400，实际 %v", resp["code"])
	}

	// 删除：非法 ID → 400
	c, w = newCtx(http.MethodDelete, "/api/v1/system/agreement/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.Delete(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}

	// 列表：非法分页参数
	c, w = newCtx(http.MethodGet, "/api/v1/system/agreement/list?page=abc", ctrlTenant, 1)
	ctl.FindList(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法分页参数应返回 400，实际 %v", resp["code"])
	}

	// 按类型取：类型为空 → 400
	c, w = newCtx(http.MethodGet, "/api/v1/system/agreement/type/", ctrlTenant, 1)
	ctl.FindByType(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("类型为空应返回 400，实际 %v", resp["code"])
	}
}

// ---- 部门 / 岗位 / 菜单 ----

func TestDeptControllerErrorPaths(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewDeptController()

	// 新增：上级部门不存在 → 业务错误（否则节点挂不上树，界面上找不到）
	c, w := newCtx(http.MethodPost, "/api/v1/system/dept", ctrlTenant, 1)
	withJSONBody(c, `{"name":"孤儿部门","parentId":99999,"status":1}`)
	ctl.Create(c)
	if resp := decodeResp(t, w); resp["code"].(float64) == 0 {
		t.Error("上级部门不存在时必须拒绝")
	}

	// 删除：非法 ID → 400
	c, w = newCtx(http.MethodDelete, "/api/v1/system/dept/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.Delete(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}

	// 详情：非法 ID → 400
	c, w = newCtx(http.MethodGet, "/api/v1/system/dept/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.FindByID(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}

	// 详情：不存在的部门 → 404
	c, w = newCtx(http.MethodGet, "/api/v1/system/dept/1", ctrlTenant, 1)
	withIDParam(c, 99999)
	ctl.FindByID(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}
}

func TestPostControllerErrorPaths(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewPostController()
	seedPost(t, ctrlTenant, "dev", "开发")

	// 新增：编码重复 → 业务错误
	c, w := newCtx(http.MethodPost, "/api/v1/system/post", ctrlTenant, 1)
	withJSONBody(c, `{"code":"dev","name":"开发2","status":1}`)
	ctl.Create(c)
	resp := decodeResp(t, w)
	if resp["code"].(float64) == 0 {
		t.Error("重复的岗位编码必须被拒绝")
	}
	if resp["code"].(float64) == float64(common.CodeInternalError) {
		t.Errorf("重复编码是业务错误，不该是 500：%v", resp["message"])
	}

	// 删除：非法 ID → 400
	c, w = newCtx(http.MethodDelete, "/api/v1/system/post/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.Delete(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}

	// 列表：非法分页参数
	c, w = newCtx(http.MethodGet, "/api/v1/system/post/list?page=abc", ctrlTenant, 1)
	ctl.FindList(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法分页参数应返回 400，实际 %v", resp["code"])
	}
}

func TestMenuControllerErrorPaths(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewMenuController()

	// 新增：上级菜单不存在 → 业务错误
	c, w := newCtx(http.MethodPost, "/api/v1/system/menu", ctrlTenant, 1)
	withJSONBody(c, `{"name":"孤儿","title":"孤儿","type":1,"parentId":99999,"status":1}`)
	ctl.Create(c)
	if resp := decodeResp(t, w); resp["code"].(float64) == 0 {
		t.Error("上级菜单不存在时必须拒绝")
	}

	// 更新：绑定失败 → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/menu", ctrlTenant, 1)
	withJSONBody(c, `{"id":`)
	ctl.Update(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 JSON 应返回 400，实际 %v", resp["code"])
	}

	// 详情：非法 ID → 400
	c, w = newCtx(http.MethodGet, "/api/v1/system/menu/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.FindByID(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}
}

// ---- 日志 ----

func TestLogControllerErrorPaths(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewLogController()

	// 非法分页参数不能被当成「没传」
	c, w := newCtx(http.MethodGet, "/api/v1/system/log/operation?page=abc", ctrlTenant, 1)
	ctl.FindOperationLogList(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法分页参数应返回 400，实际 %v", resp["code"])
	}

	c, w = newCtx(http.MethodGet, "/api/v1/system/log/login?pageSize=abc", ctrlTenant, 1)
	ctl.FindLoginLogList(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法分页参数应返回 400，实际 %v", resp["code"])
	}

	// 缺少租户上下文（tenantID=0）时仓储会拒绝：不能退化成「清空全平台」
	c, w = newCtx(http.MethodDelete, "/api/v1/system/log/operation", 0, 0)
	ctl.ClearOperationLogs(c)
	if resp := decodeResp(t, w); resp["code"].(float64) == 0 {
		t.Error("缺少租户上下文时必须拒绝清空操作日志")
	}

	c, w = newCtx(http.MethodDelete, "/api/v1/system/log/login", 0, 0)
	ctl.ClearLoginLogs(c)
	if resp := decodeResp(t, w); resp["code"].(float64) == 0 {
		t.Error("缺少租户上下文时必须拒绝清空登录日志")
	}
}

// ---- 用户 ----

func TestUserControllerErrorPaths(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewUserController()
	u := seedCtrlUser(t, ctrlTenant, "alice")

	// 列表：非法分页参数
	c, w := newCtx(http.MethodGet, "/api/v1/system/user/list?page=abc", ctrlTenant, 1)
	ctl.FindList(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法分页参数应返回 400，实际 %v", resp["code"])
	}

	// 导出：非法分页参数
	c, w = newCtx(http.MethodGet, "/api/v1/system/user/export?page=abc", ctrlTenant, 1)
	ctl.Export(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法分页参数应返回 400，实际 %v", resp["code"])
	}

	// 改状态：绑定失败 → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/user/status", ctrlTenant, 1)
	withJSONBody(c, `{"id":`)
	ctl.UpdateStatus(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 JSON 应返回 400，实际 %v", resp["code"])
	}

	// 改部门：绑定失败 → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/user/dept", ctrlTenant, 1)
	withJSONBody(c, `{"id":`)
	ctl.UpdateDept(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 JSON 应返回 400，实际 %v", resp["code"])
	}

	// 改部门：引用其他租户的部门 → 拒绝
	foreignDept := seedCtrlDeptRow(t, ctrlOtherTenant, "别人的部门")
	c, w = newCtx(http.MethodPut, "/api/v1/system/user/dept", ctrlTenant, 1)
	withJSONBody(c, `{"id":`+idStr(u.ID)+`,"deptId":`+idStr(foreignDept.ID)+`}`)
	ctl.UpdateDept(c)
	if resp := decodeResp(t, w); resp["code"].(float64) == 0 {
		t.Error("指向其他租户的部门必须被拒绝")
	}

	// 重置密码：绑定失败 → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/user/resetPwd", ctrlTenant, 1)
	withJSONBody(c, `{"id":`)
	ctl.ResetPassword(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 JSON 应返回 400，实际 %v", resp["code"])
	}

	// 改密码：绑定失败 → 400
	c, w = newCtx(http.MethodPut, "/api/v1/system/user/changePwd", ctrlTenant, 1)
	withJSONBody(c, `{"oldPassword":`)
	ctl.ChangePassword(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 JSON 应返回 400，实际 %v", resp["code"])
	}
}

// ---- 文件 ----

func TestFileControllerUploadAndDeleteErrorPaths(t *testing.T) {
	ctl := newFileControllerWithDB(t)

	// 超过上限的文件必须在落盘之前被拒（默认 10MB）
	c, w := newCtx(http.MethodPost, "/api/v1/system/file/upload", ctrlTenant, 1)
	withMultipartFile(c, "big.png", make([]byte, 10*1024*1024+1))
	ctl.Upload(c)
	resp := decodeResp(t, w)
	if resp["code"].(float64) == 0 {
		t.Error("超大文件必须被拒绝")
	}
	if resp["code"].(float64) != float64(common.CodeFileTooLarge) {
		t.Errorf("应返回文件过大码 %d，实际 %v（%v）", common.CodeFileTooLarge, resp["code"], resp["message"])
	}

	// 删除：非法 ID → 400
	c, w = newCtx(http.MethodDelete, "/api/v1/system/file/x", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: "abc"}}
	ctl.Delete(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeBadRequest) {
		t.Errorf("非法 ID 应返回 400，实际 %v", resp["code"])
	}

	// 删除：自己的文件 → 成功（磁盘文件不存在只记日志，不应影响接口结果）
	mine := seedFile(t, ctl, ctrlTenant, "mine.txt")
	c, w = newCtx(http.MethodDelete, "/api/v1/system/file/1", ctrlTenant, 1)
	withIDParam(c, mine.ID)
	ctl.Delete(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("删除自己的文件应成功，实际 %v（%v）", resp["code"], resp["message"])
	}
	var left int64
	if err := testDB(t).Model(&model.SysFile{}).Where("id = ?", mine.ID).Count(&left).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if left != 0 {
		t.Errorf("文件记录应被删除，实际剩 %d", left)
	}
}

// ---- 认证 ----

func TestAuthControllerErrorPaths(t *testing.T) {
	newFullSystemDB(t)
	ctl := NewAuthController()

	// 登出：带 refresh token 但吊销设施不可用 → 必须报错，
	// 否则用户以为已登出，手里的 refresh token 仍能换发新 access token
	c, w := newCtx(http.MethodPost, "/api/v1/auth/logout", ctrlTenant, 1)
	withJSONBody(c, `{"refreshToken":"some-refresh-token"}`)
	c.Request.Header.Set("Authorization", "Bearer not-a-real-token")
	ctl.Logout(c)
	if resp := decodeResp(t, w); resp["code"].(float64) == 0 {
		t.Error("refresh token 吊销失败时必须报错，不能静默成功")
	}

	// 用户信息：用户不存在 → 404（而不是 500）
	c, w = newCtx(http.MethodGet, "/api/v1/auth/userInfo", ctrlTenant, 99999)
	ctl.GetUserInfo(c)
	if resp := decodeResp(t, w); resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}
}
