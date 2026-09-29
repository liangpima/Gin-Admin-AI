package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"

	"go-admin/config"
	"go-admin/internal/common"
	"go-admin/internal/middleware"
	"go-admin/internal/module/payment/model"
	systemModel "go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"
	pkgAuth "go-admin/pkg/auth"

	"github.com/gin-gonic/gin"
)

// 本文件是**端到端链路测试**（P1-4）：请求从真实 HTTP 进来，走完
// DrainBody → BodyLimit → Recovery → Logger → Cors → Tenant →
// Auth → CSRF → CasbinAuth → OperationLog → Controller → Service →
// Repository（真实 SQLite）的全链路，断言出口行为与落库结果。
//
// 为什么单测覆盖不了这类问题：各层单测都在 mock 相邻层，
// 「层与层的约定」（Auth 写进上下文的值被下游读到、Casbin 的权限码
// 与路由登记表一致、租户隔离贯穿到真实 SQL）只有真启动才测得到。
//
// 场景选**支付链路**：它是本项目唯一涉及钱的链路，也是唯一同时具有
// 「已鉴权入口」与「公开回调入口」两类端点的模块 ——
// 两类入口的防护行为都必须在链路级验证。
//
// 刻意的取舍：
//   - token_transport=header（不下发 cookie）→ CSRF 中间件整体短路。
//     CSRF 的豁免表在 middleware 有专属单测穷举，E2E 不重复它；
//     这里走 Authorization 头是最省噪音的真实路径。
//   - 回调链路的「签名通过」分支需要真实商户证书，不在 E2E 范围
//     （属第三方契约，本地无法构造）；这里验证的是它的**负向面**：
//     匿名伪造报文必须被解析层拒绝且不触碰订单状态。
//   - 每条用例独立起链路（而不是共享 engine）：包级可变状态
//     （config.Cfg / database.DB / casbin / roleResolver）由各用例
//     save & restore，避免用例间污染 —— 本包已有测试因共享状态吃过亏。

const (
	e2eTenantID = 1
	e2eRole     = "pay-admin"
)

// e2eUserIDSeq 每条用例发一个唯一的 userID。
//
// 角色缓存的键是 rbac:roles:<tenant>:<user> 且 TTL 挂在 Redis 上 ——
// 各用例共用同一个 ID 时，前一条用例解析出的角色会被后一条**直接从缓存命中**
// （本地与 CI 的 Redis 都是常驻的），负向用例（无授权角色）会拿到正向用例
// 的角色缓存，403 变 200。真实生产里同样存在这个 TTL 窗口，
// 但那是「角色变更延迟生效」的已知语义；测试里必须把它隔离掉。
var e2eUserIDSeq atomic.Uint64

type roleResolverFunc func(tenantID, userID uint) ([]string, error)

func (f roleResolverFunc) RoleCodesOf(tenantID, userID uint) ([]string, error) {
	return f(tenantID, userID)
}

// setupPaymentChain 起一条真实支付链路。
// 返回 gin 引擎（供匿名请求用例复用）与「带认证发请求」的 helper，
// 以及数据库句柄（供直接落库/读回断言）。
// overrideRole 可选：覆盖注入的角色列表（缺省 e2eRole）。
// 传一个未授权的角色名即可验证「无权限 → 403」这条负向链路。
func setupPaymentChain(t *testing.T, overrideRole ...string) (*gin.Engine, func(method, path, body string) (*httptest.ResponseRecorder, *common.Response), *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// ── config.Cfg：save & restore ──
	prevCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = prevCfg })
	config.Cfg.JWT.Secret = "e2e-test-secret-not-a-real-credential-0123456789"
	config.Cfg.JWT.AccessExpire = 3600
	config.Cfg.JWT.RefreshExpire = 7200
	config.Cfg.Security.TokenTransport = config.TokenTransportHeader

	// ── 数据库：真实 SQLite ──
	db := testsupport.NewDB(t,
		&model.PayOrder{},
		&middleware.CasbinRule{},
		&systemModel.SysRole{},
		&systemModel.SysMenu{},
		&systemModel.SysRoleMenu{},
	)

	// ── Casbin：走**真实策略构建路径** ──
	// 种「角色 + 按钮菜单 + 角色-菜单关联」，再让 InitCasbin 内部的
	// SyncPoliciesFromRoleMenus 从授权关系构建策略 —— 而不是手插
	// casbin_rule。这样链路验证覆盖到「菜单授权 → 策略生成」这一环，
	// 与生产的数据流完全一致。
	role := systemModel.SysRole{Code: e2eRole, Name: "E2E支付角色", Status: 1}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("创建角色失败: %v", err)
	}
	payPerms := []string{
		"payment:order:create", "payment:order:list",
		"payment:order:close", "payment:order:refund",
	}
	for i, code := range payPerms {
		menu := systemModel.SysMenu{
			Name: fmt.Sprintf("PayBtn%d", i), Title: code, Type: 2,
			Permission: code, Status: 1,
		}
		if err := db.Create(&menu).Error; err != nil {
			t.Fatalf("创建菜单失败: %v", err)
		}
		if err := db.Create(&systemModel.SysRoleMenu{RoleID: role.ID, MenuID: menu.ID}).Error; err != nil {
			t.Fatalf("创建角色菜单关联失败: %v", err)
		}
	}

	// ── Casbin enforcer（内部会执行 SyncPoliciesFromRoleMenus）──
	if err := middleware.InitCasbin("../config/casbin/model.conf"); err != nil {
		t.Fatalf("初始化 casbin 失败: %v", err)
	}

	// ── 角色解析：注入测试 resolver（生产由 system 模块在启动时注入）──
	// 生产默认是 nil（fail-closed：解析失败按无角色拒绝），恢复 nil 即还原
	resolvedRole := e2eRole
	if len(overrideRole) > 0 {
		resolvedRole = overrideRole[0]
	}
	middleware.SetRoleResolver(roleResolverFunc(func(tenantID, userID uint) ([]string, error) {
		return []string{resolvedRole}, nil
	}))
	t.Cleanup(func() { middleware.SetRoleResolver(nil) })

	// ── Redis：吊销检查 / 角色缓存是链路的真实一环（不可用则 skip，与既有约定一致）──
	testsupport.WithTestRedis(t)

	// ── 真实路由 ──
	engine := Setup(gin.TestMode)

	userID := uint(e2eUserIDSeq.Add(777))
	token, err := pkgAuth.GenerateAccessToken(userID, "e2e-admin", e2eTenantID, 1)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}

	call := func(method, path, body string) (*httptest.ResponseRecorder, *common.Response) {
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		resp := &common.Response{}
		_ = json.Unmarshal(w.Body.Bytes(), resp) // 非 JSON 响应保持零值，由用例自行断言
		return w, resp
	}
	return engine, call, db
}

