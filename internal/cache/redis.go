package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go-admin/config"

	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client

// ErrNotReady Redis 未初始化。
//
// 生产环境下 Redis 是启动强依赖（main.go 初始化失败即退出），
// 因此这里不会真的发生；但**单元测试**不会去起一个 Redis，
// 若各函数直接解引用 RDB 就会 panic，导致 service 层完全无法单测。
// 返回错误而不是 panic：既让服务层可测，也避免误配置时把进程打挂。
var ErrNotReady = errors.New("redis 未初始化")

// client 返回可用的客户端，未初始化时返回 ErrNotReady
func client() (*redis.Client, error) {
	if RDB == nil {
		return nil, ErrNotReady
	}
	return RDB, nil
}

func Init() error {
	cfg := config.Cfg.Redis
	RDB = redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := RDB.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("连接Redis失败: %w", err)
	}
	return nil
}

func Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	c, err := client()
	if err != nil {
		return err
	}
	return c.Set(ctx, key, value, expiration).Err()
}

func Get(ctx context.Context, key string) (string, error) {
	c, err := client()
	if err != nil {
		return "", err
	}
	return c.Get(ctx, key).Result()
}

func Del(ctx context.Context, keys ...string) error {
	c, err := client()
	if err != nil {
		return err
	}
	return c.Del(ctx, keys...).Err()
}

func Exists(ctx context.Context, keys ...string) (bool, error) {
	c, err := client()
	if err != nil {
		return false, err
	}
	n, err := c.Exists(ctx, keys...).Result()
	return n > 0, err
}

func SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	c, err := client()
	if err != nil {
		return false, err
	}
	return c.SetNX(ctx, key, value, expiration).Result()
}

func Incr(ctx context.Context, key string) (int64, error) {
	c, err := client()
	if err != nil {
		return 0, err
	}
	return c.Incr(ctx, key).Result()
}

func Expire(ctx context.Context, key string, expiration time.Duration) error {
	c, err := client()
	if err != nil {
		return err
	}
	return c.Expire(ctx, key, expiration).Err()
}

// IncrWindow 在固定时间窗口内自增计数，返回自增后的值（首次调用返回 1）。
//
// 窗口从**首次调用**起算，长度固定为 ttl，不做滑动续期。
// 用 SETNX 建键 + INCR 自增，而不是「INCR 之后再 EXPIRE」：
// 后者是两次独立往返，若 INCR 成功而 EXPIRE 失败（网络抖动、主从切换），
// 该键就永远不会过期，对应的 IP/账号会被永久限流，只能人工清 Redis。
// SETNX 把 TTL 与建键合成一次原子操作；键已存在时不影响原有 TTL，
// 因此窗口语义仍是「首次计数起的固定 ttl」。
//
// 供限流类场景复用（登录失败计数、验证码生成频率等）。
func IncrWindow(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if _, err := SetNX(ctx, key, 0, ttl); err != nil {
		return 0, err
	}
	return Incr(ctx, key)
}

// ---- refresh token 相关键 ----
//
// 每个 refresh token 自身是一个键（存储 userID），
// 另外用「用户 -> token 集合」维护索引，用于改密/禁用/登出时批量吊销。
//
// 早前的实现直接删 refresh_token:user:<id>，但这个键从未被写入过 ——
// 写入时用的是 refresh_token:<随机串>。键名不匹配导致吊销静默失效，
// 改密或禁用后旧 refresh token 依然能换发新的 access token。
func RefreshTokenKey(token string) string {
	return "refresh_token:" + token
}

// RefreshTokenSetKey 用户当前有效的 refresh token 集合
func RefreshTokenSetKey(userID uint) string {
	return fmt.Sprintf("refresh_token:user:%d", userID)
}

func SAdd(ctx context.Context, key string, members ...interface{}) error {
	c, err := client()
	if err != nil {
		return err
	}
	return c.SAdd(ctx, key, members...).Err()
}

func SRem(ctx context.Context, key string, members ...interface{}) error {
	c, err := client()
	if err != nil {
		return err
	}
	return c.SRem(ctx, key, members...).Err()
}

func SMembers(ctx context.Context, key string) ([]string, error) {
	c, err := client()
	if err != nil {
		return nil, err
	}
	return c.SMembers(ctx, key).Result()
}

// Token 黑名单：吊销 access token
func RevokeToken(ctx context.Context, token string, expiration time.Duration) error {
	c, err := client()
	if err != nil {
		return err
	}
	return c.Set(ctx, "token:blacklist:"+token, "1", expiration).Err()
}

// IsTokenRevoked 判断 access token 是否已被吊销。
//
// 为什么返回 error 而不是吞掉它：签名只给 bool 时，Redis 异常只能返回 false
// ——「查不到黑名单记录」与「查不了黑名单」被混为一谈，等于 fail-open。
// 运行期 Redis 抖动的那段时间里，所有已登出的 access token 会重新变成有效。
// 安全判定必须能区分这两种情况，由调用方决定拒绝策略（见 middleware.Auth）。
//
// 客户端未就绪（Init 未调用或失败）时同样返回 error 而非 false：
// 与抖动同语义 —— 判定不了就不放行，避免退化成 fail-open。
func IsTokenRevoked(ctx context.Context, token string) (bool, error) {
	c, err := client()
	if err != nil {
		return false, err
	}
	n, err := c.Exists(ctx, "token:blacklist:"+token).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DelByPrefix 按前缀批量删除键。
// 使用 SCAN 分批遍历，避免 KEYS 命令在大 key 空间下阻塞 Redis。
func DelByPrefix(ctx context.Context, prefix string) error {
	c, err := client()
	if err != nil {
		return err
	}

	var cursor uint64
	for {
		keys, next, err := c.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := c.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}
