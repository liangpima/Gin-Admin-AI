package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/database"
	systemModel "go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Casbin 授权核心的测试（P2-1）。
//
// 这块此前**完全没有覆盖**，而它是全站鉴权的落点：
//   - SyncPoliciesFromRoleMenus 决定「谁能访问什么」，策略生成错了要么全站 403，
//     要么把人放进去
//   - gormAdapter 决定策略能否正确落库/读回，写错会让鉴权静默失效
//   - CasbinAuth 的默认拒绝是「新增接口漏配权限」时的最后一道防线
//
// 全部可离线验证：用内存 SQLite + 真实的 config/casbin/model.conf。
// ⚠️ testsupport.NewDB 会改写包级 database.DB，且本文件会动 enforcerPtr 等包级状态，
// 因此不能 t.Parallel。

// modelPath 真实的 Casbin 模型文件（相对本包目录）
func modelPath() string {
	return filepath.Join("..", "..", "config", "casbin", "model.conf")
}

// newCasbinDB 建库：策略同步要读 sys_role_menu / sys_role / sys_menu，写 casbin_rule。
func newCasbinDB(t *testing.T) {
	t.Helper()
	testsupport.NewDB(t, &systemModel.SysRole{}, &systemModel.SysRoleMenu{}, &systemModel.SysMenu{}, &CasbinRule{})
}

func seedRole(t *testing.T, code string, status int8) *systemModel.SysRole {
	t.Helper()
	r := &systemModel.SysRole{
		TenantBaseModel: common.TenantBaseModel{TenantID: 1},
		Name:            "角色-" + code,
		Code:            code,
		Status:          status,
	}
	if err := databaseCreate(t, r); err != nil {
		t.Fatalf("准备角色失败: %v", err)
	}
	return r
}

func seedMenu(t *testing.T, permission string) *systemModel.SysMenu {
	t.Helper()
	m := &systemModel.SysMenu{Name: "菜单", Title: "菜单", Type: 2, Permission: permission, Status: 1}
	if err := databaseCreate(t, m); err != nil {
		t.Fatalf("准备菜单失败: %v", err)
	}
	return m
}

func grantMenu(t *testing.T, roleID, menuID uint) {
	t.Helper()
	if err := databaseCreate(t, &systemModel.SysRoleMenu{RoleID: roleID, MenuID: menuID}); err != nil {
		t.Fatalf("绑定角色菜单失败: %v", err)
	}
}

// ---- 适配器 ----

// TestRuleToLineStripsTrailingEmptyFields 尾部空列必须去掉。
//
// 不去掉会解析出多余的空列，Casbin 的匹配就会带着一堆空字符串参与比较 ——
// 表现为「策略看着写进去了，但就是不生效」。
func TestRuleToLineStripsTrailingEmptyFields(t *testing.T) {
	cases := []struct {
		name string
		rule CasbinRule
		want string
	}{
		{"完整四列", CasbinRule{Ptype: "p", V0: "admin", V1: "default", V2: "*", V3: "*"}, "p, admin, default, *, *"},
		{"只到 v2", CasbinRule{Ptype: "p", V0: "editor", V1: "default", V2: "system:user:list"}, "p, editor, default, system:user:list"},
		{"中间空列保留", CasbinRule{Ptype: "p", V0: "a", V1: "", V2: "c"}, "p, a, , c"},
		{"只有 ptype", CasbinRule{Ptype: "p"}, "p"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ruleToLine(tc.rule); got != tc.want {
				t.Errorf("期望 %q，实际 %q", tc.want, got)
			}
		})
	}
}

// TestBuildRuleMapsFieldsAndIgnoresExtra 规则切片映射到列，超出部分忽略而不是 panic。
func TestBuildRuleMapsFieldsAndIgnoresExtra(t *testing.T) {
	r := buildRule("p", []string{"a", "b", "c", "d", "e", "f", "g", "h"})
	if r.Ptype != "p" || r.V0 != "a" || r.V5 != "f" {
		t.Errorf("字段映射错误: %+v", r)
	}
	if r.V4 != "e" {
		t.Errorf("V4 应为 e，实际 %q", r.V4)
	}
}

