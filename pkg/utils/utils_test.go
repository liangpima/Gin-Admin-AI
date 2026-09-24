package utils

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- 密码哈希 ----

// TestHashPasswordRoundTrip 密码哈希的基本契约。
//
// 这里守护的是「不可明文存储 + 能校验通过」这两条底线：
// 一旦 HashPassword 退化成原样返回，用户密码就会以明文进库，
// 而 CheckPassword 仍然会通过，功能测试完全发现不了。
func TestHashPasswordRoundTrip(t *testing.T) {
	const plain = "s3cret-密码-123"

	hash, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword 出错: %v", err)
	}
	if hash == plain {
		t.Fatal("哈希结果与明文相同 —— 密码会以明文进库")
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("应为 bcrypt 格式（$2a$/$2b$/$2y$ 开头），实际 %q", hash)
	}
	if !CheckPassword(plain, hash) {
		t.Error("正确密码应校验通过")
	}
	if CheckPassword("wrong-password", hash) {
		t.Error("错误密码必须校验失败")
	}
	// 多字节密码不能被按字节截断后仍通过（bcrypt 有 72 字节上限，
	// 这里只确认常规中文密码完整参与校验）
	if CheckPassword("s3cret-密码", hash) {
		t.Error("前缀相同的短密码不应通过")
	}
}

// TestHashPasswordUsesRandomSalt 同一密码两次哈希必须不同（盐随机）。
//
// 若盐被写成固定值，撞库时同一密码在所有账号上哈希一致，
// 攻击者可以一次性比对全表，bcrypt 的意义就没了。
func TestHashPasswordUsesRandomSalt(t *testing.T) {
	const plain = "same-password"

	first, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword 出错: %v", err)
	}
	second, err := HashPassword(plain)
	if err != nil {
		t.Fatalf("HashPassword 出错: %v", err)
	}
	if first == second {
		t.Error("同一密码两次哈希结果相同 —— 盐不是随机的")
	}
	// 两份不同的哈希都要能校验原密码
	if !CheckPassword(plain, first) || !CheckPassword(plain, second) {
		t.Error("带随机盐的哈希应都能校验出原密码")
	}
}

// TestCheckPasswordRejectsMalformedHash 非法哈希必须返回 false 而不是 panic。
//
// 校验发生在登录路径上，库里的哈希字段若被人工改坏、被截断或为空，
// panic 会让整个登录接口 500（甚至拖垮进程），而正确行为是「校验不通过」。
func TestCheckPasswordRejectsMalformedHash(t *testing.T) {
	cases := map[string]string{
		"空字符串":      "",
		"非 bcrypt 文本": "not-a-bcrypt-hash",
		"截断的 bcrypt":  "$2a$10$short",
		"明文密码":       "admin123",
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("非法哈希导致 panic: %v", r)
				}
			}()
			if CheckPassword("admin123", bad) {
				t.Error("非法哈希必须返回 false")
			}
		})
	}
}

// ---- 字符串工具 ----

// TestRandomStringLengthAndCharset 长度精确、且只含十六进制字符。
//
// 长度算错会直接影响调用方：验证码/随机密码/token 片段都按固定长度
// 切片或落库（如 varchar(n)），少一位会截断、多一位会插入失败。
func TestRandomStringLengthAndCharset(t *testing.T) {
	const hexChars = "0123456789abcdef"

	for _, n := range []int{0, 1, 2, 7, 8, 32, 64} {
		got := RandomString(n)
		if len(got) != n {
			t.Errorf("RandomString(%d) 长度为 %d，期望 %d", n, len(got), n)
		}
		if strings.Trim(got, hexChars) != "" {
			t.Errorf("RandomString(%d) 含非十六进制字符: %q", n, got)
		}
	}
}

// TestRandomStringIsNotConstant 连续取值不应重复（来自 crypto/rand）。
func TestRandomStringIsNotConstant(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		s := RandomString(16)
		if _, dup := seen[s]; dup {
			t.Fatalf("第 %d 次生成了重复值 %q —— 随机源可能被写死", i, s)
		}
		seen[s] = struct{}{}
	}
}

func TestContains(t *testing.T) {
	slice := []string{"admin", "editor", "viewer"}

	if !Contains(slice, "editor") {
		t.Error("存在的元素应返回 true")
	}
	if Contains(slice, "Admin") {
		t.Error("Contains 是精确匹配，大小写不同不应命中")
	}
	if Contains(slice, "") {
		t.Error("空串不在切片中，不应命中")
	}
	if Contains(nil, "admin") {
		t.Error("nil 切片不应命中任何元素")
	}
	if Contains([]string{}, "admin") {
		t.Error("空切片不应命中任何元素")
	}
}

