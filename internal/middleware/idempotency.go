package middleware

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/logger"

	"github.com/gin-gonic/gin"
)

// Idempotency 基于 Redis 的幂等键中间件。
//
// # 解决什么问题
//
// 客户端重试是常态：用户双击按钮、App 网络超时后自动重试、网关重发。
// 对"创建"类接口（下单、支付），重复请求会创建两笔业务记录——
// 服务端无法从请求本身区分「新请求」与「同一次操作的重试」，
// 唯一的依据是客户端声明的操作标识，即幂等键（Idempotency-Key 头）。
//
// # 语义（与 Stripe / 阿里云等公开 API 的通行做法一致）
//
//   - 请求**不带** Idempotency-Key → 原样放行，零开销。幂等是按接口
//     opt-in 的：客户端愿意为"安全重试"付出带头的成本，服务端才为它兜底。
//   - 首次请求：加锁执行，把响应（状态码 + Content-Type + body）按
//     Window 时长存起来，供后续重试重放。
//   - 相同键的重试（首次已完成）：**不执行 handler**，直接重放首次响应，
//     并附 `Idempotent-Replay: true` 头。业务代码不会再次执行——
//     这是与"靠订单号唯一约束兜底"的本质区别：那是事后拒绝，这是事前短路。
//   - 相同键的并发请求（首次仍在处理中）：返回 409，客户端稍后重试。
//   - 首次处理返回 5xx：**不缓存**、释放锁——5xx 多为瞬时故障，
//     缓存它会把一次抖动固化成 24 小时的"成功重放"，客户端用同一键重试即可。
//
// # 键的作用域
//
// `idempotency:<tenant>:<user>:<method>:<路由模板>:<key>`。
// 租户/用户来自 Auth 写入的上下文（公开路由为 0）。把路由模板编进键：
// 两个接口用了同一个幂等键值也互不干扰；把租户/用户编进键：
// 不同的租户/用户天然隔离，A 用户不能用 B 用户的键重放 A 的响应。
//
// # 故障语义
//
// Redis 不可用时 fail-open（放行、不缓存）：幂等是**锦上添花**的保护，
// 不缓存的最坏结果是"退化回无幂等的现状"（业务侧仍有唯一约束兜底），
// 而 fail-closed 会把一次 Redis 抖动放大成整个接口不可用。
//
// # 使用方式
//
// 包装式（因为需要拿到 handler 执行完的响应才能缓存）：
//
//	protected(system, http.MethodPost, "/pay/order", permPayOrderCreate,
//	    middleware.Idempotency(payController.CreateOrder))
type IdempotencyOptions struct {
	// Window 首次响应的保留时长，超过后同键重试将重新执行。默认 24h。
	Window time.Duration
	// LockTTL "处理中"锁的存活时长，防止 handler 异常退出后锁永不释放。
	// 应大于该接口最坏处理耗时。默认 60s。
	LockTTL time.Duration
}

const (
	// IdempotencyKeyHeader 幂等键请求头。
	IdempotencyKeyHeader = "Idempotency-Key"
	// IdempotencyReplayHeader 重放响应时附加的标记头。
	IdempotencyReplayHeader = "Idempotent-Replay"

	// DefaultIdempotencyWindow 响应保留时长默认值。
	DefaultIdempotencyWindow = 24 * time.Hour
	// DefaultIdempotencyLockTTL 处理锁默认 TTL。
	DefaultIdempotencyLockTTL = 60 * time.Second
	// maxIdempotencyKeyLen 幂等键长度上限：防超长键污染 Redis 键空间。
	maxIdempotencyKeyLen = 128
)

// IdempotencyStore 幂等所需的 Redis 原语，便于测试注入内存实现。
type IdempotencyStore interface {
	Get(ctx context.Context, key string) (value string, found bool, err error)
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

// redisIdempotencyStore 默认实现，包装 cache 包的全局客户端。
type redisIdempotencyStore struct{}

func (redisIdempotencyStore) Get(ctx context.Context, key string) (string, bool, error) {
	return cache.GetString(ctx, key)
}

func (redisIdempotencyStore) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return cache.SetNX(ctx, key, value, ttl)
}

func (redisIdempotencyStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return cache.Set(ctx, key, value, ttl)
}

func (redisIdempotencyStore) Del(ctx context.Context, keys ...string) error {
	return cache.Del(ctx, keys...)
}

// Idempotency 用默认配置与真实 Redis 构建幂等中间件。
func Idempotency(next gin.HandlerFunc) gin.HandlerFunc {
	return IdempotencyWithStore(IdempotencyOptions{}, redisIdempotencyStore{}, next)
}

// idempotencyEnvelope 缓存的响应体。body 用 base64 编码：
// 响应是任意字节流（gin JSON 虽是文本，但通用性优先），JSON 字符串
// 直存二进制会损坏；base64 是唯一无损且可反解的选择。
type idempotencyEnvelope struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
}