// TestAdapterSaveAndLoadPolicyRoundTrip 策略落库后能被完整读回。
//
// 这是「重启后权限仍然正确」的基础：写进去读不出来，服务一重启就全站 403。
func TestAdapterSaveAndLoadPolicyRoundTrip(t *testing.T) {
	newCasbinDB(t)
	adapter := newGormAdapter(databaseDB(t))

	m, err := model.NewModelFromFile(modelPath())
	if err != nil {
		t.Fatalf("加载模型失败: %v", err)
	}
	_ = m.AddPolicy("p", "p", []string{"admin", "default", "*", "*"})
	_ = m.AddPolicy("p", "p", []string{"editor", "default", "system:user:list", "*"})

	if err := adapter.SavePolicy(m); err != nil {
		t.Fatalf("保存策略失败: %v", err)
	}

	var count int64
	if err := databaseDB(t).Model(&CasbinRule{}).Count(&count).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if count != 2 {
		t.Fatalf("应落库 2 条策略，实际 %d", count)
	}

	// 读回到一个全新的模型里，逐条比对
	loaded, err := model.NewModelFromFile(modelPath())
	if err != nil {
		t.Fatalf("加载模型失败: %v", err)
	}
	if err := adapter.LoadPolicy(loaded); err != nil {
		t.Fatalf("读取策略失败: %v", err)
	}
	if got := len(loaded["p"]["p"].Policy); got != 2 {
		t.Fatalf("应读回 2 条策略，实际 %d", got)
	}
	for _, want := range [][]string{
		{"admin", "default", "*", "*"},
		{"editor", "default", "system:user:list", "*"},
	} {
		found := false
		for _, got := range loaded["p"]["p"].Policy {
			if strings.Join(got, "|") == strings.Join(want, "|") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("策略 %v 未被读回", want)
		}
	}
}

// TestAdapterSavePolicyIsFullRebuild SavePolicy 是全量重建（先清空再写）。
//
// 不是增量：残留的旧策略意味着「已经撤销的权限仍然生效」。
func TestAdapterSavePolicyIsFullRebuild(t *testing.T) {
	newCasbinDB(t)
	adapter := newGormAdapter(databaseDB(t))

	// 先塞一条「历史遗留」的策略
	if err := databaseDB(t).Create(&CasbinRule{Ptype: "p", V0: "stale", V1: "default", V2: "*", V3: "*"}).Error; err != nil {
		t.Fatalf("准备失败: %v", err)
	}

	m, err := model.NewModelFromFile(modelPath())
	if err != nil {
		t.Fatalf("加载模型失败: %v", err)
	}
	_ = m.AddPolicy("p", "p", []string{"admin", "default", "*", "*"})
	if err := adapter.SavePolicy(m); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	var stale int64
	if err := databaseDB(t).Model(&CasbinRule{}).Where("v0 = ?", "stale").Count(&stale).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if stale != 0 {
		t.Error("全量重建应清掉旧策略，否则已撤销的权限仍然生效")
	}
}