// TestRemoveDuplicatesKeepsOrder 去重且保持首次出现的顺序。
//
// 顺序敏感：调用方常把它用于「权限码/角色码」这类要去重后展示或
// 拼接成 SQL IN 的列表，顺序变了会让日志与界面难以对照。
func TestRemoveDuplicatesKeepsOrder(t *testing.T) {
	got := RemoveDuplicates([]string{"c", "a", "b", "a", "c", "a"})
	want := []string{"c", "a", "b"}

	if len(got) != len(want) {
		t.Fatalf("去重后为 %v，期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("去重后为 %v，期望 %v（顺序必须保持首次出现顺序）", got, want)
		}
	}
}

func TestRemoveDuplicatesEdgeCases(t *testing.T) {
	if got := RemoveDuplicates(nil); len(got) != 0 {
		t.Errorf("nil 输入应返回空切片，实际 %v", got)
	}
	if got := RemoveDuplicates([]string{}); len(got) != 0 {
		t.Errorf("空输入应返回空切片，实际 %v", got)
	}
	if got := RemoveDuplicates([]string{"x", "x", "x"}); len(got) != 1 || got[0] != "x" {
		t.Errorf("全同输入应只留一个元素，实际 %v", got)
	}
}

func TestCamelToSnake(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"user":      "user",
		"UserName":  "user_name",
		"userName":  "user_name",
		"user_name": "user_name",
		"userID":    "user_i_d",
		"A":         "a",
	}
	for in, want := range cases {
		if got := CamelToSnake(in); got != want {
			t.Errorf("CamelToSnake(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestCamelToSnakeSplitsEveryUppercase 记录当前实现的行为：每个大写字母
// 前都插下划线，不识别连续大写缩写。
//
// 之所以显式断言（而不是当成 bug 修掉）：这个方法用于按结构体字段名
// 推导列名，行为一旦变化，既有映射会静默错位。若将来要支持缩写
// （HTTPServer → http_server），必须同步检查所有调用点，本用例会提醒改动者。
func TestCamelToSnakeSplitsEveryUppercase(t *testing.T) {
	if got := CamelToSnake("HTTPServer"); got != "h_t_t_p_server" {
		t.Errorf("CamelToSnake(\"HTTPServer\") = %q，期望 \"h_t_t_p_server\"；"+
			"若有意改成识别缩写，请一并核对所有调用点的列名映射", got)
	}
}

// ---- 雪花 ID ----

// TestGenerateIDLazyInitWhenNotInitialized 未显式初始化时 GenerateID 应自建实例。
//
// 这条路径在生产里只会在「忘记调用 InitSnowflake」时走到，
// 若它坏掉，表现是启动后第一个 ID 生成请求直接 panic（sf 为 nil）。
func TestGenerateIDLazyInitWhenNotInitialized(t *testing.T) {
	sf = nil
	sfOnce = sync.Once{}
	t.Cleanup(func() {
		sf = nil
		sfOnce = sync.Once{}
	})

	id := GenerateID()

	if sf == nil {
		t.Fatal("GenerateID 的懒初始化没有建立实例")
	}
	if id <= 0 {
		t.Errorf("ID 应为正数，实际 %d", id)
	}
}

// TestNextIDMonotonicOnClockRollback 时钟回拨时 ID 仍必须单调递增。
//
// 回拨（NTP 校正、手工改时间）后若直接用当前时间，会生成比上一批更小的
// ID —— 对外表现为「新数据的主键比旧数据小」，排序与增量同步全部错乱。
// 实现的做法是「不退到 lastTime 之前」，本用例把它钉住。
func TestNextIDMonotonicOnClockRollback(t *testing.T) {
	future := time.Now().UnixMilli() + 2
	s := &Snowflake{machineID: 3, lastTime: future}

	first := s.NextID()
	second := s.NextID()

	if first <= 0 || second <= 0 {
		t.Fatalf("ID 应为正数，实际 first=%d second=%d", first, second)
	}
	if second <= first {
		t.Errorf("时钟回拨后 ID 必须仍单调递增: first=%d second=%d", first, second)
	}
}

// TestNextIDWaitsWhenSequenceExhausted 同一毫秒内序列号用尽时应等到下一毫秒，
// 而不是把序列号归零后复用 —— 复用会直接产生重复 ID。
//
// 构造手法：把 lastTime 设成「比当前时间略晚」，序列号置为最大值，
// 这样一次调用就会走完「回拨保护 → 序列号进位归零 → 等待下一毫秒」整条路径。
func TestNextIDWaitsWhenSequenceExhausted(t *testing.T) {
	s := &Snowflake{
		machineID: 1,
		sequence:  maxSequence,
		lastTime:  time.Now().UnixMilli() + 3,
	}

	start := time.Now()
	id := s.NextID()
	elapsed := time.Since(start)

	if id <= 0 {
		t.Fatalf("ID 应为正数，实际 %d", id)
	}
	if elapsed < 3*time.Millisecond {
		t.Errorf("序列号用尽后应立即等待到下一毫秒，实际仅耗时 %v（可能复用了已用尽的序列号）", elapsed)
	}
	if s.sequence != 0 {
		t.Errorf("进入新的一毫秒后序列号应归零，实际 %d", s.sequence)
	}
}
