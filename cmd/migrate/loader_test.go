package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMigration(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
}

// TestLoadMigrationsSortsByVersion 按版本升序返回，与文件在磁盘上的顺序无关。
//
// 这是执行器的正确性基础：迁移之间有依赖（先建表、后加列），
// 顺序错了会在中途失败，而 DDL 无法回滚。
func TestLoadMigrationsSortsByVersion(t *testing.T) {
	dir := t.TempDir()
	// 刻意乱序写入
	writeMigration(t, dir, "2026-09-16-post-tenant.sql", "SELECT 1;")
	writeMigration(t, dir, "2026-09-15-schema.sql", "SELECT 1;")
	writeMigration(t, dir, "2026-09-15-settings-menu-permission.sql", "SELECT 1;")

	got, err := loadMigrations(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("应加载 3 个迁移，got %d", len(got))
	}

	want := []string{
		"2026-09-15-schema",
		"2026-09-15-settings-menu-permission",
		"2026-09-16-post-tenant",
	}
	for i, w := range want {
		if got[i].version != w {
			t.Errorf("第 %d 个版本应为 %s，got %s", i, w, got[i].version)
		}
	}
}

// TestLoadMigrationsIgnoresNonSQL 非 .sql 文件必须被忽略。
// 迁移目录里常会放 README、.bak 备份，误当迁移执行会出事故。
func TestLoadMigrationsIgnoresNonSQL(t *testing.T) {
	dir := t.TempDir()
	writeMigration(t, dir, "2026-09-15-schema.sql", "SELECT 1;")
	writeMigration(t, dir, "README.md", "# 迁移说明")
	writeMigration(t, dir, "2026-09-15-schema.sql.bak", "旧版本内容")

	got, err := loadMigrations(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("应只加载 1 个迁移，got %d", len(got))
	}
	if got[0].version != "2026-09-15-schema" {
		t.Errorf("版本应为 2026-09-15-schema，got %s", got[0].version)
	}
}

// TestLoadMigrationsRejectsBadName 命名不合规必须报错，而不是"尽力猜顺序"。
//
// 版本号同时承担排序与幂等标识两个职责：格式错了会安静地跑乱顺序，
// 而这类错误在 DDL 无法回滚的前提下代价很高。宁可在启动阶段拒绝。
func TestLoadMigrationsRejectsBadName(t *testing.T) {
	cases := []string{
		"schema.sql",         // 无日期前缀
		"2026-13-45-bad.sql", // 月份日期越界
		"abcd-ef-gh-x.sql",   // 前缀不是日期
		"x.sql",              // 太短
	}

	for _, file := range cases {
		t.Run(file, func(t *testing.T) {
			dir := t.TempDir()
			writeMigration(t, dir, file, "SELECT 1;")

			if _, err := loadMigrations(dir); err == nil {
				t.Errorf("命名 %s 应被拒绝", file)
			}
		})
	}
}

// TestLoadMigrationsMissingDir 目录不存在时必须报错。
//
// 若返回空列表，「迁移目录被误删 / -config 配错导致工作目录不对」
// 就会表现成「没有待执行的迁移」，从而安静地跳过整个升级过程 ——
// 这正是加执行器要解决的问题，不能自己再制造一个。
func TestLoadMigrationsMissingDir(t *testing.T) {
	if _, err := loadMigrations(filepath.Join(t.TempDir(), "not-exist")); err == nil {
		t.Fatal("目录不存在时应返回错误")
	}
}

