// Package benchmark 是**性能基线与索引审计**（P2-4）。
//
// 只包含 _test.go 文件：不进入常规构建产物，但 `go test -bench` 与
// `go vet` 正常工作。
//
// # 为什么门控在 TEST_BENCH_DSN
//
// 本包与前几个包不同：全部价值在于**真实 MySQL + 十万级数据**。
// SQLite 的执行计划与 MySQL 完全不同，用它做索引审计是自欺。因此：
//
//	TEST_BENCH_DSN='user:pass@tcp(127.0.0.1:3306)/go_bench?charset=utf8mb4&parseTime=true&loc=Local&multiStatements=true' \
//	  go test ./internal/benchmark/ -v                        # EXPLAIN 审计
//	  go test ./internal/benchmark/ -bench . -benchtime 200x  # 基准
//
// 未设置环境变量时所有用例 skip，常规 CI 完全不受影响。
//
// # 数据规模（用户口径：十万级）
//
// pay_member / pay_order / sys_operation_log 各 10 万行，按 3 个租户打散
// （单租户 ≈ 3.3 万行）。种子用「数字表交叉连接 + INSERT..SELECT」——
// MySQL 5.7 没有递归 CTE，这是标准批量造数手法，全程 SQL 内完成。
// 种子幂等（TRUNCATE 重灌 + 库由 init.sql 的 IF NOT EXISTS 建）。
//
// # EXPLAIN 审计结论（2026-09-29，MySQL 5.7.26，各表 10 万行）
//
// | 查询 | type | key | rows | Extra |
// |---|---|---|---|---|
// | pay_member 租户+状态列表 | index | PRIMARY | 40 | Using where |
// | pay_order 租户+状态列表 | ref | idx_tenant_id | 49625 | Using where |
// | sys_operation_log 租户列表 | ref | idx_tenant_id | 49644 | Using where |
// | pay_order +渠道过滤 | ref | idx_tenant_id | 49625 | Using where |
//
// **无 ALL、无 filesort** —— ORDER BY id DESC 全部走索引序
// （InnoDB 二级索引隐含主键后缀，(tenant_id) 前缀下 id 天然有序）。
// 两个值得记住的细节：
//
//   - pay_member 那条 type=index / key=PRIMARY **不是缺陷**：LIMIT 20 +
//     高过滤密度（租户 1/3 × 状态 1/2 ≈ 1/6 命中）下，沿主键反向扫
//     40 行就凑齐 20 条，比 ref+回表更快。但它是**以过滤密度为前提**的
//     优化器选择 —— 命中率变低时（如按精确手机号前缀搜）会退化，
//     该形态变化本身就是信号，所以审计只断言「无 ALL / 无 filesort」，
//     不钉死 key
//   - rows 估算 49625 vs 真实 ~33k：估算偏差 1.5 倍属正常
//     （未跑 ANALYZE TABLE 的默认统计）。关注的是量级而非精确值
package benchmark

import (
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"go-admin/internal/database"
	"go-admin/internal/middleware"
	memberRepo "go-admin/internal/module/member/repository"
	payRepo "go-admin/internal/module/payment/repository"
	sysRepo "go-admin/internal/module/system/repository"
)

const benchTenantID = 1

var (
	benchOnce sync.Once
	benchErr  error
)

// requireBench 环境就绪后返回 *sql.DB；未配置 TEST_BENCH_DSN 时 skip。
func requireBench(tb testing.TB) *sql.DB {
	tb.Helper()
	benchOnce.Do(setupBench)
	if benchErr != nil {
		tb.Skipf("基准环境不可用: %v", benchErr)
	}
	return benchSQLDB
}

var benchSQLDB *sql.DB

