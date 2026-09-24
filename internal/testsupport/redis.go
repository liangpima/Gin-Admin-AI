package testsupport

import (
	"os"
	"testing"

	"go-admin/config"
	"go-admin/internal/cache"
)

// Redis 相关的测试门控。
//
// 放在 testsupport 而不是各测试包里各写一份：中间件与 member 模块都需要
// 「造一个 Redis 不可用的环境」和「连真实 Redis」，两份实现会各自漂移，
// 而这类脚手架的漂移很难被发现（表现是某些用例在某些机器上静默换了分支）。

// WithNilRedis 显式把 cache 的客户端置空，模拟「Redis 不可用」。
//
// 必须显式置 nil 而不是依赖环境：同包其它用例会初始化 Redis，
// 不显式清掉的话这条用例的结果就取决于执行顺序。
func WithNilRedis(t *testing.T) {
	t.Helper()
	prev := cache.RDB
	cache.RDB = nil
	t.Cleanup(func() { cache.RDB = prev })
}

// WithTestRedis 连到本机/CI 的 Redis；连不上就跳过。
//
// 走「连不上则 skip」而不是硬依赖，是为了让 `go test ./...` 在没有 Redis 的
// 机器上仍然能跑（与 internal/database 用 TEST_MYSQL_* 门控是同一取舍）。
// CI 上通过 ci.yml 的 services 起一个 Redis，因此这些用例在 CI 里是真跑的。
func WithTestRedis(t *testing.T) {
	t.Helper()

	prevRDB := cache.RDB
	t.Cleanup(func() { cache.RDB = prevRDB })

	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	prevAddr := config.Cfg.Redis.Addr
	config.Cfg.Redis.Addr = addr
	t.Cleanup(func() { config.Cfg.Redis.Addr = prevAddr })

	if err := cache.Init(); err != nil {
		t.Skipf("Redis 不可用（%s），跳过：%v", addr, err)
	}
}
