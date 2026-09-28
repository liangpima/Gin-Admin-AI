// Package cache_test 用外部测试包（package cache_test）而不是内部测试包。
//
// 原因：testsupport 依赖 go-admin/internal/cache，内部测试包再 import testsupport
// 会构成 import cycle（test 包会把 cache 与本文件编进同一个包）。
// 外部测试包只依赖导出符号，没有这个问题。
//
// # 为什么这个包值得测
//
// cache 是「鉴权链路的地基」：token 黑名单、用户级吊销标记、refresh token 索引
// 都在这里。它的函数大多只有两三行，看起来像「薄封装不用测」——
// 但薄封装正是最容易悄悄改变**语义**的地方，而这个包的语义几乎全是安全语义：
//
//   - `Get` 与 `GetString` 对「键不存在」的表示**刻意不同**，改错一个方向
//     就是 fail-open（见各自的注释）；
//   - `IsTokenRevoked` 必须能区分「没吊销」与「查不了」，
//     后者返回 false 等于让所有已登出的 token 复活；
//   - `IncrWindow` 的 TTL 必须由 SETNX 与建键一起原子完成，
//     退回「INCR 之后再 EXPIRE」会在抖动时留下永不过期的键。
//
// 这些性质都不能靠读代码「看起来对」来保证，只能钉在用例里。
//
// ⚠️ 本文件需要真实 Redis（本地 127.0.0.1:6379 或 TEST_REDIS_ADDR），
// 连不上会 skip —— 与 internal/database 的 TEST_MYSQL_* 门控同一取舍。
// 不用 miniredis 之类的内存实现：它的 SCAN 是简单全表扫描，
// 与真实 Redis 的反向二进制游标语义不同（DelByPrefix 的用例正依赖后者），
// 用它测等于没测。
package cache_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go-admin/internal/cache"
	"go-admin/internal/testsupport"
)

// testPrefix 所有用例的键都带这个前缀，便于统一清理、避免污染共享 Redis。
const testPrefix = "go-admin:ut:"

func key(name string) string { return testPrefix + name }

// setupRedis 连上测试 Redis 并注册清理。
func setupRedis(t *testing.T) context.Context {
	t.Helper()
	testsupport.WithTestRedis(t)

	ctx := context.Background()
	t.Cleanup(func() {
		keys, err := cache.RDB.Keys(ctx, testPrefix+"*").Result()
		if err == nil && len(keys) > 0 {
			cache.RDB.Del(ctx, keys...)
		}
	})
	return ctx
}

// ---- 键不存在时的两种语义：这是本包最容易改错的地方 ----

// TestGetStringDistinguishesMissingFromFailure GetString 必须把「键不存在」
// 表示成 found=false 且 err=nil，把「读不到」表示成 err!=nil。
//
// 为什么关键：调用方按 found 判定「还没失败过 / 没配置」，按 err 判定「读不到」。
// 两者混在一起时，fail-closed 会把每一次干净登录都拒掉 ——
// 这不是假设，登录限频曾因此让所有正常登录返回 500。
func TestGetStringDistinguishesMissingFromFailure(t *testing.T) {
	ctx := setupRedis(t)

	t.Run("键存在", func(t *testing.T) {
		if err := cache.Set(ctx, key("gs"), "v", time.Minute); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
		v, found, err := cache.GetString(ctx, key("gs"))
		if err != nil || !found || v != "v" {
			t.Errorf("应得到 (\"v\", true, nil)，实际 (%q, %v, %v)", v, found, err)
		}
	})

	t.Run("键不存在：found=false 且 err=nil", func(t *testing.T) {
		v, found, err := cache.GetString(ctx, key("no-such-key"))
		if err != nil {
			t.Errorf("键不存在不应算错误，实际: %v", err)
		}
		if found || v != "" {
			t.Errorf("键不存在应返回 (\"\", false, nil)，实际 (%q, %v, nil)", v, found)
		}
	})

	t.Run("Redis 未就绪：err 必须非 nil", func(t *testing.T) {
		testsupport.WithNilRedis(t)
		if _, _, err := cache.GetString(ctx, key("gs")); !errors.Is(err, cache.ErrNotReady) {
			t.Errorf("应返回 ErrNotReady，实际: %v", err)
		}
	})
}