// ─────────────────────────── 用例 ───────────────────────────

// TestPaymentChainUnauthenticatedRejected 未认证请求必须在 Auth 层被拒。
// 公开回调之外的所有支付端点都挂在 Auth 后面 —— 这条断言一旦失效，
// 等于支付操作向全网匿名开放。
func TestPaymentChainUnauthenticatedRejected(t *testing.T) {
	engine, _, _ := setupPaymentChain(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/pay/order",
		strings.NewReader(`{"subject":"匿名","amount":1,"channel":"wechat"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("未认证请求不应成功，HTTP %d body=%s", w.Code, w.Body.String())
	}
}

// TestPaymentChainCreateOrderHappyPath 已认证 + 有权限：下单全链路。
// 断言三件事：HTTP 语义（200 + code=0）、响应体带订单号、
// **真实落库**且租户/初始状态正确 —— 最后一件只有 E2E 测得到。
func TestPaymentChainCreateOrderHappyPath(t *testing.T) {
	_, call, db := setupPaymentChain(t)

	w, resp := call(http.MethodPost, "/api/v1/system/pay/order",
		`{"subject":"E2E会员年卡","body":"链路测试","amount":9900,"channel":"wechat"}`)

	if w.Code != http.StatusOK || resp.Code != common.CodeSuccess {
		t.Fatalf("下单应成功，HTTP %d body=%s", w.Code, w.Body.String())
	}
	data, _ := resp.Data.(map[string]interface{})
	orderNo, _ := data["orderNo"].(string)
	if orderNo == "" {
		t.Fatalf("响应应携带 orderNo，实际: %v", resp.Data)
	}

	var order model.PayOrder
	if err := db.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		t.Fatalf("订单应已真实落库: %v", err)
	}
	if order.TenantID != e2eTenantID {
		t.Errorf("落库租户应为 %d，实际 %d", e2eTenantID, order.TenantID)
	}
	if order.Status != model.StatusPending {
		t.Errorf("初始状态应为待支付(%d)，实际 %d", model.StatusPending, order.Status)
	}
	if order.Amount != 9900 {
		t.Errorf("金额应为 9900 分，实际 %d", order.Amount)
	}
	// 测试环境没有真实商户配置：Prepay 必然失败，但失败被收敛进 payError
	// 字段且**不影响订单落库** —— 「下单」与「发起支付」解耦的正确表现。
	if _, ok := data["payError"]; !ok {
		t.Log("此环境无 WeChat 商户配置但响应无 payError —— 若网关配置意外可用请检查测试隔离")
	}
}

// TestPaymentChainTenantIsolationOnList 列表租户隔离贯穿到真实 SQL。
// 直接往库里插一条**别的租户**的订单，再走 HTTP 列表查询 ——
// 只有 E2E 能证明「租户过滤在真实查询里生效」而不是 mock 里的假设。
func TestPaymentChainTenantIsolationOnList(t *testing.T) {
	_, call, db := setupPaymentChain(t)

	w, resp := call(http.MethodPost, "/api/v1/system/pay/order",
		`{"subject":"本租户订单","amount":500,"channel":"alipay"}`)
	if w.Code != http.StatusOK || resp.Code != common.CodeSuccess {
		t.Fatalf("前置下单失败: %s", w.Body.String())
	}

	// 直接落库一条租户 2 的订单（绕过 HTTP，模拟另一租户的既有数据）
	other := model.PayOrder{
		TenantID: 2,
		OrderNo:  "PAY-T2-OTHER-TENANT",
		Subject:  "他租户订单",
		Amount:   500,
		Channel:  "alipay",
		Status:   model.StatusPaid,
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("造跨租户数据失败: %v", err)
	}

	w, resp = call(http.MethodGet, "/api/v1/system/pay/order/list", "")
	if w.Code != http.StatusOK || resp.Code != common.CodeSuccess {
		t.Fatalf("列表查询失败: %s", w.Body.String())
	}
	raw, _ := json.Marshal(resp.Data)
	if strings.Contains(string(raw), "PAY-T2-OTHER-TENANT") {
		t.Errorf("列表泄漏了他租户订单: %s", raw)
	}
	if !strings.Contains(string(raw), "本租户订单") {
		t.Errorf("列表应包含本租户订单: %s", raw)
	}
}

// TestPaymentChainCloseOrder 关闭订单全链路：HTTP → 条件更新 → 落库。
func TestPaymentChainCloseOrder(t *testing.T) {
	_, call, db := setupPaymentChain(t)

	_, resp := call(http.MethodPost, "/api/v1/system/pay/order",
		`{"subject":"待关闭","amount":100,"channel":"wechat"}`)
	data, _ := resp.Data.(map[string]interface{})
	orderNo, _ := data["orderNo"].(string)
	if orderNo == "" {
		t.Fatalf("前置下单失败: %v", resp.Data)
	}

	w, resp := call(http.MethodPost, "/api/v1/system/pay/order/close",
		fmt.Sprintf(`{"orderNo":%q}`, orderNo))
	if w.Code != http.StatusOK || resp.Code != common.CodeSuccess {
		t.Fatalf("关闭应成功: HTTP %d body=%s", w.Code, w.Body.String())
	}

	var order model.PayOrder
	if err := db.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		t.Fatalf("读回订单失败: %v", err)
	}
	if order.Status != model.StatusClosed {
		t.Errorf("落库状态应为已关闭(%d)，实际 %d", model.StatusClosed, order.Status)
	}
}

// TestPaymentChainNotifyEndpointRejectsGarbage 公开回调入口的负向 E2E：
// 无签名/垃圾报文必须被解析层拒绝，且**不触碰任何订单状态**。
// 回调是全网匿名可打的 —— 它的防护只在链路级验证才有意义。
func TestPaymentChainNotifyEndpointRejectsGarbage(t *testing.T) {
	engine, call, db := setupPaymentChain(t)

	// 先造一个订单作为「受害者」，断言伪造回调被拒后状态原封不动
	_, resp := call(http.MethodPost, "/api/v1/system/pay/order",
		`{"subject":"回调目标","amount":100,"channel":"wechat"}`)
	data, _ := resp.Data.(map[string]interface{})
	orderNo, _ := data["orderNo"].(string)
	if orderNo == "" {
		t.Fatalf("前置下单失败: %v", resp.Data)
	}

	for _, body := range []string{
		`{"order_no":"` + orderNo + `","status":"SUCCESS"}`, // 伪造的 JSON 报文
		"not-json-at-all", // 纯垃圾
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/pay/notify/wechat", strings.NewReader(body))
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		// 渠道协议要求回调响应恒为 HTTP 200，失败用业务码 FAIL 表达
		if w.Code != http.StatusOK {
			t.Errorf("回调响应应恒为 200，实际 %d（body=%q 场景）", w.Code, body)
		}
		if !strings.Contains(w.Body.String(), "FAIL") {
			t.Errorf("垃圾报文应返回 FAIL，实际: %s", w.Body.String())
		}
	}

	var order model.PayOrder
	if err := db.Where("order_no = ?", orderNo).First(&order).Error; err != nil {
		t.Fatalf("读回订单失败: %v", err)
	}
	if order.Status != model.StatusPending {
		t.Errorf("伪造回调不得改变订单状态，期望待支付(%d) 实际 %d",
			model.StatusPending, order.Status)
	}
	if order.TradeNo != "" {
		t.Errorf("伪造回调不得写入交易号，实际 %q", order.TradeNo)
	}
}

// TestPaymentChainPermissionDeniedWithoutGrant 未授权角色必须被 CasbinAuth 拦下。
//
// 这条是 happy path 的**区分力证明**：若 CasbinAuth 形同虚设（比如策略匹配
// 恒真、或角色解析失败被当成放行），上面的 200 就毫无意义。这里让角色解析
// 返回一个**未被授权任何支付权限**的角色 —— 一切照旧却必须 403。
func TestPaymentChainPermissionDeniedWithoutGrant(t *testing.T) {
	_, call, _ := setupPaymentChain(t, "rbac-guest")

	w, resp := call(http.MethodPost, "/api/v1/system/pay/order",
		`{"subject":"越权尝试","amount":1,"channel":"wechat"}`)

	if w.Code == http.StatusOK && resp.Code == common.CodeSuccess {
		t.Fatalf("未授权角色不应能下单，实际成功: %s", w.Body.String())
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("应返回 HTTP 403，实际 %d body=%s", w.Code, w.Body.String())
	}
}
