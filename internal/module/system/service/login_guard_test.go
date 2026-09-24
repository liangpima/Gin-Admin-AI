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

// withLoginFailureLookup 替换失败计数的读取，返回被查询过的键。
//
// 为什么需要它：测试环境不初始化 Redis，cache.GetString 只会走 ErrNotReady
// 分支，「键不存在」这条分支拿不到 —— 而那正是线上最常见的情形。
func withLoginFailureLookup(t *testing.T, values map[string]string, err error) *[]string {
	t.Helper()
	var asked []string
	prev := loginFailureLookup
	loginFailureLookup = func(_ context.Context, key string) (string, bool, error) {
		asked = append(asked, key)
		if err != nil {
			return "", false, err
		}
		v, ok := values[key]
		return v, ok, nil
	}
	t.Cleanup(func() { loginFailureLookup = prev })
	return &asked
}

// TestCheckLoginRateLimitAllowsWhenNoFailureRecorded 「键不存在」必须放行。
//
// 这是本项修复的核心回归用例。redis 的 GET 在键不存在时返回 redis.Nil，
// 而 cache.Get 把它当普通 error 透出；限频侧又把「任何 error」当成
// 「Redis 不可用」并按 fail-closed 拒绝 —— 结果是**任何一次干净登录都返回 500**。
// 键不存在对失败计数而言是最常见的正常状态（还没失败过），绝不能算故障。
func TestCheckLoginRateLimitAllowsWhenNoFailureRecorded(t *testing.T) {
	withLoginFailClosed(t, nil) // 默认策略（fail-closed）下也必须放行
	withLoginFailureLookup(t, nil, nil)

	locked, err := checkLoginRateLimit(context.Background(), "login:fail:ip:203.0.113.9")
	if err != nil {
		t.Fatalf("键不存在不是故障，不应报错: %v", err)
	}
	if locked {
		t.Error("没有任何失败记录时不应判定为已锁定")
	}
}

// TestCheckLoginRateLimitLocksAtThreshold 达到阈值即锁定。
func TestCheckLoginRateLimitLocksAtThreshold(t *testing.T) {
	withLoginFailClosed(t, nil)
	withLoginFailureLookup(t, map[string]string{"login:fail:account:admin": "5"}, nil)

	locked, err := checkLoginRateLimit(context.Background(), "login:fail:account:admin")
	if err != nil {
		t.Fatalf("正常读取不应报错: %v", err)
	}
	if !locked {
		t.Errorf("失败次数达到 %d 应判定为已锁定", maxLoginAttempts)
	}
}

// TestCheckLoginRateLimitBelowThreshold 未达阈值放行。
func TestCheckLoginRateLimitBelowThreshold(t *testing.T) {
	withLoginFailClosed(t, nil)
	withLoginFailureLookup(t, map[string]string{"login:fail:ip:203.0.113.9": "4"}, nil)

	locked, err := checkLoginRateLimit(context.Background(), "login:fail:ip:203.0.113.9")
	if err != nil {
		t.Fatalf("正常读取不应报错: %v", err)
	}
	if locked {
		t.Error("未达阈值不应锁定")
	}
}

// TestCheckLoginRateLimitTreatsCorruptCounterAsLocked 计数被写坏时按已锁定处理。
//
// 不能当作「没有失败」：那等于让一次数据异常直接关掉防护。
func TestCheckLoginRateLimitTreatsCorruptCounterAsLocked(t *testing.T) {
	withLoginFailClosed(t, nil)
	withLoginFailureLookup(t, map[string]string{"login:fail:ip:203.0.113.9": "not-a-number"}, nil)

	locked, err := checkLoginRateLimit(context.Background(), "login:fail:ip:203.0.113.9")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !locked {
		t.Error("计数不是整数时应按已达上限处理")
	}
}

// TestCheckLoginRateLimitChecksEveryDimension 任一维度超限都要拒绝。
//
// 只查第一个维度的话，攻击者只要绕开 IP 维度（换 IP）就能无限撞同一个账号。
func TestCheckLoginRateLimitChecksEveryDimension(t *testing.T) {
	withLoginFailClosed(t, nil)
	asked := withLoginFailureLookup(t, map[string]string{"login:fail:account:admin": "5"}, nil)

	locked, err := checkLoginRateLimit(context.Background(), loginRateLimitKeys("203.0.113.9", "admin")...)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !locked {
		t.Error("账号维度已达上限时必须拒绝")
	}
	if len(*asked) != 2 {
		t.Errorf("应查询 IP 与账号两个维度，实际查询了 %v", *asked)
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
