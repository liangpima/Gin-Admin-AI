package common

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// newCtx 构造一个干净的 gin 上下文（不经过任何中间件）。
func newCtx() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return c
}

// TestTenantIDFromDistinguishesMissingFromZero 是 P1-3 的核心断言。
//
// 「上下文里没有租户信息」与「租户 ID 就是 0」在下游长得一模一样（都是 0），
// 但含义完全相反：
//   - 显式 0：平台级账号，TenantScope(db,0) 不过滤是**预期行为**
//   - 缺失：该路由没经过 Auth 中间件。此时返回 0 会让租户过滤被静默跳过，
//     一个本该只返回本租户数据的接口会返回全表
//
// 把两者区分开，是让「漏挂 Auth」这类错误可被发现的前提。
func TestTenantIDFromDistinguishesMissingFromZero(t *testing.T) {
	t.Run("未设置时 ok=false", func(t *testing.T) {
		c := newCtx()
		if id, ok := TenantIDFrom(c); ok || id != 0 {
			t.Errorf("未设置应返回 (0,false)，实际 (%d,%v)", id, ok)
		}
	})

	t.Run("显式设为 0 时 ok=true", func(t *testing.T) {
		c := newCtx()
		c.Set(ContextKeyTenantID, uint(0))
		id, ok := TenantIDFrom(c)
		if !ok {
			t.Error("显式设置的租户 0 必须能被识别为「已设置」")
		}
		if id != 0 {
			t.Errorf("应为 0，实际 %d", id)
		}
	})

	t.Run("正常租户", func(t *testing.T) {
		c := newCtx()
		c.Set(ContextKeyTenantID, uint(42))
		if id, ok := TenantIDFrom(c); !ok || id != 42 {
			t.Errorf("应返回 (42,true)，实际 (%d,%v)", id, ok)
		}
	})

	t.Run("类型不符视为未设置", func(t *testing.T) {
		c := newCtx()
		c.Set(ContextKeyTenantID, "42") // 有人塞了字符串
		if id, ok := TenantIDFrom(c); ok || id != 0 {
			t.Errorf("类型不符应返回 (0,false)，实际 (%d,%v)", id, ok)
		}
	})
}

// TestGetTenantIDStaysBackwardCompatible GetTenantID 对两种情形都返回 0。
//
// 它被上百处调用，语义不能变（否则平台级账号会突然查不到数据）；
// 区分能力由 TenantIDFrom 提供，GetTenantID 只额外做一件「不可见但有价值」的事：
// 对缺失情形留一条 Error 日志，让漏挂 Auth 的路由表现为一条能指向路由的告警。
func TestGetTenantIDStaysBackwardCompatible(t *testing.T) {
	c := newCtx()
	if got := GetTenantID(c); got != 0 {
		t.Errorf("缺失时应返回 0（兼容既有调用方），实际 %d", got)
	}

	c.Set(ContextKeyTenantID, uint(7))
	if got := GetTenantID(c); got != 7 {
		t.Errorf("应返回 7，实际 %d", got)
	}
}

// TestGetTenantIDLogsMissingOnlyOncePerRoute 同一路由的缺失告警只记一次。
//
// 漏挂 Auth 的路由会命中每个请求，若每次都记，日志会被刷满
// （而日志按大小轮转，刷屏会挤掉真正有用的记录）。
func TestGetTenantIDLogsMissingOnlyOncePerRoute(t *testing.T) {
	// 两次调用不应 panic 且结果一致；告警去重通过 missingTenantWarned 实现，
	// 这里断言该 map 确实记住了这个路由键。
	missingTenantWarned.Delete("GET /__probe/tenant")

	newCtxWithRoute := func() *gin.Context {
		c := newCtx()
		c.Request = httptest.NewRequest("GET", "/__probe/tenant", nil)
		return c
	}

	for i := 0; i < 3; i++ {
		_ = GetTenantID(newCtxWithRoute())
	}
	if _, ok := missingTenantWarned.Load("GET /__probe/tenant"); !ok {
		t.Error("缺失租户的路由应被记入告警去重表，否则会每个请求都刷一条日志")
	}
}