func TestLoadMigrationsEmptyDir(t *testing.T) {
	got, err := loadMigrations(t.TempDir())
	if err != nil {
		t.Fatalf("空目录不应报错: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("空目录应返回空列表，got %d", len(got))
	}
}

// TestValidateVersionAcceptsRealNames 用仓库里真实的迁移文件名做回归，
// 避免「规则改了但既有文件反而不合规」这种自伤。
func TestValidateVersionAcceptsRealNames(t *testing.T) {
	names := []string{
		"2026-09-15-schema",
		"2026-09-15-settings-menu-permission",
		"2026-09-16-post-tenant",
		"2026-09-16", // 只有日期也应接受：规则只约束前缀
	}
	for _, n := range names {
		if err := validateVersion(n); err != nil {
			t.Errorf("%s 应被接受，却报错: %v", n, err)
		}
	}
}

// TestSplitStatementsBasic 无 DELIMITER 时按分号切分，注释被丢弃。
func TestSplitStatementsBasic(t *testing.T) {
	content := `-- 文件头注释
CREATE TABLE a (id int); -- 行尾注释
# 另一种注释
/* 块
   注释 */
INSERT INTO a VALUES (1);

CREATE TABLE b (id int);
`
	got, err := splitStatements(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"CREATE TABLE a (id int)",
		"INSERT INTO a VALUES (1)",
		"CREATE TABLE b (id int)",
	}
	if len(got) != len(want) {
		t.Fatalf("应切出 %d 条语句，实际 %d 条: %q", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 条不符:\n got: %q\nwant: %q", i+1, got[i], want[i])
		}
	}
}

// TestSplitStatementsRespectsDelimiter 存储过程体内部的分号不能当作语句结束。
//
// 这是本次修复的核心：DELIMITER 是客户端指令，服务端不认识，
// 迁移工具此前整文件丢给 db.Exec，带 DELIMITER 的脚本从未能执行成功。
func TestSplitStatementsRespectsDelimiter(t *testing.T) {
	content := `DROP PROCEDURE IF EXISTS ` + "`p`" + `;

DELIMITER $$
CREATE PROCEDURE ` + "`p`" + `()
BEGIN
    IF EXISTS (SELECT 1 FROM t WHERE id = 1) THEN
        ALTER TABLE t DROP COLUMN c;
    END IF;
END$$

DELIMITER ;

CALL ` + "`p`" + `();
DROP PROCEDURE ` + "`p`" + `;
`
	got, err := splitStatements(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("应切出 4 条语句，实际 %d 条:\n%s", len(got), strings.Join(got, "\n---\n"))
	}

	// 过程体必须完整落在一条语句里（含内部 2 个分号：ALTER 与 END IF）
	if !strings.HasPrefix(got[1], "CREATE PROCEDURE") {
		t.Errorf("第 2 条应是 CREATE PROCEDURE，实际: %q", got[1])
	}
	if strings.Count(got[1], ";") != 2 {
		t.Errorf("过程体内部的分号应保留在语句内，实际 %q", got[1])
	}
	if !strings.HasSuffix(got[1], "END") {
		t.Errorf("过程体应以 END 结尾（$$ 不是语句内容），实际: %q", got[1])
	}

	// DELIMITER 指令本身不能发给服务端
	for i, s := range got {
		if strings.Contains(strings.ToUpper(s), "DELIMITER") {
			t.Errorf("第 %d 条语句里残留了 DELIMITER 指令: %q", i+1, s)
		}
		if strings.Contains(s, "$$") {
			t.Errorf("第 %d 条语句里残留了自定义分隔符: %q", i+1, s)
		}
	}

	// DELIMITER ; 之后必须恢复默认分隔符
	if got[2] != "CALL `p`()" {
		t.Errorf("第 3 条应是 CALL，实际: %q", got[2])
	}
}

// TestSplitStatementsIgnoresSemicolonInLiterals 字符串与反引号标识符里的分号
// 不能切断语句 —— 否则一条正常的 INSERT 会被劈成两条语法错误的语句。
func TestSplitStatementsIgnoresSemicolonInLiterals(t *testing.T) {
	content := "INSERT INTO t VALUES ('a;b', \"c;d\", `e;f`);\n" +
		"INSERT INTO t VALUES ('it''s;ok');\n" +
		"INSERT INTO t VALUES ('back\\slash;ok');\n"

	got, err := splitStatements(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("应切出 3 条语句，实际 %d 条: %q", len(got), got)
	}
	if !strings.Contains(got[0], "'a;b'") || !strings.Contains(got[0], "`e;f`") {
		t.Errorf("引号内的分号应保留在原语句中，实际: %q", got[0])
	}
}

// TestSplitStatementsReportsUnclosedQuote 引号未闭合必须报错。
//
// 静默吞掉的后果很严重：后面所有内容都会被当成一个字符串，
// 分隔符全部失效，最终表现成一条巨大的语句执行失败，极难定位。
func TestSplitStatementsReportsUnclosedQuote(t *testing.T) {
	for _, content := range []string{
		"INSERT INTO t VALUES ('unterminated);",
		"SELECT \"unterminated;",
		"SELECT `unterminated;",
		"/* 未闭合的块注释\nSELECT 1;",
	} {
		if _, err := splitStatements(content); err == nil {
			t.Errorf("内容 %q 应报错，实际通过", content)
		}
	}
}

// TestSplitStatementsOnRealMigrations 用仓库里真实的迁移脚本验证切分。
//
// 比构造样例更有价值：这两个文件正是此前执行失败的脚本，
// 断言「过程体完整成句」能直接防住回归。
func TestSplitStatementsOnRealMigrations(t *testing.T) {
	files := []string{
		"2026-09-15-schema.sql",
		"2026-09-16-post-tenant.sql",
	}

	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("..", "..", "sql", "migrations", name))
			if err != nil {
				t.Fatalf("读取迁移文件失败: %v", err)
			}

			stmts, err := splitStatements(string(content))
			if err != nil {
				t.Fatalf("切分失败: %v", err)
			}
			if len(stmts) == 0 {
				t.Fatal("未切出任何语句")
			}

			var createProc int
			for _, s := range stmts {
				if strings.Contains(strings.ToUpper(s), "DELIMITER") {
					t.Errorf("语句里残留 DELIMITER 指令: %q", s)
				}
				if strings.HasPrefix(s, "CREATE PROCEDURE") {
					createProc++
					if !strings.HasSuffix(s, "END") {
						t.Errorf("CREATE PROCEDURE 未完整成句（可能被内部 ; 切断）: ...%q",
							s[len(s)-40:])
					}
					if !strings.Contains(s, "BEGIN") {
						t.Errorf("CREATE PROCEDURE 缺少过程体: %q", s)
					}
				}
			}
			if createProc != 1 {
				t.Errorf("应恰好切出 1 条 CREATE PROCEDURE，实际 %d 条", createProc)
			}
		})
	}
}
