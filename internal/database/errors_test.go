package database

import (
	"errors"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// TestIsDuplicateKey 回归保护：唯一约束冲突的识别。
//
// 识别错/识别不到都是静默失效 —— 本该返回 400「XX已存在」的提示会退回
// 500「服务器内部错误」，用户完全不知道是自己重名了，运维也只看到一堆 500。
// 因此这里既验证正例，也验证**不能误判**的反例（连接错误、其他错误码）。
func TestIsDuplicateKey(t *testing.T) {
	dup := &mysql.MySQLError{Number: 1062, Message: "Duplicate entry 'admin' for key 'uk_username'"}

	t.Run("识别裸的 1062", func(t *testing.T) {
		if !IsDuplicateKey(dup) {
			t.Error("1062 应被识别为唯一冲突")
		}
	})

	t.Run("穿透包装后的 1062", func(t *testing.T) {
		// Repository 包装时必须用 `%w: %w`（两个 %w）：第一个保留
		// common.ErrDuplicateKey 供 Service 用 errors.Is 判定，第二个保留驱动
		// 原始错误，使 IsDuplicateKey 对已包装的错误依然可用。
		// 若把第二个写成 %v，原始错误会被字符串化，errors.As 从此找不到
		// *mysql.MySQLError —— 本用例守护的就是这一点。
		wrapped := fmt.Errorf("%w: %w", errors.New("duplicate key"), dup)
		if !IsDuplicateKey(wrapped) {
			t.Error("应能穿透包装识别出 1062")
		}
	})

	t.Run("其他 MySQL 错误码不误判", func(t *testing.T) {
		for _, code := range []uint16{1064, 1146, 1045, 1213, 0} {
			if IsDuplicateKey(&mysql.MySQLError{Number: code, Message: "x"}) {
				t.Errorf("错误码 %d 不应被识别为唯一冲突", code)
			}
		}
	})

	t.Run("非 MySQL 错误不误判", func(t *testing.T) {
		if IsDuplicateKey(errors.New("Duplicate entry 'admin' for key 'uk_username'")) {
			t.Error("纯文本错误不应被识别（这正是选用错误码而非字符串匹配的原因）")
		}
		if IsDuplicateKey(fmt.Errorf("dial tcp 127.0.0.1:3306: connect: connection refused")) {
			t.Error("连接错误不应被识别为唯一冲突")
		}
	})

	t.Run("nil 不 panic 且返回 false", func(t *testing.T) {
		if IsDuplicateKey(nil) {
			t.Error("nil 应返回 false")
		}
	})
}

// codedError 模拟 SQLite 驱动的错误形态（实现 Code() int）。
// 不 import 具体驱动：判定逻辑只依赖这个结构化接口。
type codedError struct{ code int }

func (e codedError) Error() string { return "constraint failed" }
func (e codedError) Code() int     { return e.code }

// TestIsDuplicateKeyRecognizesSQLite SQLite 的约束冲突也必须被识别。
//
// 为什么需要：测试用内存 SQLite，而「唯一键冲突 → 业务错误 400」这条翻译
// 依赖本函数。只认 MySQL 1062 时，所有「重名应返回 400 而不是 500」的用例
// 在测试环境下都会拿到 500 —— 断言要么失效，要么被迫写成「400 或 500 都行」，
// 而那恰好放过了本函数要防的那类回归。
//
// 判定方式仍按错误码（不做字符串匹配），与 MySQL 分支一致。
func TestIsDuplicateKeyRecognizesSQLite(t *testing.T) {
	t.Run("唯一约束 2067", func(t *testing.T) {
		if !IsDuplicateKey(codedError{code: 2067}) {
			t.Error("SQLite 唯一约束冲突应被识别")
		}
	})

	t.Run("主键冲突 1555", func(t *testing.T) {
		if !IsDuplicateKey(codedError{code: 1555}) {
			t.Error("SQLite 主键冲突同样属于重复")
		}
	})

	t.Run("穿透包装", func(t *testing.T) {
		wrapped := fmt.Errorf("%w: %w", errors.New("duplicate key"), codedError{code: 2067})
		if !IsDuplicateKey(wrapped) {
			t.Error("应能穿透包装识别")
		}
	})

	t.Run("其他 SQLite 错误码不误判", func(t *testing.T) {
		// 1299 = SQLITE_CONSTRAINT_NOTNULL，19 = SQLITE_CONSTRAINT（通用）
		for _, code := range []int{1299, 19, 5, 0} {
			if IsDuplicateKey(codedError{code: code}) {
				t.Errorf("错误码 %d 不应被识别为唯一冲突", code)
			}
		}
	})
}

// TestIsDuplicateKeyRealSQLite 用真实 SQLite 库验证（而不是只测假错误类型）。
//
// 假错误类型只能证明「判定逻辑对」，证明不了「驱动抛出的错误确实长这样」——
// 后者才是真正容易变的地方（驱动升级、错误包装层变化）。
func TestIsDuplicateKeyRealSQLite(t *testing.T) {
	type uniqueRow struct {
		ID  uint   `gorm:"primarykey"`
		Key string `gorm:"uniqueIndex"`
	}
	_ = uniqueRow{}

	db, err := gorm.Open(sqlite.Open("file:dupcheck?mode=memory&cache=shared"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("打开内存库失败: %v", err)
	}
	if err := db.AutoMigrate(&uniqueRow{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if err := db.Create(&uniqueRow{Key: "same"}).Error; err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}

	dupErr := db.Create(&uniqueRow{Key: "same"}).Error
	if dupErr == nil {
		t.Fatal("前置条件不成立：唯一索引未生效")
	}
	if !IsDuplicateKey(dupErr) {
		t.Errorf("真实 SQLite 的唯一冲突应被识别，实际错误: %T %v", dupErr, dupErr)
	}
}
