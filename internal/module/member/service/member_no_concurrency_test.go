package service

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"go-admin/internal/cache"
	"go-admin/internal/testsupport"
)

// TestGenerateMemberNoConcurrentInit 并发首次发号不允许产生畸形或重复编号。
//
// 这是「计数器初始化竞态」的回归用例。
//
// 旧实现是「先 INCR，看到 seq==1 再对齐」：并发首次发号时各调用者分别拿到
// seq = 1、2、3…，只有 seq==1 的那个走对齐分支（把计数器对齐到库内最大值），
// 其余直接按 2、3 发号 —— 于是发出去的是 000002、000003 这类编号。
// 危害不只是难看：它们远小于库内已有的 100001+ 区间，属于**与历史编号冲突**
// 的畸形值，会直接撞 uk_member_no 唯一索引，或者在编号被复用后指向错误会员。
//
// 只靠「串行调用两次看结果对不对」是测不出来的 —— 竞态只在并发下出现，
// 所以这里必须真的并发发起，再检查**发出编号的集合**是否合法。
func TestGenerateMemberNoConcurrentInit(t *testing.T) {
	s := newTestMemberService(t)
	testsupport.WithTestRedis(t)

	ctx := context.Background()
	key := "member:no"
	if err := cache.Del(ctx, key); err != nil {
		t.Fatalf("清理计数器失败: %v", err)
	}
	t.Cleanup(func() { _ = cache.Del(ctx, key) })

	const goroutines = 24
	nos := make([]string, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			nos[i], errs[i] = s.generateMemberNo(6)
		}(i)
	}
	wg.Wait()

	// 起点：库内没有会员，所以合法编号从 100001 开始。
	// 100001 本身是本次第一个发出的号，其余应严格大于它。
	const minValid = 100001

	seen := make(map[string]int, goroutines)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 个并发调用发号失败: %v", i, err)
		}
		if nos[i] == "" {
			t.Fatalf("第 %d 个并发调用返回了空编号", i)
		}

		n, convErr := strconv.Atoi(nos[i])
		if convErr != nil {
			t.Fatalf("第 %d 个并发调用返回了非数字编号 %q: %v", i, nos[i], convErr)
		}
		if n < minValid {
			t.Errorf("发出了畸形编号 %q（应 ≥ %d）：说明计数器未初始化就取了号", nos[i], minValid)
		}
		if len(nos[i]) != 6 {
			t.Errorf("编号 %q 位数不符（应为 6 位）", nos[i])
		}

		if prev, dup := seen[nos[i]]; dup {
			t.Errorf("编号 %q 被重复发出（第 %d 与第 %d 个调用）", nos[i], prev, i)
		}
		seen[nos[i]] = i
	}

	// 24 个并发调用应发出 24 个互不相同的编号
	if len(seen) != goroutines {
		t.Errorf("应发出 %d 个不同编号，实际 %d 个", goroutines, len(seen))
	}
}

// TestGenerateMemberNoConcurrentInitAlignsToDBMax 并发首次发号也必须对齐到库内最大值。
//
// 上一版只验证「编号合法且不重复」，但一个「全部从 100001 开始顺延」的实现
// 同样能通过 —— 而它会在库内已有 700000 时发出 100001，直接撞唯一索引。
// 这里把库内最大值垫高，确认并发初始化走的仍是「按库内最大值对齐」。
func TestGenerateMemberNoConcurrentInitAlignsToDBMax(t *testing.T) {
	s := newTestMemberService(t)
	testsupport.WithTestRedis(t)

	ctx := context.Background()
	key := "member:no"
	if err := cache.Del(ctx, key); err != nil {
		t.Fatalf("清理计数器失败: %v", err)
	}
	t.Cleanup(func() { _ = cache.Del(ctx, key) })

	// 垫高库内最大值，强制初始化分支必须读库
	seedMember(t, tenantA, "13300000001", "700000")

	const goroutines = 12
	nos := make([]string, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			nos[i], errs[i] = s.generateMemberNo(6)
		}(i)
	}
	wg.Wait()

	seen := make(map[string]struct{}, goroutines)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 个并发调用发号失败: %v", i, err)
		}
		n, convErr := strconv.Atoi(nos[i])
		if convErr != nil {
			t.Fatalf("第 %d 个并发调用返回了非数字编号 %q", i, nos[i])
		}
		if n <= 700000 {
			t.Errorf("编号 %q 未对齐库内最大值 700000（会与已有编号冲突）", nos[i])
		}
		if _, dup := seen[nos[i]]; dup {
			t.Errorf("编号 %q 被重复发出", nos[i])
		}
		seen[nos[i]] = struct{}{}
	}
}