func setupBench() {
	dsn := os.Getenv("TEST_BENCH_DSN")
	if dsn == "" {
		benchErr = fmt.Errorf("未设置 TEST_BENCH_DSN")
		return
	}

	raw, err := os.ReadFile("../../sql/init.sql")
	if err != nil {
		benchErr = fmt.Errorf("读 init.sql 失败: %w", err)
		return
	}
	// DSN 指向的库名由调用方定；init.sql 里的 `gin` 全部替换掉。
	// 库不存在时由 init.sql 的 CREATE DATABASE 创建。
	dbName := benchDBNameFromDSN(dsn)
	schema := strings.ReplaceAll(string(raw), "`gin`", "`"+dbName+"`")

	// 先连**服务器级** DSN（去掉库名）：库可能还不存在，
	// init.sql 里的 CREATE DATABASE 就是建库动作本身
	serverDSN := regexp.MustCompile(`/[^/@]+\?`).ReplaceAllString(dsn, "/?")
	bootstrap, err := sql.Open("mysql", serverDSN)
	if err != nil {
		benchErr = fmt.Errorf("连接失败: %w", err)
		return
	}
	defer bootstrap.Close()
	if err := bootstrap.Ping(); err != nil {
		benchErr = fmt.Errorf("连接失败: %w", err)
		return
	}

	// DSN 必须带 multiStatements=true（整份 init.sql 一次执行）
	if _, err := bootstrap.Exec(schema); err != nil {
		benchErr = fmt.Errorf("执行 init.sql 失败: %w", err)
		return
	}
	if _, err := bootstrap.Exec(seedSQL); err != nil {
		benchErr = fmt.Errorf("灌入种子数据失败: %w", err)
		return
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		benchErr = fmt.Errorf("打开基准库失败: %w", err)
		return
	}
	if err := db.Ping(); err != nil {
		benchErr = fmt.Errorf("打开基准库失败: %w", err)
		return
	}
	benchSQLDB = db

	// ── GORM 指到基准库：仓储都在构造时捕获 database.DB ──
	// 静默日志：基准输出里不能混进 SQL 日志
	g, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		benchErr = fmt.Errorf("打开 GORM 失败: %w", err)
		return
	}
	database.DB = g

	// Casbin：真实 enforcer（策略来自 init.sql 种子的 admin 通配）
	if err := middleware.InitCasbin("../../config/casbin/model.conf"); err != nil {
		benchErr = fmt.Errorf("初始化 casbin 失败: %w", err)
		return
	}

	// 跑一次 ANALYZE：init.sql 刚灌完 10 万行，统计信息若停留在空表
	// 会把 rows 估算带偏 5 个数量级，EXPLAIN 审计就失去意义
	for _, table := range []string{"pay_member", "pay_order", "sys_operation_log"} {
		if _, err := benchSQLDB.Exec("ANALYZE TABLE " + table); err != nil {
			benchErr = fmt.Errorf("ANALYZE %s 失败: %w", table, err)
			return
		}
	}
}

func benchDBNameFromDSN(dsn string) string {
	// go_bench 形态：.../dbName?params —— 取 / 与 ? 之间
	s := dsn
	if i := strings.Index(s, ")/"); i >= 0 {
		s = s[i+2:]
	} else if i := strings.Index(s, ":"); i >= 0 {
		// unix socket 等形态不支持，直接报错
		return ""
	}
	if i := strings.Index(s, "?"); i >= 0 {
		s = s[:i]
	}
	return s
}

// seedSQL 十万级造数（幂等：TRUNCATE 重灌）。
// 数字表交叉连接生成 10^5 行，租户按 n%3 打散。
//
// ⚠️ 各维度取值必须**相互独立**：第一版 tenant 和 status 都取 n%3，
// 两者完全相关 —— 租户 1 的订单全是 status 0，「status=1」命中 0 行，
// 基准测的是空结果集。第二版改用 *7 仍失败：7 ≡ 1 (mod 3)，
// 乘法没打破相关。最终用 **DIV 3（整除取商）** —— 商与余数天然独立。
// 相关性造数是基准测试的经典暗坑，两次踩坑才找对。
const seedSQL = `
CREATE TABLE IF NOT EXISTS bench_digits (n INT PRIMARY KEY);
INSERT INTO bench_digits (n) VALUES (0),(1),(2),(3),(4),(5),(6),(7),(8),(9);

TRUNCATE pay_member;
INSERT INTO pay_member
  (tenant_id, member_no, username, nickname, phone, gender, level_id, status, points)
SELECT
  1 + t.n % 3,
  CONCAT('B', LPAD(t.n + 1, 8, '0')),
  CONCAT('user', t.n + 1),
  CONCAT('会员', t.n + 1),
  CONCAT('139', LPAD((t.n + 1) % 100000000, 8, '0')),
  (t.n DIV 3) % 2,
  (t.n DIV 3) % 5 + 1,
  1,
  t.n * 7 % 1000
FROM (SELECT a.n + b.n*10 + c.n*100 + d.n*1000 + e.n*10000 AS n
      FROM bench_digits a, bench_digits b, bench_digits c, bench_digits d, bench_digits e) t;

TRUNCATE pay_order;
INSERT INTO pay_order
  (tenant_id, order_no, trade_no, subject, amount, channel, status, paid_at, created_at)
SELECT
  1 + t.n % 3,
  CONCAT('PAY-B', LPAD(t.n + 1, 8, '0')),
  CONCAT('TR', LPAD(t.n + 1, 10, '0')),
  CONCAT('订单-', t.n + 1),
  100 + t.n % 9000,
  IF((t.n DIV 3) % 2 = 0, 'wechat', 'alipay'),
  (t.n DIV 3) % 3,
  NOW(),
  DATE_ADD(NOW(), INTERVAL -(t.n % 90) DAY)
FROM (SELECT a.n + b.n*10 + c.n*100 + d.n*1000 + e.n*10000 AS n
      FROM bench_digits a, bench_digits b, bench_digits c, bench_digits d, bench_digits e) t;

TRUNCATE sys_operation_log;
INSERT INTO sys_operation_log
  (tenant_id, title, action, request_method, request_url, status, ip, operator_id, operator_name, cost_time, created_at)
SELECT
  1 + t.n % 3,
  CONCAT('操作-', t.n % 50),
  'query', 'GET', '/api/v1/system/user/list',
  1, '10.0.0.1', 1, 'admin', t.n % 200,
  DATE_ADD(NOW(), INTERVAL -(t.n % 90) DAY)
FROM (SELECT a.n + b.n*10 + c.n*100 + d.n*1000 + e.n*10000 AS n
      FROM bench_digits a, bench_digits b, bench_digits c, bench_digits d, bench_digits e) t;

DROP TABLE bench_digits;
`