// TestAdapterAddAndRemovePolicy 单条与批量增删。
func TestAdapterAddAndRemovePolicy(t *testing.T) {
	newCasbinDB(t)
	adapter := newGormAdapter(databaseDB(t))

	t.Run("AddPolicy + RemovePolicy", func(t *testing.T) {
		if err := adapter.AddPolicy("p", "p", []string{"editor", "default", "system:user:list", "*"}); err != nil {
			t.Fatalf("新增失败: %v", err)
		}
		if got := countRules(t); got != 1 {
			t.Fatalf("应有 1 条策略，实际 %d", got)
		}
		if err := adapter.RemovePolicy("p", "p", []string{"editor", "default", "system:user:list", "*"}); err != nil {
			t.Fatalf("删除失败: %v", err)
		}
		if got := countRules(t); got != 0 {
			t.Errorf("删除后应为 0 条，实际 %d", got)
		}
	})

	t.Run("AddPolicies 空切片不报错", func(t *testing.T) {
		if err := adapter.AddPolicies("p", "p", nil); err != nil {
			t.Errorf("空批量应静默成功: %v", err)
		}
	})

	t.Run("AddPolicies + RemovePolicies", func(t *testing.T) {
		rules := [][]string{
			{"a", "default", "x", "*"},
			{"b", "default", "y", "*"},
		}
		if err := adapter.AddPolicies("p", "p", rules); err != nil {
			t.Fatalf("批量新增失败: %v", err)
		}
		if got := countRules(t); got != 2 {
			t.Fatalf("应有 2 条，实际 %d", got)
		}
		if err := adapter.RemovePolicies("p", "p", rules); err != nil {
			t.Fatalf("批量删除失败: %v", err)
		}
		if got := countRules(t); got != 0 {
			t.Errorf("批量删除后应为 0 条，实际 %d", got)
		}
	})

	t.Run("RemoveFilteredPolicy 按列过滤删除", func(t *testing.T) {
		rules := [][]string{
			{"roleA", "default", "p1", "*"},
			{"roleB", "default", "p2", "*"},
		}
		if err := adapter.AddPolicies("p", "p", rules); err != nil {
			t.Fatalf("准备失败: %v", err)
		}
		// fieldIndex=0 → 只删 v0=roleA 的
		if err := adapter.RemoveFilteredPolicy("p", "p", 0, "roleA"); err != nil {
			t.Fatalf("过滤删除失败: %v", err)
		}
		if got := countRules(t); got != 1 {
			t.Errorf("应只剩 1 条，实际 %d", got)
		}
		var left CasbinRule
		if err := databaseDB(t).First(&left).Error; err != nil {
			t.Fatalf("读取剩余策略失败: %v", err)
		}
		if left.V0 != "roleB" {
			t.Errorf("删错了策略: %+v", left)
		}
	})
}

// TestAdapterImplementsBatchAdapter 批量接口必须实现。
//
// casbin 的 Enforcer.AddPolicies 会对适配器做**不带检查**的
// persist.BatchAdapter 类型断言，缺失该方法会直接 panic（不是返回错误）。
func TestAdapterImplementsBatchAdapter(t *testing.T) {
	var adapter persist.Adapter = newGormAdapter(nil)
	if _, ok := adapter.(persist.BatchAdapter); !ok {
		t.Fatal("适配器必须实现 persist.BatchAdapter，否则 Enforcer.AddPolicies 会 panic")
	}
}

// ---- 策略同步 ----

// TestSyncPoliciesGeneratesRulesFromRoleMenus 策略由「角色-菜单」关系生成。
//
// 三条关键规则：
//   - admin 始终有一条通配策略（*/*），与它挂了哪些菜单无关
//   - 每条 (启用角色, 非空权限码) 生成一条策略
//   - 停用角色不参与生成（「停用角色」这个操作必须立即生效）
func TestSyncPoliciesGeneratesRulesFromRoleMenus(t *testing.T) {
	newCasbinDB(t)

	editor := seedRole(t, "editor", 1)
	disabled := seedRole(t, "disabled", 0) // 停用角色

	userList := seedMenu(t, "system:user:list")
	roleAdd := seedMenu(t, "system:role:add")
	dirMenu := seedMenu(t, "") // 目录型菜单，无权限码

	// 刻意**不给 admin 挂任何菜单**：它的通配策略由 SyncPoliciesFromRoleMenus
	// 无条件写入，与挂了哪些菜单无关（挂上反而会多生成一条冗余的具名策略）
	grantMenu(t, editor.ID, userList.ID)
	grantMenu(t, editor.ID, dirMenu.ID) // 不应生成策略
	grantMenu(t, disabled.ID, roleAdd.ID)

	if err := InitCasbin(modelPath()); err != nil {
		t.Fatalf("初始化 Casbin 失败: %v", err)
	}

	rules := loadAllRules(t)
	// admin 通配 + editor 的 user:list = 2 条；目录型菜单与停用角色都不生成
	if len(rules) != 2 {
		t.Fatalf("应生成 2 条策略，实际 %d 条: %+v", len(rules), rules)
	}

	want := map[string]bool{
		AdminRoleCode + "|default|*|*":                    false,
		"editor|default|system:user:list|*":               false,
	}
	for _, r := range rules {
		key := strings.Join([]string{r.V0, r.V1, r.V2, r.V3}, "|")
		if _, ok := want[key]; ok {
			want[key] = true
		} else {
			t.Errorf("出现了不该有的策略: %s", key)
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("缺少策略: %s", k)
		}
	}

	// 停用角色的权限不能出现在策略里
	for _, r := range rules {
		if r.V0 == "disabled" {
			t.Error("停用角色不应生成策略（否则「停用角色」不生效）")
		}
	}
}

