package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go-admin/config"

	"github.com/gin-gonic/gin"
)

// 可信代理配置的回归测试（P0-2）。
//
// 背景：c.ClientIP() 是登录失败限频、登录日志、操作日志的 IP 来源。
// gin 默认信任所有代理头，于是客户端只要每次伪造一个新的 X-Forwarded-For，
// 就能把「单 IP 连续 5 次失败即锁定」变成无限次尝试。
// 这里用真实路由验证 Setup 的接线：默认忽略代理头，配置后才采信。
//
// 顺带补上 router 包的测试空白 —— 它此前 0 测试，却是权限登记的中枢。

// probeClientIP 用 Setup 建出的引擎跑一次请求，返回 handler 看到的 ClientIP。
func probeClientIP(t *testing.T, trustedProxies []string, remoteAddr, forwardedFor string) string {
	t.Helper()

	prev := config.Cfg.Server.TrustedProxies
	config.Cfg.Server.TrustedProxies = trustedProxies
	t.Cleanup(func() { config.Cfg.Server.TrustedProxies = prev })

	r := Setup(gin.TestMode)
	r.GET("/__probe/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

	req := httptest.NewRequest(http.MethodGet, "/__probe/ip", nil)
	req.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Body.String()
}

// TestSetupIgnoresForwardedForByDefault 默认（白名单为空）不得采信代理头。
func TestSetupIgnoresForwardedForByDefault(t *testing.T) {
	got := probeClientIP(t, nil, "203.0.113.9:12345", "1.2.3.4")
	if got != "203.0.113.9" {
		t.Errorf("默认配置下 ClientIP 应取连接对端地址，实际 %q —— 伪造 X-Forwarded-For 会绕过 IP 限频", got)
	}
}

// TestSetupRespectsForwardedForFromTrustedProxy 白名单内的代理才采信代理头。
func TestSetupRespectsForwardedForFromTrustedProxy(t *testing.T) {
	got := probeClientIP(t, []string{"10.0.0.0/8"}, "10.1.2.3:12345", "1.2.3.4")
	if got != "1.2.3.4" {
		t.Errorf("来自可信代理的 X-Forwarded-For 应被采信（否则 nginx 后面的部署会按代理 IP 计数，全站共用一个限额），实际 %q", got)
	}
}

// TestSetupIgnoresForwardedForFromUntrustedPeer 白名单外的对端不得采信代理头。
//
// 这是最容易配错的场景：配了白名单就以为万事大吉，但攻击者是直连的，
// 它的 X-Forwarded-For 必须被忽略。
func TestSetupIgnoresForwardedForFromUntrustedPeer(t *testing.T) {
	got := probeClientIP(t, []string{"10.0.0.0/8"}, "203.0.113.9:12345", "1.2.3.4")
	if got != "203.0.113.9" {
		t.Errorf("非可信对端的 X-Forwarded-For 必须忽略，实际 %q", got)
	}
}