// explainPlan 执行 EXPLAIN 并返回 (type, key, rows, Extra)。
// EXPLAIN 输出列固定 12 列（MySQL 5.7 含 partitions），逐列扫成字符串。
func explainPlan(t *testing.T, db *sql.DB, query string, args ...interface{}) (typ, key string, rows int64, extra string) {
	t.Helper()
	rs, err := db.Query("EXPLAIN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN 失败: %v", err)
	}
	defer rs.Close()

	cols, err := rs.Columns()
	if err != nil {
		t.Fatalf("读 EXPLAIN 列名失败: %v", err)
	}
	for rs.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]*sql.NullString, len(cols))
		for i := range vals {
			ptrs[i] = &sql.NullString{}
			vals[i] = ptrs[i]
		}
		if err := rs.Scan(vals...); err != nil {
			t.Fatalf("读 EXPLAIN 行失败: %v", err)
		}
		get := func(name string) string {
			for i, c := range cols {
				if c == name {
					return ptrs[i].String
				}
			}
			return ""
		}
		typ, key = get("type"), get("key")
		extra = get("Extra")
		n := get("rows")
		fmt.Sscanf(n, "%d", &rows)
	}
	return typ, key, rows, extra
}

// assertNoFullScanAndNoFilesort 热点查询的两条硬性判定。
// 刻意不钉死 key：优化器在「ref + 回表」与「index 反向扫」之间的选择
// 取决于过滤密度（见包注释），钉死会在统计信息变化时假红。
func assertNoFullScanAndNoFilesort(t *testing.T, name, typ, key string, rows int64, extra string) {
	t.Helper()
	if typ == "ALL" {
		t.Errorf("[%s] 全表扫（type=ALL，rows=%d）—— 需要补索引", name, rows)
	}
	if strings.Contains(extra, "filesort") {
		t.Errorf("[%s] 出现 filesort（key=%s）—— 排序列未走索引序", name, key)
	}
	t.Logf("[%s] type=%s key=%s rows=%d extra=%q", name, typ, key, rows, extra)
}

// ─────────────────────────── EXPLAIN 审计 ───────────────────────────

