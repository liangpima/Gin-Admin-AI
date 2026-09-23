package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"

	"github.com/gin-gonic/gin"
)

// 本文件是 Controller 层的第一个测试，用来确立范式：
// 用 httptest + gin.CreateTestContext 构造请求上下文，
// 用 testsupport 注入内存库，断言「状态码 + 业务码 + 返回体」。
//
// ⚠️ testsupport.NewDB 会改写包级 database.DB，因此不能 t.Parallel。

const ctrlTenant uint = 1

// newFileControllerWithDB 先建库再构造 Controller ——
// Service/Repository 在构造时捕获 database.DB，顺序反了会拿到旧引用。
func newFileControllerWithDB(t *testing.T) *FileController {
	t.Helper()
	gin.SetMode(gin.TestMode)
	testsupport.NewDB(t, &model.SysFile{})
	return NewFileController()
}

// newCtx 构造带租户/操作人上下文的测试请求
func newCtx(method, path string, tenantID, userID uint) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)
	if tenantID > 0 {
		c.Set(common.ContextKeyTenantID, tenantID)
	}
	if userID > 0 {
		c.Set(common.ContextKeyUserID, userID)
	}
	return c, w
}

// decodeResp 解析统一响应结构
func decodeResp(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v (body=%s)", err, w.Body.String())
	}
	return out
}

func seedFile(t *testing.T, ctl *FileController, tenantID uint, name string) *model.SysFile {
	t.Helper()
	f := &model.SysFile{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Name:            name,
		Path:            "uploads/" + name,
		URL:             "/uploads/" + name,
		Size:            1024,
		MimeType:        "text/plain",
	}
	if err := ctl.fileService.Create(tenantID, f); err != nil {
		t.Fatalf("准备文件记录失败: %v", err)
	}
	return f
}

// TestFileControllerUploadWithoutFile 缺少文件时必须返回参数错误，而不是 500。
//
// 这是上传接口最基本的兜底：前端未选文件就提交时，应得到可理解的提示。
func TestFileControllerUploadWithoutFile(t *testing.T) {
	ctl := newFileControllerWithDB(t)

	c, w := newCtx(http.MethodPost, "/api/v1/system/file/upload", ctrlTenant, 1)
	ctl.Upload(c)

	if w.Code != http.StatusOK {
		t.Fatalf("业务错误应保持 HTTP 200，实际 %d", w.Code)
	}
	resp := decodeResp(t, w)
	if code, _ := resp["code"].(float64); int(code) != common.CodeBadRequest {
		t.Errorf("应返回参数错误码 %d，实际 %v", common.CodeBadRequest, resp["code"])
	}
}

// TestFileControllerFindListIsTenantScoped 列表接口不能跨租户返回数据。
//
// 租户隔离是这套框架的核心约定（AGENTS 规则 7），
// 但此前 Controller 层没有任何测试，回归时不会被发现。
func TestFileControllerFindListIsTenantScoped(t *testing.T) {
	ctl := newFileControllerWithDB(t)
	seedFile(t, ctl, ctrlTenant, "mine.txt")
	seedFile(t, ctl, ctrlTenant+1, "others.txt")

	c, w := newCtx(http.MethodGet, "/api/v1/system/file/list?page=1&pageSize=10", ctrlTenant, 1)
	ctl.FindList(c)

	resp := decodeResp(t, w)
	if code, _ := resp["code"].(float64); int(code) != common.CodeSuccess {
		t.Fatalf("列表查询失败: %v", resp)
	}

	body, _ := json.Marshal(resp["data"])
	if strings.Contains(string(body), "others.txt") {
		t.Fatalf("列表返回了其他租户的数据: %s", body)
	}
	if !strings.Contains(string(body), "mine.txt") {
		t.Errorf("列表未返回本租户数据: %s", body)
	}
}

// TestFileControllerDeleteOtherTenantNotFound 删除他人文件必须 404，且不落任何副作用。
//
// 删除路径还包含「先删库记录、再删磁盘文件」的顺序约定，
// 这里的断言保证越权请求在第一步就被挡下。
func TestFileControllerDeleteOtherTenantNotFound(t *testing.T) {
	ctl := newFileControllerWithDB(t)
	foreign := seedFile(t, ctl, ctrlTenant+1, "others.txt")

	c, w := newCtx(http.MethodDelete, "/api/v1/system/file/1", ctrlTenant, 1)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(foreign.ID), 10)}}
	ctl.Delete(c)

	resp := decodeResp(t, w)
	if code, _ := resp["code"].(float64); int(code) != common.CodeNotFound {
		t.Fatalf("删除他人文件应返回 404，实际 %v", resp)
	}

	// 记录必须还在（越权请求不能产生任何副作用）
	if _, err := ctl.fileService.FindByID(ctrlTenant+1, foreign.ID); err != nil {
		t.Errorf("越权删除产生了副作用，记录已不可见: %v", err)
	}
}
