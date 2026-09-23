// migrate 是一个极简的数据库迁移执行器。
//
// 存在的理由：升级脚本放在 sql/migrations/ 下，而在此之前**没有执行工具** ——
// 升级只能靠人手执行 SQL，漏掉不会有任何提示，直到某个接口开始报
// "Unknown column" 才被发现（例如 sys_post 缺 tenant_id，岗位接口会直接挂）。
//
// 用法：
//
//	go run ./cmd/migrate                 # 执行未应用的迁移
//	go run ./cmd/migrate -status         # 只查看状态，不执行
//	go run ./cmd/migrate -dry-run        # 只列出将执行的迁移
//	go run ./cmd/migrate -config <path>  # 指定配置文件（默认 config/config.yaml）
//
// 三个必须知道的设计取舍：
//
//  1. **不做事务回滚。** MySQL 的 DDL（CREATE/ALTER/DROP）会隐式提交，
//     事务根本包不住它，包了只会给人虚假的安全感。因此迁移脚本**必须幂等**
//     —— 这也是本项目既有约定（用 information_schema 判断后再操作）。
//  2. **失败即停，且不标记该版本。** 修正脚本后重新执行即可，
//     前面已成功的不会重跑（已记录在 schema_migrations）。
//  3. **版本号取自文件名**，记录在 schema_migrations 表。文件名必须形如
//     YYYY-MM-DD-描述.sql，否则直接拒绝执行 —— 排序错了会静默跑乱顺序。
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"

	"go-admin/config"

	// 迁移用原生 database/sql（GORM 的 DSN 不带 multiStatements），
	// 因此需要显式注册 mysql 驱动。
	_ "github.com/go-sql-driver/mysql"
)

const migrationsDir = "sql/migrations"

func main() {
	configPath := flag.String("config", "config/config.yaml", "配置文件路径")
	statusOnly := flag.Bool("status", false, "只显示迁移状态，不执行任何 SQL")
	dryRun := flag.Bool("dry-run", false, "只列出将要执行的迁移，不实际执行")
	flag.Parse()

	if err := config.Init(*configPath); err != nil {
		exitf("读取配置失败: %v", err)
	}

	migrations, err := loadMigrations(migrationsDir)
	if err != nil {
		exitf("%v", err)
	}
	if len(migrations) == 0 {
		fmt.Printf("迁移目录 %s 下没有 .sql 文件，无事可做\n", migrationsDir)
		return
	}

	db, err := openDB()
	if err != nil {
		exitf("%v", err)
	}
	defer db.Close()

	if err := ensureMigrationTable(db); err != nil {
		exitf("创建 %s 表失败: %v", migrationsTable, err)
	}

	applied, err := appliedVersions(db)
	if err != nil {
		exitf("读取已应用的迁移失败: %v", err)
	}

	var pending []migration
	for _, m := range migrations {
		if !applied[m.version] {
			pending = append(pending, m)
		}
	}

	printStatus(len(migrations), applied, pending)

	if *statusOnly || len(pending) == 0 {
		return
	}
	if *dryRun {
		fmt.Println("\n[dry-run] 未执行任何 SQL")
		return
	}

	warnOutOfOrder(applied, pending)

	if err := applyPending(db, pending); err != nil {
		exitf("\n%v", err)
	}
	fmt.Printf("\n完成：本次应用了 %d 个迁移\n", len(pending))
}

const migrationsTable = "schema_migrations"

// openDB 建立迁移专用的数据库连接。
//
// 刻意**不开** multiStatements：语句由 splitStatements 逐条切好后单条执行。
// 开着它反而有害 —— 若切分出错（比如漏判了某个分隔符），多条语句会被当成
// 一次请求悄悄执行掉；关掉之后服务端会直接报语法错误，问题立刻暴露。
func openDB() (*sql.DB, error) {
	dsn := config.Cfg.Database.DSN()

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	dsn += sep + "timeout=10s"

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库连接失败: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("连接数据库失败（请检查 database.* 配置）: %w", err)
	}
	return db, nil
}