// IdempotencyWithStore 指定配置与存储实现的幂等中间件（测试注入用）。
func IdempotencyWithStore(opts IdempotencyOptions, store IdempotencyStore, next gin.HandlerFunc) gin.HandlerFunc {
	if opts.Window <= 0 {
		opts.Window = DefaultIdempotencyWindow
	}
	if opts.LockTTL <= 0 {
		opts.LockTTL = DefaultIdempotencyLockTTL
	}

	return func(c *gin.Context) {
		key := c.GetHeader(IdempotencyKeyHeader)
		// 不带键：按普通请求放行。这是 opt-in 语义，也是 Redis 故障时
		// fail-open 的同一条放行路径。
		if key == "" {
			next(c)
			return
		}
		if len(key) > maxIdempotencyKeyLen {
			common.ErrorWithHttpStatus(c, http.StatusBadRequest,
				common.CodeBadRequest, "Idempotency-Key 过长（最多 128 字符）")
			c.Abort()
			return
		}

		storeKey := idempotencyStoreKey(c, key)

		// 1. 已有缓存响应：直接重放，handler 不执行。
		raw, found, err := store.Get(c.Request.Context(), storeKey)
		if err != nil {
			logger.Log.Errorf("[idempotency] %s 读取缓存失败，fail-open 放行: %v", storeKey, err)
			next(c)
			return
		}
		if found {
			var env idempotencyEnvelope
			if json.Unmarshal([]byte(raw), &env) == nil && env.Status > 0 {
				replayIdempotent(c, env)
				return
			}
			// 缓存内容损坏：当作不存在继续走加锁路径，不要让一个坏键
			// 把接口打成永久 500。锁 TTL 到期后自愈。
		}

		// 2. 加"处理中"锁：拿到锁的执行，其余并发同键请求返回 409。
		lockKey := storeKey + ":lock"
		acquired, err := store.SetNX(c.Request.Context(), lockKey, "1", opts.LockTTL)
		if err != nil {
			logger.Log.Errorf("[idempotency] %s 加锁失败，fail-open 放行: %v", storeKey, err)
			next(c)
			return
		}
		if !acquired {
			common.ErrorWithHttpStatus(c, http.StatusConflict,
				common.CodeIdempotencyConflict, common.ErrorCodeMessages[common.CodeIdempotencyConflict])
			c.Abort()
			return
		}

		// 3. 捕获 handler 的响应再缓存。
		capture := &captureResponseWriter{ResponseWriter: c.Writer}
		c.Writer = capture
		next(c)

		// 5xx 不缓存并释放锁：瞬时故障不该被固化为 24h 的"成功重放"，
		// 客户端用同一键重试即可重新执行。
		if capture.Status() >= http.StatusInternalServerError {
			logger.Log.Warnf("[idempotency] %s 返回 %d，不缓存并释放锁", storeKey, capture.Status())
			if err := store.Del(c.Request.Context(), lockKey); err != nil {
				logger.Log.Errorf("[idempotency] %s 释放锁失败: %v", lockKey, err)
			}
			return
		}

		env := idempotencyEnvelope{
			Status:      capture.Status(),
			ContentType: capture.Header().Get("Content-Type"),
			Body:        base64.StdEncoding.EncodeToString(capture.body.Bytes()),
		}
		if raw, err := json.Marshal(env); err == nil {
			if err := store.Set(c.Request.Context(), storeKey, string(raw), opts.Window); err != nil {
				logger.Log.Errorf("[idempotency] %s 缓存响应失败: %v", storeKey, err)
			}
		} else {
			logger.Log.Errorf("[idempotency] %s 序列化响应失败: %v", storeKey, err)
		}
		// 锁单独删而不是等 TTL：处理完成后立刻允许"带新数据的新请求"是
		// 正常语义，且缓存已写入，后续同键请求走重放路径。
		if err := store.Del(c.Request.Context(), lockKey); err != nil {
			logger.Log.Errorf("[idempotency] %s 释放锁失败: %v", lockKey, err)
		}
	}
}

// replayIdempotent 重放缓存的响应。
func replayIdempotent(c *gin.Context, env idempotencyEnvelope) {
	c.Header(IdempotencyReplayHeader, "true")
	if env.ContentType != "" {
		c.Header("Content-Type", env.ContentType)
	}
	c.Status(env.Status)
	if body, err := base64.StdEncoding.DecodeString(env.Body); err == nil {
		_, _ = c.Writer.Write(body)
	}
	c.Abort()
}

// idempotencyStoreKey 构建缓存键：租户/用户/方法/路由模板/键值。
// 用 c.FullPath()（路由模板）而非 URL 原始路径：同一接口的不同参数
// 属于同一幂等作用域，键值本身才负责区分具体那次操作。
func idempotencyStoreKey(c *gin.Context, key string) string {
	tenant, _ := common.TenantIDFrom(c)
	path := c.FullPath()
	if path == "" {
		path = c.Request.URL.Path
	}
	return fmt.Sprintf("idempotency:%d:%d:%s:%s:%s",
		tenant, common.GetCurrentUserID(c), c.Request.Method, path, key)
}

// captureResponseWriter 包装 gin.ResponseWriter，捕获下游 handler 写出的
// body。状态码直接复用 gin 自带的 Status()（默认 200，handler 显式写过
// 则为写入值），因此只需拦截 Write。
type captureResponseWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *captureResponseWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}
