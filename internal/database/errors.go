package database

import (
	"errors"

	"github.com/go-sql-driver/mysql"
)

// mysqlDuplicateEntry 唯一约束冲突的错误码（ER_DUP_ENTRY）
const mysqlDuplicateEntry = 1062

// SQLite 的约束冲突错误码（modernc.org/sqlite）。
// 唯一约束是 2067（SQLITE_CONSTRAINT_UNIQUE），主键冲突是 1555。
const (
	sqliteConstraintUnique     = 2067
	sqliteConstraintPrimaryKey = 1555
)

// sqliteCoder SQLite 驱动错误暴露的错误码接口。
//
// 用结构化接口断言而不是 import 该驱动包：本文件属生产代码，
// 不该为了一条判定把测试用的 SQLite 驱动拉进主依赖图。
type sqliteCoder interface {
	Code() int
}

// IsDuplicateKey 判断是否为唯一约束冲突。
//
// 这里用错误码类型断言，而不是匹配 "Duplicate entry" 字符串：
// 原始错误文本会随 MySQL 版本、sql_mode、甚至驱动版本变化，
// 字符串匹配一旦失效是**静默**的 —— 本该返回 400 的友好提示会退回 500，
// 而且没有任何迹象表明匹配逻辑坏了。
//
// 注意：gorm.Config 未开启 TranslateError，所以 GORM 不会把它转成
// gorm.ErrDuplicatedKey，拿到的是驱动原始的 *mysql.MySQLError。
//
// 同时识别 SQLite 的约束冲突码：测试用内存 SQLite（internal/testsupport），
// 不认它的话，所有「重名应返回 400 而不是 500」的翻译在测试环境下都不生效 ——
// 要么用例断言失效，要么只能把断言写得很弱（"400 或 500 都行"），
// 而那正好放过了本函数要防的那类回归。判定方式与 MySQL 保持一致：仍按错误码。
func IsDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == mysqlDuplicateEntry
	}

	var coder sqliteCoder
	if errors.As(err, &coder) {
		code := coder.Code()
		return code == sqliteConstraintUnique || code == sqliteConstraintPrimaryKey
	}
	return false
}
