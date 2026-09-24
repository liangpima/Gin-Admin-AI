package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"

	"github.com/gin-gonic/gin"
)

// 部门控制器的租户透传测试（P1-1）。
//
// 为什么值得在 Controller 层再测一遍：Service/Repository 的隔离已由各自的用例覆盖，
// 但「Controller 从上下文取出 tenantID 并传给 Service」这一步没有类型层面的保护 ——
// 少写一个参数、传错顺序，编译一样通过，结果是租户过滤被静默跳过。
// 这里走真实的 HTTP 上下文，端到端确认租户边界成立。
//
// ⚠️ testsupport.NewDB 会改写包级 database.DB，因此不能 t.Parallel。

func newDeptControllerWithDB(t *testing.T) *DeptController {
	t.Helper()
	// 先建库再构造 Controller —— Service/Repository 在构造时捕获 database.DB
	testsupport.NewDB(t, &model.SysDept{})
	return NewDeptController()
}

// seedCtrlDept 建一个根级部门并返回它的 ID。
// Service 的 Create 只返回 error，所以建完从树里回查 ID。
func seedCtrlDept(t *testing.T, ctl *DeptController, tenantID uint, name string) uint {
	t.Helper()
	if err := ctl.deptService.Create(&dto.CreateDeptRequest{Name: name, Status: 1}, 1, tenantID); err != nil {
		t.Fatalf("创建部门失败: %v", err)
	}
	depts, err := ctl.deptService.FindTree(tenantID)
	if err != nil {
		t.Fatalf("回读部门失败: %v", err)
	}
	for _, d := range depts {
		if d.Name == name {
			return d.ID
		}
	}
	t.Fatalf("未找到刚创建的部门 %q", name)
	return 0
}

// withIDParam 把路径参数塞进测试上下文（gin 的 c.Param 由此读取）
func withIDParam(c *gin.Context, id uint) {
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(id), 10)}}
}

// withJSONBody 用给定 JSON 作为请求体（Controller 用 ShouldBindJSON 读取）
func withJSONBody(c *gin.Context, body string) {
	c.Request = httptest.NewRequest(http.MethodPost, c.Request.URL.Path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
}

// TestDeptControllerTreeIsTenantScoped 租户 A 的部门树只包含本租户的部门。
//
// 改造前 sys_dept 是全局表，两个租户会看到彼此的部门（含名称、负责人、电话、邮箱）。
func TestDeptControllerTreeIsTenantScoped(t *testing.T) {
	ctl := newDeptControllerWithDB(t)
	seedCtrlDept(t, ctl, ctrlTenant, "A-研发")
	seedCtrlDept(t, ctl, ctrlTenant+1, "B-市场")

	c, w := newCtx(http.MethodGet, "/api/v1/system/dept/tree", ctrlTenant, 1)
	ctl.FindTree(c)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp["code"].(float64) != 0 {
		t.Fatalf("业务码应为 0，实际 %v（%v）", resp["code"], resp["message"])
	}

	list, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("data 应为数组，实际 %T", resp["data"])
	}
	if len(list) != 1 {
		t.Fatalf("租户 %d 应只看到 1 个部门，实际 %d 个：%v", ctrlTenant, len(list), list)
	}
	first := list[0].(map[string]interface{})
	if first["name"] != "A-研发" {
		t.Errorf("应只看到本租户的部门，实际 %v", first["name"])
	}
	// tenantId 出现在响应里，便于排查归属问题
	if first["tenantId"].(float64) != float64(ctrlTenant) {
		t.Errorf("响应里的 tenantId 应为 %d，实际 %v", ctrlTenant, first["tenantId"])
	}
}

// TestDeptControllerFindByIDIsTenantScoped 拿别租户的部门 ID 查不到（404 而非返回数据）。
func TestDeptControllerFindByIDIsTenantScoped(t *testing.T) {
	ctl := newDeptControllerWithDB(t)
	foreignID := seedCtrlDept(t, ctl, ctrlTenant+1, "B-部门")

	c, w := newCtx(http.MethodGet, "/api/v1/system/dept/1", ctrlTenant, 1)
	withIDParam(c, foreignID)
	ctl.FindByID(c)

	resp := decodeResp(t, w)
	if resp["code"].(float64) != float64(common.CodeNotFound) {
		t.Errorf("跨租户查询应返回 404，实际 %v（%v）", resp["code"], resp["message"])
	}
}

// TestDeptControllerDeleteIsTenantScoped 别租户的部门删不掉。
func TestDeptControllerDeleteIsTenantScoped(t *testing.T) {
	ctl := newDeptControllerWithDB(t)
	foreignID := seedCtrlDept(t, ctl, ctrlTenant+1, "B-部门")

	c, w := newCtx(http.MethodDelete, "/api/v1/system/dept/1", ctrlTenant, 1)
	withIDParam(c, foreignID)
	ctl.Delete(c)

	resp := decodeResp(t, w)
	if resp["code"].(float64) == 0 {
		t.Error("跨租户删除必须失败")
	}

	// 部门必须仍在
	if _, err := ctl.deptService.FindByID(ctrlTenant+1, foreignID); err != nil {
		t.Errorf("部门不应被删除: %v", err)
	}
}

// TestDeptControllerCreateStampsOperatorTenant 新建的部门必须落在操作者所属租户下。
//
// 租户只能来自登录上下文：请求体里没有（也不该有）tenantId 字段，
// 否则租户 A 就能把部门建到租户 B 名下。
func TestDeptControllerCreateStampsOperatorTenant(t *testing.T) {
	ctl := newDeptControllerWithDB(t)
	const otherTenant uint = 9

	c, w := newCtx(http.MethodPost, "/api/v1/system/dept", otherTenant, 1)
	withJSONBody(c, `{"name":"新部门","status":1}`)
	ctl.Create(c)

	if resp := decodeResp(t, w); resp["code"].(float64) != 0 {
		t.Fatalf("创建应成功，实际 %v（%v）", resp["code"], resp["message"])
	}

	// 平台级视角（tenantID=0 不过滤）能看到它，且归属正确
	all, err := ctl.deptService.FindTree(0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	found := false
	for _, d := range all {
		if d.Name == "新部门" {
			found = true
			if d.TenantID != otherTenant {
				t.Errorf("部门应归属租户 %d，实际 %d", otherTenant, d.TenantID)
			}
		}
	}
	if !found {
		t.Fatal("未找到新建的部门")
	}

	// 其他租户看不到它
	elsewhere, err := ctl.deptService.FindTree(otherTenant + 1)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(elsewhere) != 0 {
		t.Errorf("其他租户不应看到该部门，实际 %d 个", len(elsewhere))
	}
}