// TestSyncPoliciesDeduplicates 同一角色重复挂同一权限只生成一条策略。
func TestSyncPoliciesDeduplicates(t *testing.T) {
	newCasbinDB(t)

	role := seedRole(t, "editor", 1)
	// 两个不同菜单声明同一个权限码（例如「保存」按钮在多处存在）
	m1 := seedMenu(t, "system:config:edit")
	m2 := seedMenu(t, "system:config:edit")
	grantMenu(t, role.ID, m1.ID)
	grantMenu(t, role.ID, m2.ID)

	if err := InitCasbin(modelPath()); err != nil {
		t.Fatalf("初始化失败: %v", err)
	}

	rules := loadAllRules(t)
	if len(rules) != 2 { // admin 通配 + 1 条去重后的
		t.Errorf("重复权限应去重，实际 %d 条: %+v", len(rules), rules)
	}
}

// TestSyncPoliciesRejectsOverlongPermission 超长权限码必须报错并保持旧策略不变。
//
// 校验必须在**动数据库之前**完成：否则会出现「旧策略已清空、新策略没写进去」，
// 结果是除 admin 外全站 403。
func TestSyncPoliciesRejectsOverlongPermission(t *testing.T) {
	newCasbinDB(t)

	role := seedRole(t, "editor", 1)
	long := strings.Repeat("x", maxRuleValueLen+1)
	menu := seedMenu(t, long)
	grantMenu(t, role.ID, menu.ID)

	if err := SyncPoliciesFromRoleMenus(); err == nil {
		t.Fatal("超长权限码应报错")
	}

	// 失败后策略表不应被清空/写入半截数据
	if got := countRules(t); got != 0 {
		t.Errorf("校验失败时不应写入任何策略，实际 %d 条", got)
	}
}

// TestCasbinAuth 鉴权中间件的判定。
//
// 这是「新增接口漏配权限」时的最后一道防线，也是「默认拒绝」的落点。
func TestCasbinAuth(t *testing.T) {
	newCasbinDB(t)

	role := seedRole(t, "editor", 1)
	menu := seedMenu(t, "system:user:list")
	grantMenu(t, role.ID, menu.ID)

	if err := InitCasbin(modelPath()); err != nil {
		t.Fatalf("初始化失败: %v", err)
	}

	// 注册一条测试路由（用独立路径，避免影响同包其它用例的登记表）
	RegisterPermission(http.MethodGet, "/__test/casbin/users", "system:user:list")
	RegisterPermission(http.MethodGet, "/__test/casbin/open", "")
	// /__test/casbin/unregistered 刻意不登记

	cases := []struct {
		name       string
		path       string
		roles      []string
		wantStatus int
	}{
		{"持有权限放行", "/__test/casbin/users", []string{"editor"}, http.StatusOK},
		{"admin 通配放行", "/__test/casbin/users", []string{AdminRoleCode}, http.StatusOK},
		{"角色不含该权限拒绝", "/__test/casbin/users", []string{"viewer"}, http.StatusForbidden},
		{"无角色拒绝", "/__test/casbin/users", nil, http.StatusForbidden},
		{"未登记路由默认拒绝", "/__test/casbin/unregistered", []string{AdminRoleCode}, http.StatusForbidden},
		{"空权限码仅要求登录态", "/__test/casbin/open", nil, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withRoleResolver(t, stubRoleResolver{codes: tc.roles})

			handlerRan := false
			r := gin.New()
			r.GET(tc.path,
				func(c *gin.Context) {
					// 用独立的 userID 避开角色缓存的相互干扰
					c.Set(common.ContextKeyUserID, uint(1000+len(tc.name)))
					c.Set(common.ContextKeyTenantID, uint(1))
				},
				CasbinAuth(),
				func(c *gin.Context) {
					handlerRan = true
					c.Status(http.StatusOK)
				})

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))

			if w.Code != tc.wantStatus {
				t.Errorf("状态码应为 %d，实际 %d（body=%s）", tc.wantStatus, w.Code, w.Body.String())
			}
			if wantRun := tc.wantStatus == http.StatusOK; handlerRan != wantRun {
				t.Errorf("业务处理函数是否执行应为 %v，实际 %v", wantRun, handlerRan)
			}
		})
	}
}

