package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// migration 一个待应用的迁移文件。
type migration struct {
	// version 取自文件名（去掉 .sql），既是排序依据也是幂等标识。
	// 约定形如 2026-09-16-post-tenant —— 日期前缀让字典序等于时间序。
	version string
	path    string
}

// loadMigrations 读取目录下的 .sql 文件，按版本升序返回。
//
// 为什么必须保证顺序：迁移之间存在依赖（先建表、后加列、再建索引），
// 顺序错了会在中途失败，而 DDL 无法回滚。用「日期前缀 + 字典序」
// 是实现稳定排序最简单可靠的方式，因此文件名规范是硬约定 ——
// 不合规的文件直接报错，而不是"尽力猜一个顺序"。
func loadMigrations(dir string) ([]migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// 目录不存在时**不能**返回空列表：那会把「迁移目录被误删或路径配错」
		// 表现成「没有待执行的迁移」，从而安静地跳过整个升级过程。
		return nil, fmt.Errorf("读取迁移目录 %s 失败: %w", dir, err)
	}

	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			// 非 .sql 一律忽略：目录里可能放 README、.bak 备份等
			continue
		}

		version := strings.TrimSuffix(e.Name(), ".sql")
		if err := validateVersion(version); err != nil {
			return nil, fmt.Errorf("迁移文件 %s 命名不合规: %w", e.Name(), err)
		}

		out = append(out, migration{
			version: version,
			path:    filepath.Join(dir, e.Name()),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// validateVersion 校验版本号以合法的 YYYY-MM-DD 开头。
//
// 只要求日期前缀，不强制完整的「日期-描述」格式 ——
// 目的是保证排序正确，而不是限制命名风格。
func validateVersion(v string) error {
	if len(v) < 10 {
		return fmt.Errorf("应以 YYYY-MM-DD 开头（如 2026-09-16-post-tenant）")
	}
	prefix := v[:10]
	if _, err := time.Parse("2006-01-02", prefix); err != nil {
		return fmt.Errorf("日期前缀 %q 不是合法的 YYYY-MM-DD", prefix)
	}
	return nil
}

// defaultDelimiter 默认语句分隔符，与 mysql 客户端一致。
const defaultDelimiter = ";"

// splitStatements 把迁移脚本切成可逐条执行的语句。
//
// 为什么必须自己切分：**DELIMITER 是 mysql 客户端的指令，服务端不认识它**。
// 而本项目用「存储过程 + IF EXISTS 判断」实现条件 DDL（MySQL 5.7 没有
// ADD COLUMN IF NOT EXISTS），过程体内部含分号，必须靠 DELIMITER 才能界定
// 语句边界。此前是把整个文件丢给 db.Exec，一遇到 DELIMITER 就报语法错误 ——
// 也就是说带 DELIMITER 的迁移在迁移工具下**从来没能执行过**。
//
// 切分规则（对齐 mysql 客户端行为）：
//   - 默认分隔符 `;`
//   - 行首的 `DELIMITER xx` 指令只改变分隔符，指令本身不发送给服务端
//   - 字符串（'..' / ".."）、标识符（`..`）、行注释（-- / #）、块注释（/* */）
//     内部出现的分隔符不算语句结束
//   - 注释不进入语句内容
//
// 不做的事：不解析 `/*! ... */` 这类可执行版本注释（当前脚本未使用，
// 若将来引入需要单独处理，否则会被当普通块注释丢弃）。
func splitStatements(content string) ([]string, error) {
	var (
		stmts     []string
		buf       strings.Builder
		delimiter = defaultDelimiter
		atStart   = true // 缓冲区里只有空白，处于「语句开头」
		lineStart = true // 位于行首（DELIMITER 指令只出现在行首）
	)

	flush := func() {
		if s := strings.TrimSpace(buf.String()); s != "" {
			stmts = append(stmts, s)
		}
		buf.Reset()
		atStart = true
	}

	i, n := 0, len(content)
	for i < n {
		c := content[i]

		// 换行：保留并重置行首标记
		if c == '\n' {
			buf.WriteByte(c)
			i++
			lineStart = true
			continue
		}
		// 其余空白：保留，不改变 atStart
		if c == ' ' || c == '\t' || c == '\r' {
			buf.WriteByte(c)
			i++
			continue
		}

		// DELIMITER 指令：仅当位于语句开头且行首时才识别，
		// 避免把列名或字符串里出现的 "delimiter" 误判成指令
		if atStart && lineStart {
			if d, consumed, ok := parseDelimiterDirective(content[i:]); ok {
				flush()
				delimiter = d
				i += consumed
				lineStart = true
				continue
			}
		}
		lineStart = false

		// 行注释 `-- `（MySQL 规定 -- 后必须跟空白）与 `#`
		if c == '-' && i+1 < n && content[i+1] == '-' &&
			(i+2 >= n || content[i+2] == ' ' || content[i+2] == '\t' ||
				content[i+2] == '\n' || content[i+2] == '\r') {
			i = skipToLineEnd(content, i)
			continue
		}
		if c == '#' {
			i = skipToLineEnd(content, i)
			continue
		}
		// 块注释
		if c == '/' && i+1 < n && content[i+1] == '*' {
			j := strings.Index(content[i+2:], "*/")
			if j < 0 {
				return nil, fmt.Errorf("块注释 /* 未闭合")
			}
			i += j + 4
			continue
		}

		// 字符串 / 标识符：整段拷贝，内部不参与分隔符匹配
		if c == '\'' || c == '"' || c == '`' {
			end, err := scanQuoted(content, i)
			if err != nil {
				return nil, err
			}
			buf.WriteString(content[i:end])
			i = end
			atStart = false
			continue
		}

		// 命中当前分隔符 → 一条语句结束
		if strings.HasPrefix(content[i:], delimiter) {
			i += len(delimiter)
			flush()
			continue
		}

		buf.WriteByte(c)
		atStart = false
		i++
	}

	flush()
	return stmts, nil
}

// parseDelimiterDirective 解析 `DELIMITER xx` 指令。
// 返回新分隔符、消费的字节数、是否匹配。消费范围含行尾换行，避免在语句
// 之间留下空行。
func parseDelimiterDirective(s string) (delimiter string, consumed int, ok bool) {
	const kw = "DELIMITER"
	if len(s) < len(kw)+2 || !strings.EqualFold(s[:len(kw)], kw) {
		return "", 0, false
	}

	p := len(kw)
	// 关键字后必须有空白，否则可能是 `delimiterxxx` 这样的标识符
	if s[p] != ' ' && s[p] != '\t' {
		return "", 0, false
	}
	for p < len(s) && (s[p] == ' ' || s[p] == '\t') {
		p++
	}

	start := p
	for p < len(s) && s[p] != '\r' && s[p] != '\n' {
		p++
	}
	delimiter = strings.TrimSpace(s[start:p])
	if delimiter == "" {
		return "", 0, false
	}

	if p < len(s) && s[p] == '\r' {
		p++
	}
	if p < len(s) && s[p] == '\n' {
		p++
	}
	return delimiter, p, true
}

// skipToLineEnd 返回从 i 起的本行末尾位置（指向换行符，或文件末尾）。
func skipToLineEnd(content string, i int) int {
	if j := strings.IndexByte(content[i:], '\n'); j >= 0 {
		return i + j
	}
	return len(content)
}

// scanQuoted 从 s[start]（引号字符）扫到配对的结束引号，返回结束位置（不含）。
//
// 处理两种转义：反斜杠（MySQL 默认开启）与双写（''、""、``）。
// 未闭合时报错而不是静默吞掉 —— 静默吞掉会让后续所有内容被当成一个字符串，
// 分隔符全部失效，最终表现成一条巨大的语句执行失败，很难定位。
func scanQuoted(s string, start int) (int, error) {
	q := s[start]
	i := start + 1
	for i < len(s) {
		switch s[i] {
		case '\\':
			// 反斜杠转义仅对 ' 与 " 有效；反引号标识符里反斜杠是普通字符
			if q == '`' {
				i++
				continue
			}
			i += 2
			continue
		case q:
			// 双写表示一个字面量引号字符
			if i+1 < len(s) && s[i+1] == q {
				i += 2
				continue
			}
			return i + 1, nil
		}
		i++
	}
	return 0, fmt.Errorf("引号 %c 未闭合", q)
}
