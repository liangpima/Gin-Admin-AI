// Package metrics 提供框架级 Prometheus 指标。
//
// # 为什么独立成包
//
// 指标要被**多层**调用：路由中间件记 HTTP、支付服务记回调结果、
// main 注入 DB 连接池。放在任何一层都会造成反向依赖或循环依赖。
//
// # 暴露方式（安全边界，必须遵守）
//
// 指标通过 /metrics 暴露在**独立的内网端口**（server.metrics_port，
// 0 = 关闭），而不是挂在业务端口上。原因：指标标签带路由维度，
// 组合起来等同暴露内部拓扑与调用量分布，属于信息泄漏面；
// 且 Prometheus 抓取端点历来是未鉴权信息源，绝不能进公网。
// 部署时该端口只对内网/抓取器开放（compose 网络或安全组）。
//
// # 标签基数（cardinality）纪律
//
// HTTP 指标的 path 标签取 **c.FullPath()（路由模板）**，
// 不是原始 URL —— 否则 /pay/order/PAY20260928/0001 这类带 ID 的路径
// 会让时间序列无限增长，最终打爆 Prometheus。未匹配任何路由的请求
// 一律记为 "UNMATCHED"。
package metrics

import (
	"database/sql"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// httpRequestsTotal 按「方法 × 路由模板 × 状态码」计请求量。
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "goadmin",
			Name:      "http_requests_total",
			Help:      "HTTP 请求总数（path 为路由模板，未匹配路由记为 UNMATCHED）",
		},
		[]string{"method", "path", "status"},
	)

	// httpRequestDuration 按「方法 × 路由模板」记时延分布。
	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "goadmin",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP 请求时延（秒）",
			// 后台管理 API 的时延量级：大部分 <100ms，慢查询 >1s。
			// 桶上界到 10s：覆盖日志清理这类长任务的外部调用观测。
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"method", "path"},
	)

	// paymentNotifyTotal 支付回调结果计数。
	//
	// 这是本项目第一条**业务级**指标 —— 第三轮审查发现的 M7
	// （回调静默 ACK 导致「钱收了单没成」）当时发现不了，根因就是
	// 只有日志没有指标。三个取值：
	//   success   条件更新完成状态流转（本请求把订单置为已支付）
	//   duplicate 重复通知（订单已是已支付，幂等 ACK）
	//   rejected  拒绝处理（订单已关闭、金额不符、签名/报文解析失败…），
	//             渠道会重试 —— rejected 持续增长是资金问题的最直接信号
	paymentNotifyTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "goadmin",
			Name:      "payment_notify_total",
			Help:      "支付回调处理结果计数（success/duplicate/rejected）",
		},
		[]string{"result"},
	)

	// dbPoolGauges 连接池四项统计，kind 标签统一为数据库名。
	dbPoolGauges = map[string]*prometheus.GaugeVec{
		"max_open": prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "goadmin", Name: "db_pool_max_open",
			Help: "连接池最大打开连接数",
		}, []string{"kind"}),
		"in_use": prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "goadmin", Name: "db_pool_in_use",
			Help: "正在使用的连接数",
		}, []string{"kind"}),
		"idle": prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "goadmin", Name: "db_pool_idle",
			Help: "空闲连接数",
		}, []string{"kind"}),
		"wait_count": prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "goadmin", Name: "db_pool_wait_count_total",
			Help: "等待连接的累计次数（持续增长=池太小或查询太慢）",
		}, []string{"kind"}),
	}
)

func init() {
	prometheus.MustRegister(httpRequestsTotal, httpRequestDuration, paymentNotifyTotal)
	for _, g := range dbPoolGauges {
		prometheus.MustRegister(g)
	}
}

// ─────────────────────────── HTTP 中间件 ───────────────────────────

// Middleware 记录每个 HTTP 请求的量与时延。
// 挂在路由最外层（Recovery 之前），panic 被兜成 500 的请求同样计入。
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := c.FullPath()
		if path == "" {
			// 404 / 405 等未匹配请求：FullPath() 为空。
			// 绝不能用原始 URL 当标签 —— 那是基数爆炸的标准来源。
			path = "UNMATCHED"
		}
		status := strconv.Itoa(c.Writer.Status())
		httpRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		httpRequestDuration.WithLabelValues(c.Request.Method, path).
			Observe(time.Since(start).Seconds())
	}
}

// ─────────────────────────── 业务计数入口 ───────────────────────────

// 支付回调的结果枚举（见 paymentNotifyTotal 注释）。
const (
	NotifySuccess   = "success"
	NotifyDuplicate = "duplicate"
	NotifyRejected  = "rejected"
)

// IncPaymentNotify 支付回调结果计数 +1。
func IncPaymentNotify(result string) {
	paymentNotifyTotal.WithLabelValues(result).Inc()
}

// ─────────────────────────── DB 连接池 ───────────────────────────

// dbPoolPtr 由 SetDBPool 注入；nil 时刷新协程直接跳过 ——
// gauge 维持上一次值。用「注入 + 定时刷新」而不是 collector 回调：
// 后者会在每次 /metrics 抓取时调用 DB，抓取频率高时有额外开销。
var dbPoolPtr atomic.Pointer[sql.DB]

// SetDBPool 注入连接池（main.go 用 database.DB.DB()）。
func SetDBPool(db *sql.DB) { dbPoolPtr.Store(db) }

func init() {
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for range t.C {
			refreshDBPool()
		}
	}()
}

// refreshDBPool 把当前统计刷进 gauge。
func refreshDBPool() {
	db := dbPoolPtr.Load()
	if db == nil {
		return
	}
	st := db.Stats()
	dbPoolGauges["max_open"].WithLabelValues("mysql").Set(float64(st.MaxOpenConnections))
	dbPoolGauges["in_use"].WithLabelValues("mysql").Set(float64(st.InUse))
	dbPoolGauges["idle"].WithLabelValues("mysql").Set(float64(st.Idle))
	dbPoolGauges["wait_count"].WithLabelValues("mysql").Set(float64(st.WaitCount))
}

// ─────────────────────────── /metrics 处理器 ───────────────────────────

// Handler 返回 Prometheus 抓取端点。
func Handler() http.Handler {
	return promhttp.Handler()
}