// TestCasbinAuthRejectsWhenEnforcerMissing enforcer 未就绪时拒绝而不是放行。
//
// 鉴权是安全控制：初始化失败时宁可不可用，也不能在「无鉴权」状态下对外服务。
func TestCasbinAuthRejectsWhenEnforcerMissing(t *testing.T) {
	newCasbinDB(t)
	RegisterPermission(http.MethodGet, "/__test/casbin/nofnforcer", "system:user:list")

	prev := enforcerPtr.Load()
	enforcerPtr.Store(nil)
	t.Cleanup(func() { enforcerPtr.Store(prev) })

	handlerRan := false
	r := gin.New()
	r.GET("/__test/casbin/nofnforcer", CasbinAuth(), func(c *gin.Context) {
		handlerRan = true
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/__test/casbin/nofnforcer", nil))

	// 注意：项目约定「业务码在 body 里，HTTP 状态只在 401/403 用真实值」，
	// 所以这里不能断言 HTTP != 200，要看 body 的 code 与「处理函数有没有跑」。
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v（body=%s）", err, w.Body.String())
	}
	if code, _ := resp["code"].(float64); int(code) != common.CodeInternalError {
		t.Errorf("应返回 500 业务码，实际 %v", resp["code"])
	}
	if handlerRan {
		t.Fatal("enforcer 未就绪时必须拒绝，绝不能放行到业务处理")
	}
}

// TestOperatorHoldsPermissions 授权收敛判定的直接用例。
//
// role_grant_test.go 从 Service 侧间接覆盖了它；这里直接断言判定本身，
// 因为它是「低权管理员不得授予超出自身权限」这条规则的唯一实现点。
func TestOperatorHoldsPermissions(t *testing.T) {
	newCasbinDB(t)

	role := seedRole(t, "editor", 1)
	menu := seedMenu(t, "system:user:list")
	grantMenu(t, role.ID, menu.ID)

	if err := InitCasbin(modelPath()); err != nil {
		t.Fatalf("初始化失败: %v", err)
	}

	const (
		heldPerm  = "system:user:list"
		otherPerm = "system:role:add"
	)

	t.Run("空权限列表直接放行", func(t *testing.T) {
		withRoleResolver(t, stubRoleResolver{codes: []string{"editor"}})
		ok, err := OperatorHoldsPermissions(1, 2001, nil)
		if err != nil || !ok {
			t.Errorf("空权限表示本次不涉及授权，应放行，实际 ok=%v err=%v", ok, err)
		}
	})

	t.Run("持有该权限", func(t *testing.T) {
		withRoleResolver(t, stubRoleResolver{codes: []string{"editor"}})
		ok, err := OperatorHoldsPermissions(1, 2002, []string{heldPerm})
		if err != nil || !ok {
			t.Errorf("应判定为持有，实际 ok=%v err=%v", ok, err)
		}
	})

	t.Run("未持有该权限", func(t *testing.T) {
		withRoleResolver(t, stubRoleResolver{codes: []string{"editor"}})
		ok, err := OperatorHoldsPermissions(1, 2003, []string{otherPerm})
		if err != nil {
			t.Fatalf("不应返回系统错误: %v", err)
		}
		if ok {
			t.Error("未持有的权限必须判为 false，否则授权收敛形同虚设")
		}
	})

	t.Run("多权限中有一个未持有即拒绝", func(t *testing.T) {
		withRoleResolver(t, stubRoleResolver{codes: []string{"editor"}})
		ok, err := OperatorHoldsPermissions(1, 2004, []string{heldPerm, otherPerm})
		if err != nil {
			t.Fatalf("不应返回系统错误: %v", err)
		}
		if ok {
			t.Error("必须全部持有才放行")
		}
	})

	t.Run("admin 直接放行", func(t *testing.T) {
		withRoleResolver(t, stubRoleResolver{codes: []string{AdminRoleCode}})
		ok, err := OperatorHoldsPermissions(1, 2005, []string{otherPerm})
		if err != nil || !ok {
			t.Errorf("admin 持有通配策略，应放行，实际 ok=%v err=%v", ok, err)
		}
	})

	t.Run("无角色拒绝", func(t *testing.T) {
		withRoleResolver(t, stubRoleResolver{codes: nil})
		ok, err := OperatorHoldsPermissions(1, 2006, []string{heldPerm})
		if err != nil {
			t.Fatalf("不应返回系统错误: %v", err)
		}
		if ok {
			t.Error("无角色必须判为 false")
		}
	})

	t.Run("角色解析失败返回错误而不是放行", func(t *testing.T) {
		withRoleResolver(t, stubRoleResolver{err: errResolverDown})
		ok, err := OperatorHoldsPermissions(1, 2007, []string{heldPerm})
		if err == nil {
			t.Error("解析失败必须返回错误（由上层转 500），绝不能放行")
		}
		if ok {
			t.Error("解析失败时不能判定为持有")
		}
	})
}

// TestSplitRoleCodes 角色缓存值的解析。
func TestSplitRoleCodes(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"admin", 1},
		{"admin,editor", 2},
		{"admin,,editor", 2}, // 空段被跳过
		{",,", 0},
	}
	for _, tc := range cases {
		if got := splitRoleCodes(tc.in); len(got) != tc.want {
			t.Errorf("splitRoleCodes(%q) 应得 %d 个，实际 %v", tc.in, tc.want, got)
		}
	}
}