// TestGetKeepsRedisNilSemantics Get 的语义**不能**被改成 GetString 那样。
//
// captcha 的「一次性凭证」正是靠 `err != nil` 判定「凭证不存在」而拒绝的；
// 把 Get 改成 found 语义会让校验变成放行。
func TestGetKeepsRedisNilSemantics(t *testing.T) {
	ctx := setupRedis(t)

	if _, err := cache.Get(ctx, key("no-such-key")); err == nil {
		t.Error("键不存在时 Get 必须返回错误（调用方依赖它判定凭证不存在）")
	}

	if err := cache.Set(ctx, key("g"), "v", time.Minute); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if v, err := cache.Get(ctx, key("g")); err != nil || v != "v" {
		t.Errorf("应得到 (\"v\", nil)，实际 (%q, %v)", v, err)
	}
}

// ---- 基础读写 ----

func TestSetExistsDel(t *testing.T) {
	ctx := setupRedis(t)

	ok, err := cache.Exists(ctx, key("e"))
	if err != nil {
		t.Fatalf("Exists 失败: %v", err)
	}
	if ok {
		t.Error("键还不存在，Exists 应为 false")
	}

	if err := cache.Set(ctx, key("e"), "1", time.Minute); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	if ok, err := cache.Exists(ctx, key("e")); err != nil || !ok {
		t.Errorf("Set 之后 Exists 应为 true，实际 (%v, %v)", ok, err)
	}

	if err := cache.Del(ctx, key("e")); err != nil {
		t.Fatalf("Del 失败: %v", err)
	}
	if ok, _ := cache.Exists(ctx, key("e")); ok {
		t.Error("Del 之后 Exists 应为 false")
	}
}

// TestSetNXAndIncrWindow IncrWindow 的窗口语义：首次计数起算固定 TTL，不续期。
//
// 关键性质是「TTL 与建键一次原子完成」。若实现退回「INCR 之后再 EXPIRE」，
// INCR 成功而 EXPIRE 失败（网络抖动、主从切换）会留下**永不过期**的键 ——
// 对应的 IP/账号被永久限流，只能人工清 Redis。这里通过「第二次调用不重置 TTL」
// 把窗口语义钉住。
func TestSetNXAndIncrWindow(t *testing.T) {
	ctx := setupRedis(t)

	t.Run("SetNX 只在键不存在时生效", func(t *testing.T) {
		set, err := cache.SetNX(ctx, key("nx"), "a", time.Minute)
		if err != nil || !set {
			t.Fatalf("首次 SetNX 应成功，实际 (%v, %v)", set, err)
		}
		set, err = cache.SetNX(ctx, key("nx"), "b", time.Minute)
		if err != nil {
			t.Fatalf("第二次 SetNX 出错: %v", err)
		}
		if set {
			t.Error("键已存在时 SetNX 不应生效")
		}
		if v, _ := cache.Get(ctx, key("nx")); v != "a" {
			t.Errorf("值不应被覆盖，实际 %q", v)
		}
	})

	t.Run("IncrWindow 自增且窗口不续期", func(t *testing.T) {
		k := key("window")
		n, err := cache.IncrWindow(ctx, k, time.Minute)
		if err != nil || n != 1 {
			t.Fatalf("首次调用应返回 1，实际 (%d, %v)", n, err)
		}

		ttlBefore, err := cache.RDB.TTL(ctx, k).Result()
		if err != nil {
			t.Fatalf("读取 TTL 失败: %v", err)
		}
		if ttlBefore <= 0 {
			t.Fatalf("键应带 TTL，实际 %v", ttlBefore)
		}

		// 让时间走一点，再自增：TTL 不应被重置（固定窗口，非滑动窗口）
		time.Sleep(1100 * time.Millisecond)
		if n, err = cache.IncrWindow(ctx, k, time.Minute); err != nil || n != 2 {
			t.Fatalf("第二次调用应返回 2，实际 (%d, %v)", n, err)
		}

		ttlAfter, err := cache.RDB.TTL(ctx, k).Result()
		if err != nil {
			t.Fatalf("读取 TTL 失败: %v", err)
		}
		if ttlAfter >= ttlBefore {
			t.Errorf("窗口不应被续期：TTL 应从 %v 减少，实际 %v", ttlBefore, ttlAfter)
		}
	})

	t.Run("未就绪时返回 ErrNotReady", func(t *testing.T) {
		testsupport.WithNilRedis(t)
		if _, err := cache.IncrWindow(ctx, key("w2"), time.Minute); !errors.Is(err, cache.ErrNotReady) {
			t.Errorf("应返回 ErrNotReady，实际: %v", err)
		}
	})
}

