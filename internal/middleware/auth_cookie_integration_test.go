package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-admin/config"
	"go-admin/internal/authcookie"
	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/testsupport"
	"go-admin/pkg/auth"

	"github.com/gin-gonic/gin"
)

// B1（后端双读 cookie）的端到端验证。
//
// 为什么要走到真中间件而不止测 extractToken：extractToken 只回答
// 「token 取出来没有」，而这一项的验收标准是「**带着 cookie 能把接口打通**」——
// 中间还隔着吊销检查（fail-closed）、JWT 解析、平台级身份校验三关，
// 任何一关把 cookie 认证的请求挡下来，功能都是不成立的。
//
// 依赖真实 Redis（吊销检查走不通就直接 500，这是有意的 fail-closed）。
// 连不上 Redis 时 WithTestRedis 会 t.Skip，因此本用例在没起 Redis 的机器上
// 不会误报红；CI 里 ci.yml 起了 redis service，是真跑的。
func TestAuthMiddlewareAcceptsCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testsupport.WithTestRedis(t)

	// JWT 与有效期由 config 全局提供，用例里显式设定并还原
	prevCfg := config.Cfg
	config.Cfg.JWT.Secret = "test-secret-for-cookie-transport"
	config.Cfg.JWT.AccessExpire = 3600
	t.Cleanup(func() { config.Cfg = prevCfg })

	// tenantID 用非 0 值：tenantID==0 会触发平台级身份校验（要求 admin 角色），
	// 那是另一条用例的职责，这里不该被它干扰
	token, err := auth.GenerateAccessToken(42, "cookie-user", 1, 1)
	if err != nil {
		t.Fatalf("签发测试 token 失败: %v", err)
	}

	// 造一个能读到上下文的最小引擎
	doRequest := func(r *gin.Engine, withHeader, withCookie bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/probe", nil)
		if withHeader {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if withCookie {
			req.AddCookie(&http.Cookie{Name: authcookie.AccessCookieName, Value: token})
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	cases := []struct {
		name       string
		transport  string
		withHeader bool
		withCookie bool
		wantOK     bool
		wantSource string
	}{
		{"header 模式：头可用", config.TokenTransportHeader, true, false, true, common.TokenSourceHeader},
		{"header 模式：只有 cookie 必须拒绝（回滚开关要真的回滚）",
			config.TokenTransportHeader, false, true, false, ""},
		{"both 模式：只有 cookie 可用", config.TokenTransportBoth, false, true, true, common.TokenSourceCookie},
		{"both 模式：头优先", config.TokenTransportBoth, true, true, true, common.TokenSourceHeader},
		{"both 模式：两者都没有 → 401", config.TokenTransportBoth, false, false, false, ""},
		{"cookie 模式：只有 cookie 可用", config.TokenTransportCookie, false, true, true, common.TokenSourceCookie},
		{"cookie 模式：只有头必须拒绝（Swagger/curl 会因此失效，故不能做默认值）",
			config.TokenTransportCookie, true, false, false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config.Cfg.Security.TokenTransport = tc.transport
			r := gin.New()
			var captured *gin.Context
			r.GET("/probe", Auth(), func(c *gin.Context) {
				captured = c
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			w := doRequest(r, tc.withHeader, tc.withCookie)

			if tc.wantOK {
				if w.Code != http.StatusOK {
					t.Fatalf("应放行，实际 %d（body=%s）", w.Code, w.Body.String())
				}
				if captured == nil {
					t.Fatal("处理函数应被调用")
				}
				// 上下文里的这两项是下游（CSRF 豁免判定、登出拉黑）的输入，
				// 放行却不写入等于把问题推给下一个中间件
				if got := captured.GetString(common.ContextKeyTokenSource); got != tc.wantSource {
					t.Errorf("ContextKeyTokenSource = %q，期望 %q", got, tc.wantSource)
				}
				if got := captured.GetString(common.ContextKeyAccessToken); got != token {
					t.Errorf("ContextKeyAccessToken 应保存 token 原文（登出要拿它拉黑）")
				}
				if got := common.GetCurrentUserID(captured); got != 42 {
					t.Errorf("应解析出 userID=42，实际 %d", got)
				}
			} else {
				if w.Code != http.StatusUnauthorized {
					t.Fatalf("应拒绝并返回 401，实际 %d（body=%s）", w.Code, w.Body.String())
				}
				if captured != nil {
					t.Error("拒绝时处理函数**绝不能**被执行 —— 只断言状态码会漏掉「先放行再报错」")
				}
			}
		})
	}
}

// TestAuthMiddlewareRejectsRevokedCookieToken 走 cookie 认证的请求同样要过吊销检查。
//
// 双读改造最容易出的漏洞是「新增的那条读路径绕过了吊销」——
// 老的 header 路径查黑名单，新的 cookie 路径忘了查，于是登出后
// 浏览器里的 cookie 还能继续用。这里显式钉住它。
func TestAuthMiddlewareRejectsRevokedCookieToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testsupport.WithTestRedis(t)

	prevCfg := config.Cfg
	config.Cfg.JWT.Secret = "test-secret-for-cookie-transport"
	config.Cfg.JWT.AccessExpire = 3600
	config.Cfg.Security.TokenTransport = config.TokenTransportBoth
	t.Cleanup(func() { config.Cfg = prevCfg })

	token, err := auth.GenerateAccessToken(43, "revoked-user", 1, 1)
	if err != nil {
		t.Fatalf("签发测试 token 失败: %v", err)
	}
	if err := cache.RevokeToken(context.Background(), token, time.Minute); err != nil {
		t.Fatalf("写入吊销标记失败: %v", err)
	}
	t.Cleanup(func() { _ = cache.Del(context.Background(), "token:blacklist:"+token) })

	r := gin.New()
	reached := false
	r.GET("/probe", Auth(), func(c *gin.Context) {
		reached = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.AddCookie(&http.Cookie{Name: authcookie.AccessCookieName, Value: token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("已吊销的 token 走 cookie 也必须被拒绝，实际 %d（body=%s）", w.Code, w.Body.String())
	}
	if reached {
		t.Error("已吊销的请求不能到达处理函数")
	}
}
