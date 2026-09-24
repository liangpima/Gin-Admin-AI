package common

import "testing"

// TestEscapeLike 锁定 LIKE 模式转义（P1-4）。
//
// 未转义时用户输入里的 % 与 _ 会改写匹配语义：搜 `%%%` 等价于「匹配全部记录」，
// 既能让攻击者探测数据量，也把本应受限的模糊搜索变成全表扫描。
// 参数始终是绑定的，所以这不是注入，而是「模式注入」。
func TestEscapeLike(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"普通文本原样返回", "alice", "alice"},
		{"中文原样返回", "张三", "张三"},
		{"百分号被转义", "%", `\%`},
		{"下划线被转义", "_", `\_`},
		{"反斜杠被转义", `\`, `\\`},
		{"全通配符模式被中和", "%%%", `\%\%\%`},
		{"混合输入逐字符处理", "a%b_c", `a\%b\_c`},
		// 关键用例：反斜杠必须与 % 一起被转义，否则输入 `\%` 会让 % 恢复通配语义
		{"反斜杠加百分号不能互相抵消", `\%`, `\\\%`},
		{"前后缀保留为字面量", "100%off", `100\%off`},
		{"空串原样返回", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EscapeLike(tc.in); got != tc.want {
				t.Errorf("EscapeLike(%q) = %q，期望 %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestEscapeLikeIsIdempotentForPlainText 无特殊字符时必须是零开销的恒等变换。
//
// 全仓 18 处 LIKE 都走这个函数，绝大多数输入是普通关键字，
// 不该为它们分配新字符串（实现里先做 ContainsAny 短路就是这个意思）。
func TestEscapeLikeIsIdempotentForPlainText(t *testing.T) {
	in := "普通关键字abc123"
	if got := EscapeLike(in); got != in {
		t.Errorf("无特殊字符的输入应原样返回，实际 %q", got)
	}
}
