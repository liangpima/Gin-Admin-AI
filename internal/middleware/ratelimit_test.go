package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"go-admin/internal/common"

	"github.com/gin-gonic/gin"
)

// fakeRateLimitStore 内存实现：按需为每个键返回预置的自增序列与错误。
type fakeRateLimitStore struct {
	counts  map[string]int64
	ttls    map[string]time.Duration
	errKey  string // 命中该前缀时返回错误，模拟 Redis 故障
	errFlag bool
}

func newFakeRateLimitStore() *fakeRateLimitStore {
	return &fakeRateLimitStore{counts: map[string]int64{}, ttls: map[string]time.Duration{}}
}

func (f *fakeRateLimitStore) IncrWindow(_ context.Context, key string, ttl time.Duration) (int64, error) {
	if f.errFlag && (f.errKey == "" || len(key) >= len(f.errKey) && key[:len(f.errKey)] == f.errKey) {
		return 0, errors.New("redis down")
	}
	f.counts[key]++
	f.ttls[key] = ttl
	return f.counts[key], nil
}

func (f *fakeRateLimitStore) TTL(_ context.Context, key string) (time.Duration, error) {
	return f.ttls[key], nil
}

// runRateLimit 发起一个来自指定 IP 的请求，返回响应。
func runRateLimit(t *testing.T, opts RateLimitOptions, store RateLimitStore, clientIP string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", RateLimitWithStore(opts, store), func(c *gin.Context) {
		common.Success(c, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = clientIP + ":12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRateLimit_AllowsUnderLimit(t *testing.T) {
	store := newFakeRateLimitStore()
	opts := RateLimitOptions{Name: "test", Limit: 3, Window: time.Minute}

	for i := 1; i <= 3; i++ {
		w := runRateLimit(t, opts, store, "1.2.3.4")
		if w.Code != http.StatusOK {
			t.Fatalf("第 %d 个请求应放行(200)，实际 %d", i, w.Code)
		}
	}
}

func TestRateLimit_BlocksOverLimitWithRetryAfter(t *testing.T) {
	store := newFakeRateLimitStore()
	opts := RateLimitOptions{Name: "test", Limit: 2, Window: time.Minute}

	// 打满窗口
	_ = runRateLimit(t, opts, store, "1.2.3.4")
	_ = runRateLimit(t, opts, store, "1.2.3.4")

	w := runRateLimit(t, opts, store, "1.2.3.4")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("超限应返回 429，实际 %d", w.Code)
	}
	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("超限响应必须带 Retry-After 头")
	}
	if sec, err := strconv.Atoi(retryAfter); err != nil || sec <= 0 || sec > 60 {
		t.Fatalf("Retry-After 应在 (0,60] 秒内，实际 %q", retryAfter)
	}
}

func TestRateLimit_IsolatedByClientIP(t *testing.T) {
	store := newFakeRateLimitStore()
	opts := RateLimitOptions{Name: "test", Limit: 1, Window: time.Minute}

	// A 用尽配额后，B 不受影响
	if w := runRateLimit(t, opts, store, "1.1.1.1"); w.Code != http.StatusOK {
		t.Fatalf("第一个 IP 首次请求应放行，实际 %d", w.Code)
	}
	if w := runRateLimit(t, opts, store, "1.1.1.1"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("第一个 IP 超限应 429，实际 %d", w.Code)
	}
	if w := runRateLimit(t, opts, store, "2.2.2.2"); w.Code != http.StatusOK {
		t.Fatalf("第二个 IP 首次请求应放行，实际 %d", w.Code)
	}
}

func TestRateLimit_FailClosedOnRedisError(t *testing.T) {
	store := newFakeRateLimitStore()
	store.errFlag = true
	opts := RateLimitOptions{Name: "login", Limit: 10, Window: time.Minute, FailClosed: true}

	w := runRateLimit(t, opts, store, "1.2.3.4")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("fail-closed 时应返回 503，实际 %d", w.Code)
	}
}

func TestRateLimit_FailOpenOnRedisError(t *testing.T) {
	store := newFakeRateLimitStore()
	store.errFlag = true
	opts := RateLimitOptions{Name: "test", Limit: 10, Window: time.Minute}

	w := runRateLimit(t, opts, store, "1.2.3.4")
	if w.Code != http.StatusOK {
		t.Fatalf("fail-open 时应放行(200)，实际 %d", w.Code)
	}
}

func TestRateLimit_DisabledWhenLimitNotPositive(t *testing.T) {
	store := newFakeRateLimitStore()
	opts := RateLimitOptions{Name: "off", Limit: 0, Window: time.Minute}

	// 任意多次请求都放行，且不触碰存储
	for i := 0; i < 5; i++ {
		if w := runRateLimit(t, opts, store, "1.2.3.4"); w.Code != http.StatusOK {
			t.Fatalf("Limit<=0 应完全不生效，实际 %d", w.Code)
		}
	}
	if len(store.counts) != 0 {
		t.Fatalf("Limit<=0 不应产生计数，实际 %v", store.counts)
	}
}
