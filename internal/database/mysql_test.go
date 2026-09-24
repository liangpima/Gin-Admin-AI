package database

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"go-admin/config"
)

// TestInitReturnsWrappedErrorWhenUnreachable 连不上库时必须返回错误，且不能
// 留下一个「看起来可用」的全局 DB。
//
// 为什么这条最值得测：Init 的调用方（cmd/server）是按「err == nil 就继续」
// 写的。若连不上却返回 nil，后续所有查询都在一个坏连接池上执行，
// 表现是服务正常启动、每个接口各自 500 —— 启动期的快速失败就此丢失。
//
// 端口 1 是刻意选的：TCP 连接会被立即拒绝，不需要等拨号超时，用例不会拖慢。
func TestInitReturnsWrappedErrorWhenUnreachable(t *testing.T) {
	origCfg, origDB := config.Cfg, DB
	t.Cleanup(func() { config.Cfg, DB = origCfg, origDB })

	config.Cfg.Database = config.DatabaseConfig{
		Host:     "127.0.0.1",
		Port:     1,
		Username: "nobody",
		Password: "nobody",
		DBName:   "nobody",
	}
	DB = nil

	err := Init()

	if err == nil {
		t.Fatal("连不上数据库时 Init 必须返回错误")
	}
	if !strings.Contains(err.Error(), "连接数据库失败") {
		t.Errorf("错误应带上下文说明是连接失败，实际: %v", err)
	}
	if DB != nil {
		t.Error("Init 失败时不应写入全局 DB（会让调用方误以为已就绪）")
	}
}

// TestInitAppliesPoolSettingsWhenReachable 连接成功时要真的应用连接池参数。
//
// 池参数写错是典型的静默故障：MaxOpenConns 若为 0（未设置），
// database/sql 会当成「无上限」，高并发下把 MySQL 的 max_connections 打满，
// 而单机压测往往看不出来。
//
// 需要真实 MySQL，因此用环境变量显式开启（不在仓库里写库密码）；
// 未配置时跳过，CI 上不设置该变量即可。
func TestInitAppliesPoolSettingsWhenReachable(t *testing.T) {
	host := os.Getenv("TEST_MYSQL_HOST")
	if host == "" {
		t.Skip("未设置 TEST_MYSQL_HOST，跳过需要真实 MySQL 的用例")
	}

	port := 3306
	if v := os.Getenv("TEST_MYSQL_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("TEST_MYSQL_PORT 不是合法端口: %q", v)
		}
		port = p
	}

	origCfg, origDB := config.Cfg, DB
	t.Cleanup(func() { config.Cfg, DB = origCfg, origDB })

	config.Cfg.Database = config.DatabaseConfig{
		Host:            host,
		Port:            port,
		Username:        os.Getenv("TEST_MYSQL_USER"),
		Password:        os.Getenv("TEST_MYSQL_PASSWORD"),
		DBName:          os.Getenv("TEST_MYSQL_DBNAME"),
		MaxIdleConns:    3,
		MaxOpenConns:    7,
		ConnMaxLifetime: 60,
		ConnMaxIdleTime: 30,
	}

	if err := Init(); err != nil {
		t.Fatalf("连接真实 MySQL 失败: %v", err)
	}
	if DB == nil {
		t.Fatal("Init 成功后全局 DB 不应为 nil")
	}

	sqlDB, err := DB.DB()
	if err != nil {
		t.Fatalf("取 sql.DB 失败: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if got := sqlDB.Stats().MaxOpenConnections; got != 7 {
		t.Errorf("MaxOpenConns 未生效，期望 7 实际 %d", got)
	}

	if err := sqlDB.Ping(); err != nil {
		t.Errorf("Init 返回成功后连接应可用: %v", err)
	}

	// ConnMaxLifetime / ConnMaxIdleTime / MaxIdleConns 没有公开的读取接口，
	// 用一次真实查询确认连接池确实能跑通 SQL（间接说明 Set* 那几行被执行了）。
	var one int
	if err := DB.Raw("SELECT 1").Scan(&one).Error; err != nil {
		t.Errorf("在 Init 建立的连接上执行查询失败: %v", err)
	}
	if one != 1 {
		t.Errorf("SELECT 1 返回 %d，期望 1", one)
	}
}