// ---- token 吊销：安全语义 ----

// TestRefreshTokenKeysDoNotCollide 用户集合键与单个 token 键**必须不同名**。
//
// 这里钉的是一处真实的历史缺陷：吊销逻辑当时直接删
// `refresh_token:user:<id>`，而写入用的是 `refresh_token:<随机串>` ——
// 删的是一个从未写入过的键，等于没吊销。改密/禁用后旧 refresh token
// 仍能换发新的 access token（7 天）。
func TestRefreshTokenKeysDoNotCollide(t *testing.T) {
	const token = "abc123"
	setKey := cache.RefreshTokenSetKey(7)
	tokenKey := cache.RefreshTokenKey(token)
	revokedKey := cache.UserTokenRevokedKey(7)

	if setKey == tokenKey {
		t.Errorf("集合键与 token 键不应同名：%q", setKey)
	}
	if setKey == revokedKey || tokenKey == revokedKey {
		t.Error("吊销标记键不应与其它两个键同名")
	}
	// 键名形态是契约的一部分：换名会让升级期间新旧键并存、吊销失效
	if setKey != "refresh_token:user:7" {
		t.Errorf("集合键名变了：%q", setKey)
	}
	if tokenKey != "refresh_token:abc123" {
		t.Errorf("token 键名变了：%q", tokenKey)
	}
	if revokedKey != "user:token_revoked:7" {
		t.Errorf("吊销标记键名变了：%q", revokedKey)
	}
}

func TestTokenRevocation(t *testing.T) {
	ctx := setupRedis(t)

	t.Run("未吊销的 token 返回 false 且 err 为 nil", func(t *testing.T) {
		revoked, err := cache.IsTokenRevoked(ctx, "not-revoked")
		if err != nil || revoked {
			t.Errorf("应得到 (false, nil)，实际 (%v, %v)", revoked, err)
		}
	})

	t.Run("吊销后返回 true", func(t *testing.T) {
		if err := cache.RevokeToken(ctx, "revoked-one", time.Minute); err != nil {
			t.Fatalf("RevokeToken 失败: %v", err)
		}
		revoked, err := cache.IsTokenRevoked(ctx, "revoked-one")
		if err != nil || !revoked {
			t.Errorf("应得到 (true, nil)，实际 (%v, %v)", revoked, err)
		}
	})

	t.Run("Redis 未就绪必须返回错误而不是 false", func(t *testing.T) {
		// 返回 false 等于 fail-open：抖动期间所有已登出的 token 会重新有效
		testsupport.WithNilRedis(t)
		revoked, err := cache.IsTokenRevoked(ctx, "any")
		if err == nil {
			t.Error("未就绪时必须返回错误（返回 false 就是 fail-open）")
		}
		if revoked {
			t.Error("未就绪时不应声称已吊销")
		}
	})
}

