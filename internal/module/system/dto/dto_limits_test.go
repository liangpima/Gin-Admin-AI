package dto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 本文件钉住「入口处的长度上限」这条契约。
//
// 为什么值得单独测：这些上限唯一的执行点是 gin 的 binding tag，
// 而 tag 是字符串 —— 写错（拼成 `max=1000`、漏掉 `omitempty`、
// 或者后来有人「顺手」删掉）不会有任何编译期提示，
// 单测是唯一的守门人。同时它也把「上限到底是多少」变成可执行的文档。
//
// 断言分两个方向：
//   - 超限必须被拒（否则就是报告里说的内存放大 / 巨型 IN 查询）
//   - 恰好等于上限、以及「不传该字段」必须通过（否则合法请求会被误伤，
//     这是这类改动最容易引入的回归 —— 部分更新语义依赖 omitempty）

// bindDTO 走真实的 gin binding 路径校验请求体。
//
// 刻意不直接调 validator：gin 对 JSON 的绑定行为（字段名大小写、
// Content-Type 判断、错误包装）才是线上真正生效的那条链路。
func bindDTO(t *testing.T, body interface{}, dst interface{}) error {
	t.Helper()
	gin.SetMode(gin.TestMode)

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("构造请求体失败: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(raw)))
	c.Request.Header.Set("Content-Type", "application/json")
	return c.ShouldBindJSON(dst)
}

// ids 生成 n 个互不相同的 ID。
func ids(n int) []uint {
	out := make([]uint, n)
	for i := range out {
		out[i] = uint(i + 1)
	}
	return out
}

// validCreateUserBody 一个能通过 CreateUserRequest 全部必填校验的请求体，
// 只把待测字段留出来覆盖，避免「因为缺 username 而失败」的假阳性。
func validCreateUserBody() map[string]interface{} {
	return map[string]interface{}{
		"username": "alice",
		"password": "secret123",
		"status":   1,
		"deptId":   1,
	}
}

func TestCreateUserDTORejectsOversizedIDArrays(t *testing.T) {
	t.Run("roleIds 超过 100 被拒", func(t *testing.T) {
		body := validCreateUserBody()
		body["roleIds"] = ids(101)

		var req CreateUserRequest
		err := bindDTO(t, body, &req)
		if err == nil {
			t.Fatal("101 个 roleIds 必须被拒（无上限时可被放大成数百万 ID + 巨型 IN 查询）")
		}
		// 错误信息必须指出是哪个字段：否则前端只能提示「参数错误」，
		// 用户面对一个几百字段的表单无从下手。
		if !strings.Contains(err.Error(), "RoleIds") {
			t.Errorf("错误信息应指明字段 RoleIds，实际: %v", err)
		}
	})

	t.Run("roleIds 恰好 100 通过", func(t *testing.T) {
		body := validCreateUserBody()
		body["roleIds"] = ids(100)

		var req CreateUserRequest
		if err := bindDTO(t, body, &req); err != nil {
			t.Fatalf("恰好等于上限不应被拒: %v", err)
		}
		if len(req.RoleIds) != 100 {
			t.Errorf("绑定后应拿到 100 个 ID，实际 %d", len(req.RoleIds))
		}
	})

	t.Run("postIds 超过 100 被拒", func(t *testing.T) {
		body := validCreateUserBody()
		body["postIds"] = ids(101)

		var req CreateUserRequest
		if err := bindDTO(t, body, &req); err == nil {
			t.Fatal("101 个 postIds 必须被拒")
		}
	})

	t.Run("不传与空数组都通过（部分更新语义依赖它）", func(t *testing.T) {
		// 不传：nil 切片，omitempty 跳过校验
		var req CreateUserRequest
		if err := bindDTO(t, validCreateUserBody(), &req); err != nil {
			t.Fatalf("不传 roleIds 应通过: %v", err)
		}
		if req.RoleIds != nil {
			t.Errorf("未提供时应保持 nil，实际 %#v", req.RoleIds)
		}

		// 传空数组：非 nil，但仍应通过（len 0 <= max）
		body := validCreateUserBody()
		body["roleIds"] = []uint{}
		var req2 CreateUserRequest
		if err := bindDTO(t, body, &req2); err != nil {
			t.Fatalf("空 roleIds 数组应通过（前端「清空全部角色」就是提交 []）: %v", err)
		}
		if req2.RoleIds == nil || len(req2.RoleIds) != 0 {
			t.Errorf("空数组应绑定成非 nil 的空切片，实际 %#v", req2.RoleIds)
		}
	})
}

func TestUpdateUserDTORejectsOversizedIDArrays(t *testing.T) {
	t.Run("UpdateUserRequest.roleIds 超过 100 被拒", func(t *testing.T) {
		var req UpdateUserRequest
		if err := bindDTO(t, map[string]interface{}{"id": 1, "roleIds": ids(101)}, &req); err == nil {
			t.Fatal("101 个 roleIds 必须被拒")
		}
	})

	t.Run("UpdateUserRequest.postIds 超过 100 被拒", func(t *testing.T) {
		var req UpdateUserRequest
		if err := bindDTO(t, map[string]interface{}{"id": 1, "postIds": ids(101)}, &req); err == nil {
			t.Fatal("101 个 postIds 必须被拒")
		}
	})

	t.Run("UpdateUserRequest 只传 id 仍合法", func(t *testing.T) {
		var req UpdateUserRequest
		if err := bindDTO(t, map[string]interface{}{"id": 1}, &req); err != nil {
			t.Fatalf("只传 id 的部分更新应通过: %v", err)
		}
	})

	t.Run("UpdateUserRolesRequest.roleIds 超过 100 被拒", func(t *testing.T) {
		// 这是「分配角色」独立入口，同样不能被绕过
		var req UpdateUserRolesRequest
		if err := bindDTO(t, map[string]interface{}{"id": 1, "roleIds": ids(101)}, &req); err == nil {
			t.Fatal("101 个 roleIds 必须被拒（否则可绕过 Create/Update 的上限）")
		}
	})

	t.Run("UpdateUserRolesRequest 空数组通过（清空授权）", func(t *testing.T) {
		var req UpdateUserRolesRequest
		if err := bindDTO(t, map[string]interface{}{"id": 1, "roleIds": []uint{}}, &req); err != nil {
			t.Fatalf("清空全部角色应通过: %v", err)
		}
	})
}

func TestRoleDTORejectsOversizedMenuIDs(t *testing.T) {
	validRole := func() map[string]interface{} {
		return map[string]interface{}{
			"name":      "测试角色",
			"code":      "test_role",
			"status":    1,
			"dataScope": 1,
		}
	}

	t.Run("CreateRoleRequest.menuIds 超过 500 被拒", func(t *testing.T) {
		body := validRole()
		body["menuIds"] = ids(501)

		var req CreateRoleRequest
		err := bindDTO(t, body, &req)
		if err == nil {
			t.Fatal("501 个 menuIds 必须被拒")
		}
		if !strings.Contains(err.Error(), "MenuIds") {
			t.Errorf("错误信息应指明字段 MenuIds，实际: %v", err)
		}
	})

	t.Run("CreateRoleRequest.menuIds 恰好 500 通过", func(t *testing.T) {
		// 上限必须高于「全选授权」的真实用量（当前种子菜单 69 条），
		// 否则超管给角色授全量权限时会被自己的校验挡住。
		body := validRole()
		body["menuIds"] = ids(500)

		var req CreateRoleRequest
		if err := bindDTO(t, body, &req); err != nil {
			t.Fatalf("恰好等于上限不应被拒: %v", err)
		}
	})

	t.Run("CreateRoleRequest 不传 menuIds 通过", func(t *testing.T) {
		var req CreateRoleRequest
		if err := bindDTO(t, validRole(), &req); err != nil {
			t.Fatalf("不传 menuIds 应通过: %v", err)
		}
	})

	t.Run("UpdateRoleRequest.menuIds 超过 500 被拒", func(t *testing.T) {
		// 「保存权限」走的就是这个 DTO，是 menuIds 的主要入口
		var req UpdateRoleRequest
		if err := bindDTO(t, map[string]interface{}{"id": 1, "menuIds": ids(501)}, &req); err == nil {
			t.Fatal("501 个 menuIds 必须被拒")
		}
	})

	t.Run("UpdateRoleRequest 只传 id+menuIds 仍合法", func(t *testing.T) {
		// 前端「保存权限」只提交这两个字段，其余字段缺省不能触发校验失败
		var req UpdateRoleRequest
		if err := bindDTO(t, map[string]interface{}{"id": 1, "menuIds": ids(10)}, &req); err != nil {
			t.Fatalf("保存权限的请求应通过: %v", err)
		}
	})
}

func TestDictDTORejectsOversizedStrings(t *testing.T) {
	t.Run("字典类型 name 超 128 被拒", func(t *testing.T) {
		var req CreateDictTypeRequest
		err := bindDTO(t, map[string]interface{}{
			"name": strings.Repeat("a", 129),
			"type": "sys_x",
		}, &req)
		if err == nil {
			t.Fatal("129 字符的 name 必须被拒（列宽 varchar(128)，否则 DB 报 1406 → 500）")
		}
		if !strings.Contains(err.Error(), "Name") {
			t.Errorf("错误信息应指明字段 Name，实际: %v", err)
		}
	})

	t.Run("字典类型 name 恰好 128 通过", func(t *testing.T) {
		var req CreateDictTypeRequest
		if err := bindDTO(t, map[string]interface{}{
			"name": strings.Repeat("a", 128),
			"type": "sys_x",
		}, &req); err != nil {
			t.Fatalf("恰好等于列宽不应被拒: %v", err)
		}
	})

	t.Run("max 按字符数而非字节数", func(t *testing.T) {
		// MySQL 的 varchar(128) 在 utf8mb4 下按**字符**计数，
		// validator 的 max 对字符串同样用 RuneCountInString。
		// 若哪一方改成按字节算，128 个汉字（384 字节）就会被误判超长。
		var req CreateDictTypeRequest
		if err := bindDTO(t, map[string]interface{}{
			"name": strings.Repeat("中", 128),
			"type": "sys_x",
		}, &req); err != nil {
			t.Fatalf("128 个汉字应通过（列宽与校验都按字符计）: %v", err)
		}

		var req2 CreateDictTypeRequest
		if err := bindDTO(t, map[string]interface{}{
			"name": strings.Repeat("中", 129),
			"type": "sys_x",
		}, &req2); err == nil {
			t.Fatal("129 个汉字必须被拒")
		}
	})

	t.Run("字典类型 type 超 128 被拒", func(t *testing.T) {
		var req CreateDictTypeRequest
		if err := bindDTO(t, map[string]interface{}{
			"name": "状态",
			"type": strings.Repeat("a", 129),
		}, &req); err == nil {
			t.Fatal("129 字符的 type 必须被拒")
		}
	})

	t.Run("UpdateDictTypeRequest.remark 超 500 被拒", func(t *testing.T) {
		var req UpdateDictTypeRequest
		if err := bindDTO(t, map[string]interface{}{
			"name":   "状态",
			"remark": strings.Repeat("a", 501),
		}, &req); err == nil {
			t.Fatal("501 字符的 remark 必须被拒（列宽 varchar(500)）")
		}
	})

	t.Run("CreateDictDataRequest 各字段上限", func(t *testing.T) {
		ok := func() map[string]interface{} {
			return map[string]interface{}{
				"dictType": "sys_user_status",
				"label":    "启用",
				"value":    "1",
			}
		}

		for _, field := range []string{"dictType", "label", "value", "cssClass", "listClass"} {
			body := ok()
			body[field] = strings.Repeat("a", 129)

			var req CreateDictDataRequest
			if err := bindDTO(t, body, &req); err == nil {
				t.Errorf("%s 超 128 必须被拒", field)
			}
		}

		body := ok()
		body["remark"] = strings.Repeat("a", 501)
		var req CreateDictDataRequest
		if err := bindDTO(t, body, &req); err == nil {
			t.Error("remark 超 500 必须被拒")
		}

		// 全部取合法值时应通过，确认上面的失败确实是长度导致
		var okReq CreateDictDataRequest
		if err := bindDTO(t, ok(), &okReq); err != nil {
			t.Fatalf("合法请求体不应被拒: %v", err)
		}
	})

	t.Run("UpdateDictDataRequest 各字段上限", func(t *testing.T) {
		ok := func() map[string]interface{} {
			return map[string]interface{}{"label": "启用", "value": "1"}
		}

		for _, field := range []string{"label", "value", "cssClass", "listClass"} {
			body := ok()
			body[field] = strings.Repeat("a", 129)

			var req UpdateDictDataRequest
			if err := bindDTO(t, body, &req); err == nil {
				t.Errorf("%s 超 128 必须被拒", field)
			}
		}

		var okReq UpdateDictDataRequest
		if err := bindDTO(t, ok(), &okReq); err != nil {
			t.Fatalf("合法请求体不应被拒: %v", err)
		}
	})
}
