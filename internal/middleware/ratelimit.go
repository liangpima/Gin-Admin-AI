package middleware

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/logger"

	"github.com/gin-gonic/gin"
)

// RateLimit 基于 Redis 固定窗口的限流中间件。
//
// # 解决什么问题
//
// 框架此前只有一处业务自建的限速（支付平台公钥获取），登录、验证码、
// 支付回调等公开端点对"同一来源的请求突刺"没有任何上限：
// 登录可被高频爆破（登录失败锁有计数，但成功登录本身无限流），
// 验证码接口可被刷到占满 CPU，回调端点可被伪造流量打满连接池。
//
// # 设计取舍
//
//   - **固定窗口**（cache.IncrWindow）而不是滑动窗口/令牌桶：实现一次往返
//     完成计数，误判上界也只是窗口边界处 2×Limit，对防爆破/防刷足够，
//     且与登录失败计数同一套原语，运维心智一致。
//   - **维度**：Auth 之后走 `u:<tenant>:<userID>`（按人限，NAT 后多用户互不拖累）；
//     Auth 之前（登录/验证码/回调）只能走 `ip:<ClientIP>`，依赖
//     server.trusted_proxies 已正确配置（否则伪造 X-Forwarded-For 可换 IP 绕过，
//     该配置在 router.Setup 顶部已强制收紧）。
//   - **Redis 故障语义由挂载点决定**：登录类设 FailClosed（与登录限频
//     security.login_fail_closed 同理——限流失效正是爆破成本最低的窗口）；
//     其余默认 fail-open，只记 Error 日志，可用性优先。
//
// # 使用方式
//
// 中间件式挂载，可挂在组上或单条路由前：
//
//	auth.POST("/login", middleware.RateLimit(middleware.RateLimitOptions{
//	    Name: "login", Limit: 10, Window: time.Minute, FailClosed: true,
//	}), authController.Login)
//
// Name 是计数桶的名字，必须唯一且有业务含义（它进 Redis 键）。
// 测试用 RateLimitWithStore 注入内存实现，不依赖真实 Redis。
type RateLimitOptions struct {
	// Name 计数桶名，进 Redis 键（ratelimit:<name>:<identity>），必须唯一。
	Name string
	// Limit 窗口内允许的最大请求数；<=0 表示本中间件不生效。
	Limit int
	// Window 窗口长度，从窗口内**第一次请求**起算固定 ttl。
	Window time.Duration
	// FailClosed Redis 不可用时是否拒绝请求。登录/验证码类应设 true。
	FailClosed bool
}

// RateLimitStore 限流所需的 Redis 原语，便于测试注入内存实现。
type RateLimitStore interface {
	IncrWindow(ctx context.Context, key string, ttl time.Duration) (int64, error)
	TTL(ctx context.Context, key string) (time.Duration, error)
}

// redisRateLimitStore 默认实现，包装 cache 包的全局客户端。
type redisRateLimitStore struct{}

func (redisRateLimitStore) IncrWindow(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	return cache.IncrWindow(ctx, key, ttl)
}

func (redisRateLimitStore) TTL(ctx context.Context, key string) (time.Duration, error) {
	return cache.TTL(ctx, key)
}

// RateLimit 用真实 Redis 构建限流中间件。
func RateLimit(opts RateLimitOptions) gin.HandlerFunc {
	return RateLimitWithStore(opts, redisRateLimitStore{})
}

// RateLimitWithStore 指定存储实现的限流中间件（测试注入用）。
func RateLimitWithStore(opts RateLimitOptions, store RateLimitStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 未配置或配错（<=0）直接放行：与 BodyLimit 的 limit<=0 语义一致，
		// 让"关掉某个桶"成为配置改动而不是删代码。
		if opts.Limit <= 0 || opts.Window <= 0 {
			c.Next()
			return
		}

		key := fmt.Sprintf("ratelimit:%s:%s", opts.Name, rateLimitIdentity(c))
		n, err := store.IncrWindow(c.Request.Context(), key, opts.Window)
		if err != nil {
			if opts.FailClosed {
				// 503 而不是 429：语义是"防护此刻不可用"，不是"你太快了"。
				logger.Log.Errorf("[ratelimit] %s Redis 不可用，fail-closed 拒绝: %v", opts.Name, err)
				common.ErrorWithHttpStatus(c, http.StatusServiceUnavailable,
					common.CodeInternalError, "服务暂时不可用，请稍后重试")
				c.Abort()
				return
			}
			logger.Log.Errorf("[ratelimit] %s Redis 不可用，本次放行(fail-open): %v", opts.Name, err)
			c.Next()
			return
		}

		if n > int64(opts.Limit) {
			// Retry-After 取窗口剩余秒数；TTL 读失败时退化为整个窗口长度，
			// 多报不少报——客户端提前重试只是再吃一次 429，没有副作用。
			retry := opts.Window
			if ttl, err := store.TTL(c.Request.Context(), key); err == nil && ttl > 0 {
				retry = ttl
			}
			c.Header("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
			logger.Log.Warnf("[ratelimit] %s 超限拒绝: identity=%s count=%d/%d",
				opts.Name, rateLimitIdentity(c), n, opts.Limit)
			common.ErrorWithHttpStatus(c, http.StatusTooManyRequests,
				common.CodeTooManyRequests, common.ErrorCodeMessages[common.CodeTooManyRequests])
			c.Abort()
			return
		}

		c.Next()
	}
}

// rateLimitIdentity 计算限流主体：已登录按人（含租户隔离），未登录按来源 IP。
//
// 注意用 TenantIDFrom 而不是 GetTenantID：公开路由没有 Auth 写入的租户信息，
// GetTenantID 会打「缺少租户信息」的 Error 告警——那告警是给漏挂 Auth 的
// 路由准备的，被限流中间件误触发会污染日志。
func rateLimitIdentity(c *gin.Context) string {
	if uid := common.GetCurrentUserID(c); uid > 0 {
		tenant, _ := common.TenantIDFrom(c)
		return fmt.Sprintf("u:%d:%d", tenant, uid)
	}
	return "ip:" + common.NormalizeIP(c.ClientIP())
}