// TestExplainHotQueries 四条热点查询的索引审计。
// 查询必须**逐字镜像**仓储实现里的 WHERE/ORDER（含 deleted_at 过滤、
// TenantScope 的形态），否则审的是想象的查询。
//
// ⚠️ sys_operation_log 没有 deleted_at 列（日志表走硬删清理），
// 它的查询不能套软删除过滤 —— 审计曾因此报过「Unknown column」。
func TestExplainHotQueries(t *testing.T) {
	db := requireBench(t)

	// 1. 会员列表（memberRepository.FindList：租户 + 状态，倒序分页）
	typ, key, rows, extra := explainPlan(t, db,
		`SELECT * FROM pay_member WHERE tenant_id = ? AND deleted_at IS NULL AND status = ? ORDER BY id DESC LIMIT 20`,
		benchTenantID, 1)
	assertNoFullScanAndNoFilesort(t, "会员列表", typ, key, rows, extra)

	// 2. 订单列表（payOrderRepository.FindList：租户 + 状态，倒序分页）
	typ, key, rows, extra = explainPlan(t, db,
		`SELECT * FROM pay_order WHERE tenant_id = ? AND deleted_at IS NULL AND status = ? ORDER BY id DESC LIMIT 20`,
		benchTenantID, 1)
	assertNoFullScanAndNoFilesort(t, "订单列表", typ, key, rows, extra)

	// 3. 操作日志列表（logRepository.FindOperationLogList：仅租户，倒序分页）
	typ, key, rows, extra = explainPlan(t, db,
		`SELECT * FROM sys_operation_log WHERE tenant_id = ? ORDER BY id DESC LIMIT 20`,
		benchTenantID)
	assertNoFullScanAndNoFilesort(t, "操作日志列表", typ, key, rows, extra)

	// 4. 订单列表带渠道过滤（FindList 的 channel 分支）
	typ, key, rows, extra = explainPlan(t, db,
		`SELECT * FROM pay_order WHERE tenant_id = ? AND deleted_at IS NULL AND status = ? AND channel = ? ORDER BY id DESC LIMIT 20`,
		benchTenantID, 1, "wechat")
	assertNoFullScanAndNoFilesort(t, "订单列表(渠道)", typ, key, rows, extra)

	// 5. 手机号模糊搜索（memberRepository.FindList 的 phone 分支）——**已知不走索引**。
	// LIKE '%x%' 无法用 B+ 树；十万级实测是全表扫。记录为文档化的已知取舍：
	// 后台按手机号搜会员的频率低（运营检索），若未来变成高频路径，
	// 方案是前缀匹配（LIKE 'x%'，uk_tenant_phone 可用）或全文索引。
	typ, key, rows, extra = explainPlan(t, db,
		`SELECT * FROM pay_member WHERE tenant_id = ? AND deleted_at IS NULL AND phone LIKE ? ORDER BY id DESC LIMIT 20`,
		benchTenantID, "%888")
	if typ != "ALL" {
		t.Logf("[手机号模糊] 预期 ALL（%q 无法走索引），实际 type=%s key=%s rows=%d", "%888", typ, key, rows)
	}
	t.Logf("[手机号模糊] type=%s key=%s rows=%d extra=%q（已知取舍：LIKE '%%x%%' 全表扫）", typ, key, rows, extra)
}

// ─────────────────────────── 基准 ───────────────────────────

// BenchmarkMemberListPage 会员列表第一页（租户 1，状态=正常）。
// 数字记录在案（docs/bench-100k.md），后续改动可对比。
func BenchmarkMemberListPage(b *testing.B) {
	requireBench(b)
	repo := memberRepo.NewMemberRepository()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := repo.FindList(benchTenantID, "", "", 0, 1, 1, 20); err != nil {
			b.Fatalf("查询失败: %v", err)
		}
	}
}

// BenchmarkOrderListPage 订单列表第一页（租户 1，已支付）。
func BenchmarkOrderListPage(b *testing.B) {
	requireBench(b)
	repo := payRepo.NewPayOrderRepository()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := repo.FindList(benchTenantID, "", 1, "", 1, 20); err != nil {
			b.Fatalf("查询失败: %v", err)
		}
	}
}

// BenchmarkOperationLogPage 审计日志第一页（租户 1）。
func BenchmarkOperationLogPage(b *testing.B) {
	requireBench(b)
	repo := sysRepo.NewLogRepository()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := repo.FindOperationLogList(benchTenantID, "", nil, 1, 20); err != nil {
			b.Fatalf("查询失败: %v", err)
		}
	}
}

// BenchmarkCasbinEnforce 权限校验（Casbin 四元组 Enforce）。
// 每个鉴权请求都要过一次 —— 它的量级决定中间件开销下限。
func BenchmarkCasbinEnforce(b *testing.B) {
	requireBench(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if ok, err := middleware.EnforceCurrent("admin", "default", "system:user:list", "GET"); err != nil {
			b.Fatalf("enforce 失败: %v", err)
		} else if !ok {
			b.Fatal("admin 应拥有 system:user:list")
		}
	}
}