func ensureMigrationTable(db *sql.DB) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS ` + migrationsTable + ` (
  version    varchar(128) NOT NULL COMMENT '迁移版本（取自文件名，去扩展名）',
  applied_at datetime     NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '执行时间',
  PRIMARY KEY (version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='数据库迁移执行记录'`
	_, err := db.Exec(ddl)
	return err
}

func appliedVersions(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT version FROM " + migrationsTable)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		result[v] = true
	}
	return result, rows.Err()
}

func printStatus(total int, applied map[string]bool, pending []migration) {
	fmt.Printf("迁移目录: %s\n", migrationsDir)
	fmt.Printf("共 %d 个迁移，已应用 %d 个，待执行 %d 个\n",
		total, total-len(pending), len(pending))

	if len(pending) == 0 {
		fmt.Println("数据库已是最新状态")
		return
	}

	fmt.Println("\n待执行:")
	for _, m := range pending {
		fmt.Printf("  - %s\n", m.version)
	}
}

// warnOutOfOrder 提醒「待执行的迁移比已应用的更早」这种情况。
//
// 通常意味着有人往历史里补了脚本。这类脚本可能依赖了较新的表结构，
// 按序执行才会成功 —— 值得人工确认，而不是默默跑过去。
func warnOutOfOrder(applied map[string]bool, pending []migration) {
	for _, m := range pending {
		for a := range applied {
			if a > m.version {
				fmt.Printf(
					"\n⚠️  注意：待执行的 %s 早于已应用的 %s。\n"+
						"   这通常表示历史迁移被补充过；若该脚本依赖较新的表结构，可能执行失败。\n\n",
					m.version, a)
				return
			}
		}
	}
}

func applyPending(db *sql.DB, pending []migration) error {
	for _, m := range pending {
		content, err := os.ReadFile(m.path)
		if err != nil {
			return fmt.Errorf("读取 %s 失败: %w", m.path, err)
		}

		// 先切分再执行：DELIMITER 是客户端指令，必须自己处理，
		// 整文件丢给 db.Exec 会在 DELIMITER 处直接语法错误（详见 splitStatements）。
		stmts, err := splitStatements(string(content))
		if err != nil {
			return fmt.Errorf("解析 %s 失败: %w", m.path, err)
		}
		if len(stmts) == 0 {
			return fmt.Errorf("迁移 %s 未解析出任何语句（文件为空或只有注释）", m.version)
		}

		fmt.Printf("执行 %s (%d 条语句) ... ", m.version, len(stmts))

		// 刻意不用事务：MySQL 的 DDL 隐式提交，事务无法回滚它，
		// 加上只会让人误以为失败能自动回滚。幂等性由脚本自身保证。
		if err := execStatements(db, m.version, stmts); err != nil {
			fmt.Println("失败")
			return err
		}

		if _, err := db.Exec(
			"INSERT INTO "+migrationsTable+" (version) VALUES (?)", m.version,
		); err != nil {
			fmt.Println("失败")
			return fmt.Errorf(
				"迁移 %s 的 SQL 已执行，但写入版本记录失败: %w\n"+
					"· 该迁移的 SQL 已经生效，请核对后手工补记录："+
					"INSERT INTO %s (version) VALUES ('%s');",
				m.version, err, migrationsTable, m.version)
		}

		fmt.Println("完成")
	}
	return nil
}

// execStatements 逐条执行语句。
//
// 为什么逐条而不是整文件一次性执行：出错时能指出**是第几条、内容是什么**。
// 迁移脚本动辄上百行，只报「第 310 行附近语法错误」很难定位；
// 而且 DDL 无法回滚，越早知道断点越容易判断残留状态。
func execStatements(db *sql.DB, version string, stmts []string) error {
	for i, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf(
				"迁移 %s 第 %d/%d 条语句执行失败: %w\n"+
					"· 语句内容: %s\n"+
					"· 该文件**未**被标记为已应用，修正脚本后重新执行即可（前面成功的不会重跑）\n"+
					"· DDL 无法回滚：若已执行了一部分语句，请对照脚本人工确认残留状态",
				version, i+1, len(stmts), err, summarizeStatement(stmt))
		}
	}
	return nil
}

// summarizeStatement 把语句压成单行并截断，用于错误提示。
func summarizeStatement(stmt string) string {
	const maxLen = 200
	s := strings.Join(strings.Fields(stmt), " ")
	if len([]rune(s)) <= maxLen {
		return s
	}
	return string([]rune(s)[:maxLen]) + " ...（已截断）"
}

func exitf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