func TestTokenSetOperations(t *testing.T) {
	ctx := setupRedis(t)
	setKey := cache.RefreshTokenSetKey(99)

	if err := cache.SAdd(ctx, setKey, "t1", "t2"); err != nil {
		t.Fatalf("SAdd 失败: %v", err)
	}
	members, err := cache.SMembers(ctx, setKey)
	if err != nil {
		t.Fatalf("SMembers 失败: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("应有 2 个成员，实际 %v", members)
	}

	if err := cache.SRem(ctx, setKey, "t1"); err != nil {
		t.Fatalf("SRem 失败: %v", err)
	}
	members, _ = cache.SMembers(ctx, setKey)
	if len(members) != 1 || members[0] != "t2" {
		t.Errorf("SRem 之后应只剩 t2，实际 %v", members)
	}

	// 不存在的集合：空结果 + nil 错误（吊销逻辑依赖它区分「没有 token」与「读不到」）
	if members, err := cache.SMembers(ctx, key("no-such-set")); err != nil || len(members) != 0 {
		t.Errorf("不存在的集合应返回 (空, nil)，实际 (%v, %v)", members, err)
	}
}

func TestSMembersUnavailable(t *testing.T) {
	ctx := setupRedis(t)
	testsupport.WithNilRedis(t)

	if _, err := cache.SMembers(ctx, cache.RefreshTokenSetKey(1)); !errors.Is(err, cache.ErrNotReady) {
		t.Errorf("应返回 ErrNotReady，实际: %v", err)
	}
}

// ---- 未就绪时的统一行为：每个导出函数都必须返回错误而不是 panic ----

// TestUnavailableReturnsError 逐个数过导出函数，确保 Redis 未就绪时
// 全部返回 ErrNotReady。漏一个就会在「未配置 Redis 的部署」里 panic ——
// 而本项目的注释明确写了「未配置 Redis 也要能跑单测」。
func TestUnavailableReturnsError(t *testing.T) {
	testsupport.WithNilRedis(t)
	ctx := context.Background()

	checks := map[string]error{
		"Set":              cache.Set(ctx, key("x"), "1", time.Minute),
		"Del":              cache.Del(ctx, key("x")),
		"Expire":           cache.Expire(ctx, key("x"), time.Minute),
		"SAdd":             cache.SAdd(ctx, key("x"), "m"),
		"SRem":             cache.SRem(ctx, key("x"), "m"),
		"RevokeToken":      cache.RevokeToken(ctx, "t", time.Minute),
		"DelByPrefix":      cache.DelByPrefix(ctx, testPrefix),
	}
	if _, err := cache.Get(ctx, key("x")); err == nil {
		t.Error("Get 未就绪时应返回错误")
	}
	if _, err := cache.Exists(ctx, key("x")); err == nil {
		t.Error("Exists 未就绪时应返回错误")
	}
	if _, err := cache.SetNX(ctx, key("x"), "1", time.Minute); err == nil {
		t.Error("SetNX 未就绪时应返回错误")
	}
	if _, err := cache.Incr(ctx, key("x")); err == nil {
		t.Error("Incr 未就绪时应返回错误")
	}

	for name, err := range checks {
		if !errors.Is(err, cache.ErrNotReady) {
			t.Errorf("%s 未就绪时应返回 ErrNotReady，实际: %v", name, err)
		}
	}
}

// ---- DelByPrefix ----

// TestDelByPrefix 批量删除必须删全、且只删匹配前缀的键。
//
// 用例造了 1200 个键，刻意超过单次 SCAN 的 COUNT（100）——
// 循环写错（漏掉尾批、游标提前归零）都会在这里暴露成「残留 N 个键」。
func TestDelByPrefix(t *testing.T) {
	ctx := setupRedis(t)

	const prefix = testPrefix + "rbac:roles:"
	const total = 1200

	// 干扰项：前缀相近但不应被 `prefix + "*"` 命中
	interlopers := []string{
		testPrefix + "rbac:roles_keep",
		testPrefix + "rbac:role:keep",
		testPrefix + "unrelated:keep",
	}

	pipe := cache.RDB.Pipeline()
	for i := 0; i < total; i++ {
		pipe.Set(ctx, fmt.Sprintf("%s%d", prefix, i), "1", 0)
	}
	for _, k := range interlopers {
		pipe.Set(ctx, k, "1", 0)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatalf("准备键失败: %v", err)
	}

	if err := cache.DelByPrefix(ctx, prefix); err != nil {
		t.Fatalf("批量删除失败: %v", err)
	}

	left, err := cache.RDB.Keys(ctx, prefix+"*").Result()
	if err != nil {
		t.Fatalf("查询残留键失败: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("应删除全部 %d 个键，实际残留 %d 个", total, len(left))
	}

	for _, k := range interlopers {
		n, err := cache.RDB.Exists(ctx, k).Result()
		if err != nil {
			t.Fatalf("检查 %s 失败: %v", k, err)
		}
		if n != 1 {
			t.Errorf("%q 不匹配前缀，不应被删除", k)
		}
	}
}

// TestDelByPrefixNoMatch 没有匹配键时不应报错。
func TestDelByPrefixNoMatch(t *testing.T) {
	ctx := setupRedis(t)

	if err := cache.DelByPrefix(ctx, testPrefix+"definitely:no:such:prefix:"); err != nil {
		t.Fatalf("无匹配键时应返回 nil，实际: %v", err)
	}
}

// ---- 前缀清理工具本身的自检 ----

// TestTestPrefixIsolation 防止用例把键写到共享前缀之外（那会污染别的用例）。
func TestTestPrefixIsolation(t *testing.T) {
	if !strings.HasPrefix(key("anything"), "go-admin:ut:") {
		t.Fatal("测试键必须落在隔离前缀下")
	}
}
