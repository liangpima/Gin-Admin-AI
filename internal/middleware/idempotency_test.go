package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-admin/internal/common"

	"github.com/gin-gonic/gin"
)

// fakeIdempotencyStore 内存实现，行为对齐 Redis：Get/Set/SetNX/Del。
type fakeIdempotencyStore struct {
	mu     sync.Mutex
	kv     map[string]string
	locks  map[string]bool
	errDel bool // Del 返回错误，模拟释放锁失败
}

func newFakeIdempotencyStore() *fakeIdempotencyStore {
	return &fakeIdempotencyStore{kv: map[string]string{}, locks: map[string]bool{}}
}

func (f *fakeIdempotencyStore) Get(_ context.Context, key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.kv[key]
	return v, ok, nil
}

func (f *fakeIdempotencyStore) Set(_ context.Context, key, value string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kv[key] = value
	return nil
}

func (f *fakeIdempotencyStore) SetNX(_ context.Context, key, _ string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.locks[key] {
		return false, nil
	}
	f.locks[key] = true
	return true, nil
}

func (f *fakeIdempotencyStore) Del(_ context.Context, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errDel {
		return errors.New("del failed")
	}
	for _, k := range keys {
		delete(f.locks, k)
	}
	return nil
}

// runIdempotent 构建一个被幂等中间件包住的下游 handler 并执行一次请求。
// next 里操作 *hits，用于断言「handler 是否真的被执行」。
func runIdempotent(t *testing.T, store IdempotencyStore, opts IdempotencyOptions, key string, next func(c *gin.Context), hits *int) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/pay/order", IdempotencyWithStore(opts, store, func(c *gin.Context) {
		*hits++
		next(c)
	}))

	req := httptest.NewRequest(http.MethodPost, "/pay/order", nil)
	if key != "" {
		req.Header.Set(IdempotencyKeyHeader, key)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestIdempotency_NoKeyPassesThrough(t *testing.T) {
	store := newFakeIdempotencyStore()
	hits := 0
	okNext := func(c *gin.Context) { common.Success(c, gin.H{"n": hits}) }

	for i := 1; i <= 3; i++ {
		w := runIdempotent(t, store, IdempotencyOptions{}, "", okNext, &hits)
		if w.Code != http.StatusOK {
			t.Fatalf("不带幂等键的请求应放行，实际 %d", w.Code)
		}
	}
	if hits != 3 {
		t.Fatalf("不带幂等键应每次都执行 handler，实际执行 %d 次", hits)
	}
}

func TestIdempotency_ReplaysFirstResponse(t *testing.T) {
	store := newFakeIdempotencyStore()
	hits := 0
	okNext := func(c *gin.Context) {
		common.Success(c, gin.H{"orderNo": "PO-1", "hits": hits})
	}

	// 第一次：正常执行
	w1 := runIdempotent(t, store, IdempotencyOptions{}, "abc-123", okNext, &hits)
	if w1.Code != http.StatusOK {
		t.Fatalf("首次请求应 200，实际 %d", w1.Code)
	}
	if w1.Header().Get(IdempotencyReplayHeader) != "" {
		t.Fatal("首次响应不应带重放标记头")
	}

	// 第二次：同键重试，重放首次响应，handler 不再执行
	w2 := runIdempotent(t, store, IdempotencyOptions{}, "abc-123", okNext, &hits)
	if w2.Code != http.StatusOK {
		t.Fatalf("重放应 200，实际 %d", w2.Code)
	}
	if w2.Header().Get(IdempotencyReplayHeader) != "true" {
		t.Fatal("重放响应必须带 Idempotent-Replay: true")
	}
	if w2.Body.String() != w1.Body.String() {
		t.Fatalf("重放 body 应与首次一致: 首次 %q 重放 %q", w1.Body.String(), w2.Body.String())
	}
	if hits != 1 {
		t.Fatalf("同键重试不应再次执行 handler，实际执行 %d 次", hits)
	}
}

func TestIdempotency_ConcurrentConflict(t *testing.T) {
	store := newFakeIdempotencyStore()
	hits := 0
	okNext := func(c *gin.Context) { common.Success(c, gin.H{}) }

	// 模拟"首次仍在处理中"：直接预占锁
	key := "in-flight"
	fullKey := idempotencyStoreKeyForTest("POST", "/pay/order", key)
	if _, err := store.SetNX(context.Background(), fullKey+":lock", "1", time.Minute); err != nil {
		t.Fatalf("预占锁失败: %v", err)
	}

	w := runIdempotent(t, store, IdempotencyOptions{}, key, okNext, &hits)
	if w.Code != http.StatusConflict {
		t.Fatalf("锁被占用应返回 409，实际 %d", w.Code)
	}
	if hits != 0 {
		t.Fatalf("处理中的同键请求不应执行 handler，实际执行 %d 次", hits)
	}
}

// idempotencyStoreKeyForTest 与 idempotencyStoreKey 对齐的测试键构造
// （测试里没有真实的路由模板上下文，直接复刻同样的拼接规则）。
func idempotencyStoreKeyForTest(method, path, key string) string {
	return fmt.Sprintf("idempotency:%d:%d:%s:%s:%s", 0, 0, method, path, key)
}

func TestIdempotency_ServerErrorNotCachedAndLockReleased(t *testing.T) {
	store := newFakeIdempotencyStore()
	hits := 0
	failingNext := func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500})
	}

	w1 := runIdempotent(t, store, IdempotencyOptions{}, "k-500", failingNext, &hits)
	if w1.Code != http.StatusInternalServerError {
		t.Fatalf("首次 5xx 应透传，实际 %d", w1.Code)
	}

	// 5xx 不缓存：同键重试会**再次执行** handler（可恢复重试语义）
	okNext := func(c *gin.Context) { common.Success(c, gin.H{"recovered": true}) }
	w2 := runIdempotent(t, store, IdempotencyOptions{}, "k-500", okNext, &hits)
	if w2.Code != http.StatusOK {
		t.Fatalf("5xx 后同键重试应重新执行并成功，实际 %d", w2.Code)
	}
	if hits != 2 {
		t.Fatalf("5xx 不应缓存，handler 应被执行 2 次，实际 %d", hits)
	}
}

