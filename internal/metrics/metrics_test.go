package metrics

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/glebarez/sqlite" // 纯 Go SQLite 驱动（与 testsupport 同款，无需 cgo）
	"github.com/prometheus/client_golang/prometheus"
)

func init() { gin.SetMode(gin.TestMode) }

// sumFamily 从默认 registry 汇总某个指标家族的全部序列值。
//
// 刻意不用 testutil.ToFloat64：它要求 collector 恰好一个序列，
// 而 CounterVec/GaugeVec 在多标签组合下必然有多个 —— 多用例共享
// 默认 registry（进程内全局）时更是如此。
func sumFamily(t *testing.T, familyName string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather 失败: %v", err)
	}
	var sum float64
	for _, f := range families {
		if f.GetName() != familyName {
			continue
		}
		for _, m := range f.GetMetric() {
			sum += m.GetCounter().GetValue() + m.GetGauge().GetValue()
		}
	}
	return sum
}

// TestMiddlewareCountsMatchedAndUnmatched 计数必须覆盖两类请求：
// 匹配路由的（path=路由模板）与未匹配的（path=UNMATCHED）。
//
// 未匹配分支是基数纪律的落点 —— 若实现误用原始 URL 当标签，
// 这条用例（固定请求一个不存在的带 ID 路径）就会因为「标签值每次不同」
// 而出现第二个时间序列，断言失败。
func TestMiddlewareCountsMatchedAndUnmatched(t *testing.T) {
	r := gin.New()
	r.Use(Middleware())
	r.GET("/api/v1/ping", func(c *gin.Context) { c.Status(200) })

	before := sumFamily(t, "goadmin_http_requests_total")

	// 匹配路由
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil))
	// 未匹配（带 ID 的路径，模拟基数爆炸载荷）
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/ping/000123", nil))

	after := sumFamily(t, "goadmin_http_requests_total")
	if after-before != 2 {
		t.Fatalf("两次请求应计 2 次，实际增量 %v", after-before)
	}

	// 标签必须是路由模板，而不是原始路径
	body := renderMetrics(t)
	if !strings.Contains(body, `path="/api/v1/ping"`) {
		t.Errorf("应包含路由模板标签，实际: %s", body)
	}
	if strings.Contains(body, `path="/api/v1/ping/000123"`) {
		t.Error("原始 URL 不得成为标签值（基数爆炸）")
	}
	if !strings.Contains(body, `path="UNMATCHED"`) {
		t.Error("未匹配请求应记为 UNMATCHED")
	}
}

// TestMiddlewareCountsStatus 中间件必须记到**真实响应码**：
// handler 自己写 500 与 panic 被 Recovery 兜成的 500 是同一条计数。
func TestMiddlewareCountsStatus(t *testing.T) {
	r := gin.New()
	r.Use(Middleware())
	r.GET("/boom", func(c *gin.Context) { c.Status(500) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	body := renderMetrics(t)
	if !strings.Contains(body, `status="500"`) {
		t.Errorf("应记录 500 状态码，实际: %s", body)
	}
}

// TestPaymentNotifyCounter 业务计数入口必须真的落到指标上。
func TestPaymentNotifyCounter(t *testing.T) {
	before := sumFamily(t, "goadmin_payment_notify_total")
	IncPaymentNotify(NotifySuccess)
	IncPaymentNotify(NotifySuccess)
	IncPaymentNotify(NotifyRejected)

	if got := sumFamily(t, "goadmin_payment_notify_total"); got-before != 3 {
		t.Fatalf("三次调用应计 3，实际增量 %v", got-before)
	}
}

// TestDBPoolGauges 注入连接池后 gauge 必须反映真实统计。
func TestDBPoolGauges(t *testing.T) {
	// sql.Open 不真正建连（惰性），纯 Go 驱动无需 cgo；
	// Stats() 在未连接时也返回合法零值
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("打开 sql.DB 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	SetDBPool(db)
	refreshDBPool()

	total := sumFamily(t, "goadmin_db_pool_max_open") + sumFamily(t, "goadmin_db_pool_in_use") +
		sumFamily(t, "goadmin_db_pool_idle") + sumFamily(t, "goadmin_db_pool_wait_count_total")
	if total < 0 {
		t.Errorf("db pool gauge 不应为负，实际 %v", total)
	}
}

// TestHandlerRendersFamilies /metrics 端点必须包含本包全部指标家族。
// 抓取规则依赖指标名稳定 —— 家族消失等于抓取规则静默失效。
func TestHandlerRendersFamilies(t *testing.T) {
	body := renderMetrics(t)
	for _, family := range []string{
		"goadmin_http_requests_total",
		"goadmin_http_request_duration_seconds",
		"goadmin_payment_notify_total",
		"goadmin_db_pool_max_open",
		"goadmin_db_pool_in_use",
		"goadmin_db_pool_idle",
		"goadmin_db_pool_wait_count_total",
	} {
		if !strings.Contains(body, family) {
			t.Errorf("/metrics 应包含指标家族 %s", family)
		}
	}
}

func renderMetrics(t *testing.T) string {
	t.Helper()
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/metrics 应返回 200，实际 %d", w.Code)
	}
	return w.Body.String()
}