// TestAuthRejectsBadHeader 认证中间件的入口校验（不依赖 Redis 的几条路径）。
//
// 更靠后的步骤依赖 Redis（吊销检查 fail-closed），在无 Redis 的测试环境里
// 会以 500 收场，因此这里只断言能确定的部分。
func TestAuthRejectsBadHeader(t *testing.T) {
	cases := []struct {
		name    string
		header  string
		wantMsg string
	}{
		{"缺少 Authorization 头", "", "请先登录"},
		{"方案不是 Bearer", "Basic dXNlcjpwYXNz", "Token格式错误"},
		{"只有 Bearer 没有 token", "Bearer", "Token格式错误"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/__test/auth", Auth(), func(c *gin.Context) { c.Status(http.StatusOK) })

			req := httptest.NewRequest(http.MethodGet, "/__test/auth", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("应返回 401，实际 %d（body=%s）", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.wantMsg) {
				t.Errorf("提示应包含 %q，实际 %s", tc.wantMsg, w.Body.String())
			}
		})
	}
}

// ---- 测试辅助 ----

// errResolverDown 模拟角色解析依赖（Redis / DB）故障
var errResolverDown = errors.New("resolver unavailable")

// databaseDB 取当前测试库（testsupport.NewDB 已注入 database.DB）
func databaseDB(t *testing.T) *gorm.DB {
	t.Helper()
	return database.DB
}

func databaseCreate(t *testing.T, v interface{}) error {
	t.Helper()
	return database.DB.Create(v).Error
}

func countRules(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := database.DB.Model(&CasbinRule{}).Count(&n).Error; err != nil {
		t.Fatalf("统计策略失败: %v", err)
	}
	return n
}

func loadAllRules(t *testing.T) []CasbinRule {
	t.Helper()
	var rules []CasbinRule
	if err := database.DB.Find(&rules).Error; err != nil {
		t.Fatalf("读取策略失败: %v", err)
	}
	return rules
}