func TestIdempotency_FailOpenOnStoreError(t *testing.T) {
	// Get 报错 → 放行执行
	store := newFakeIdempotencyStore()
	hits := 0
	okNext := func(c *gin.Context) { common.Success(c, gin.H{}) }

	brokenStore := brokenGetStore{inner: store}
	w := runIdempotent(t, brokenStore, IdempotencyOptions{}, "k-err", okNext, &hits)
	if w.Code != http.StatusOK {
		t.Fatalf("存储故障应 fail-open 放行，实际 %d", w.Code)
	}
	if hits != 1 {
		t.Fatalf("存储故障时 handler 应执行，实际 %d 次", hits)
	}
}

type brokenGetStore struct{ inner IdempotencyStore }

func (b brokenGetStore) Get(ctx context.Context, key string) (string, bool, error) {
	return "", false, errors.New("redis down")
}
func (b brokenGetStore) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return b.inner.SetNX(ctx, key, value, ttl)
}
func (b brokenGetStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return b.inner.Set(ctx, key, value, ttl)
}
func (b brokenGetStore) Del(ctx context.Context, keys ...string) error {
	return b.inner.Del(ctx, keys...)
}

func TestIdempotency_KeyTooLongRejected(t *testing.T) {
	store := newFakeIdempotencyStore()
	hits := 0
	okNext := func(c *gin.Context) { common.Success(c, gin.H{}) }

	w := runIdempotent(t, store, IdempotencyOptions{}, strings.Repeat("k", 129), okNext, &hits)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("超长幂等键应返回 400，实际 %d", w.Code)
	}
	if hits != 0 {
		t.Fatalf("超长幂等键不应执行 handler，实际 %d 次", hits)
	}
}
