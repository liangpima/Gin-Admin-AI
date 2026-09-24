package service

import (
	"context"
	"testing"

	"go-admin/config"
)

// 登录限频（P0-2）的回归测试。
//
// 背景：限频是登录链路上唯一的暴力破解闸门，双维度（IP + 账号）计数都存 Redis。
// 原先 Redis 读取失败时直接放行（fail-open），于是「缓存抖动」会被放大成
// 「可无限撞库」，而攻击者完全可以等待甚至主动制造这个窗口。
// 现在默认 fail-closed，仅允许显式配置退回 fail-open。

// withLoginFailClosed 临时改写 fail-closed 配置并在用例结束时还原。
// nil 表示「配置里没写这一项」，用于验证代码侧的默认值。
func withLoginFailClosed(t *testing.T, v *bool) {
	t.Helper()
	prev := config.Cfg.Security.LoginFailClosed
	config.Cfg.Security.LoginFailClosed = v
	t.Cleanup(func() { config.Cfg.Security.LoginFailClosed = prev })
}

// TestCheckLoginRateLimitFailClosedByDefault 未配置该项时必须默认拒绝。
//
// 这是本项修复的核心：Go 的 bool 零值恰好等于「不安全的那一侧」，
// 若用普通 bool，漏配就等于关掉防护。用例通过「不配置（nil）」来钉住默认值。
func TestCheckLoginRateLimitFailClosedByDefault(t *testing.T) {
	withLoginFailClosed(t, nil)

	// 测试环境不初始化 Redis，cache.Get 会返回 ErrNotReady
	locked, err := checkLoginRateLimit(context.Background(), "login:fail:ip:203.0.113.9")
	if err == nil {
		t.Fatal("限频设施不可用时必须拒绝本次登录（默认 fail-closed）")
	}
	if locked {
		t.Error("设施故障应以 error 表达（系统错误 500），而不是伪装成「失败次数过多」")
	}
}

// TestCheckLoginRateLimitFailOpenWhenConfigured 显式配置 false 时退回可用性优先。
func TestCheckLoginRateLimitFailOpenWhenConfigured(t *testing.T) {
	off := false
	withLoginFailClosed(t, &off)

	locked, err := checkLoginRateLimit(context.Background(), "login:fail:ip:203.0.113.9")
	if err != nil {
		t.Fatalf("显式配置 fail-open 时不应报错: %v", err)
	}
	if locked {
		t.Error("fail-open 时不应判定为已锁定")
	}
}

// TestLoginRateLimitKeysCoverBothDimensions 限频必须同时按 IP 与账号计数。
//
// 单维度都可被绕过：只按 IP 计数则攻击者换 IP 即可；只按账号计数则
// 可以用大量账号撞库而不触发限制。
func TestLoginRateLimitKeysCoverBothDimensions(t *testing.T) {
	keys := loginRateLimitKeys("203.0.113.9", "admin")
	if len(keys) != 2 {
		t.Fatalf("应为 IP + 账号两个维度，实际 %v", keys)
	}
	if keys[0] != "login:fail:ip:203.0.113.9" {
		t.Errorf("IP 维度键不符合预期: %q", keys[0])
	}
	if keys[1] != "login:fail:account:admin" {
		t.Errorf("账号维度键不符合预期: %q", keys[1])
	}
}
