package task

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/robfig/cron/v3"
)

// TestWrapWithRecoverCatchesPanic 验证任务 panic 不会外溢。
//
// 这是关键回归测试：robfig/cron 默认不 recover，
// 任务 panic 会直接终止整个进程（Recovery 中间件只管 HTTP，救不了后台任务）。
// 若这里失败，说明恢复能力被破坏，凌晨的清理任务一旦 panic 会导致服务无人值守时挂掉。
func TestWrapWithRecoverCatchesPanic(t *testing.T) {
	var gotSpec string
	var gotPanic interface{}
	var gotStack []byte

	orig := PanicHandler
	defer func() { PanicHandler = orig }()

	PanicHandler = func(spec string, r interface{}, stack []byte) {
		gotSpec, gotPanic, gotStack = spec, r, stack
	}

	wrapped := wrapWithRecover("0 3 * * *", func() {
		panic("模拟任务内部 panic")
	})

	// 关键断言：调用过程本身不能 panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic 未被拦截，直接外溢了: %v", r)
		}
	}()
	wrapped()

	if gotSpec != "0 3 * * *" {
		t.Errorf("PanicHandler 收到的 spec 不正确: %q", gotSpec)
	}
	if gotPanic != "模拟任务内部 panic" {
		t.Errorf("PanicHandler 收到的 panic 值不正确: %v", gotPanic)
	}
	if len(gotStack) == 0 || !strings.Contains(string(gotStack), "goroutine") {
		t.Error("应记录堆栈信息以便定位，实际为空或不含 goroutine")
	}
}

// TestWrapWithRecoverPassesThroughNormalRun 确保正常任务不受包装影响
func TestWrapWithRecoverPassesThroughNormalRun(t *testing.T) {
	called := false
	PanicHandlerCalled := false

	orig := PanicHandler
	defer func() { PanicHandler = orig }()
	PanicHandler = func(string, interface{}, []byte) { PanicHandlerCalled = true }

	wrapWithRecover("test", func() { called = true })()

	if !called {
		t.Error("任务体未被执行")
	}
	if PanicHandlerCalled {
		t.Error("正常执行不应触发 PanicHandler")
	}
}

// TestDefaultPanicHandlerNotNil 默认实现必须存在，避免未注入时静默吞掉 panic
func TestDefaultPanicHandlerNotNil(t *testing.T) {
	if PanicHandler == nil {
		t.Fatal("PanicHandler 默认实现不应为 nil")
	}
}

// ---- 以下为包级状态相关用例，务必先读这段约定 ----

// 包级状态 c / once 的约定：
//
//	once 一旦触发就不会再重建实例，因此「once 已触发 且 c == nil」是**不可恢复**
//	的坏状态 —— 之后任何 AddJob/Start 都会在 nil 上调用方法直接 panic。
//
// 由此推出两条写测试的纪律：
//  1. 需要 cron 实例的用例一律先调 requireCron，它会保证 c 非 nil；
//  2. 需要临时把 c 置 nil 的用例，还原时必须还原成**非 nil**（见
//     TestStopWhenNotInitialized），绝不能写 c = origC 把 nil 放回去。
//
// 这里刻意不保存/还原 once：它是 sync.Once（含锁），赋值会被 go vet 的
// copylocks 拦下。

// restorePanicHandler 保存并恢复注入的 panic 处理器
func restorePanicHandler(t *testing.T) {
	t.Helper()
	orig := PanicHandler
	t.Cleanup(func() { PanicHandler = orig })
}

// requireCron 确保 cron 实例存在并返回它
func requireCron(t *testing.T) *cron.Cron {
	t.Helper()
	Init()
	if c == nil {
		t.Fatal("cron 实例为 nil —— 说明有用例把 c 置 nil 后没有还原，" +
			"此时 once 已触发，后续所有用例都会失败")
	}
	return c
}

// TestDefaultPanicHandlerLogs 未注入自定义 handler 时，默认实现要真的把
// spec / panic 值 / 堆栈写进标准日志。
//
// 兜底路径的典型场景是脚本与测试环境（main.go 之外没有注入 zap）。
// 若它退化成空函数，panic 会被完全静默吞掉 —— 任务不再执行，日志里却
// 一条记录都没有，排查时连「任务曾经跑过」都不知道。
//
// 本用例依赖 PanicHandler 仍是默认实现，因此必须排在所有 Set* 用例之前，
// 且那些用例都要用 restorePanicHandler 还原。
func TestDefaultPanicHandlerLogs(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	PanicHandler("0 3 * * *", "boom", []byte("goroutine 1 [running]"))

	out := buf.String()
	for _, want := range []string{"0 3 * * *", "boom", "goroutine 1 [running]"} {
		if !strings.Contains(out, want) {
			t.Errorf("默认 handler 的输出缺少 %q，实际: %q", want, out)
		}
	}
}

// TestSetPanicHandlerReplaces 注入的 handler 必须生效
func TestSetPanicHandlerReplaces(t *testing.T) {
	restorePanicHandler(t)

	called := false
	SetPanicHandler(func(string, interface{}, []byte) { called = true })

	wrapWithRecover("spec", func() { panic("x") })()

	if !called {
		t.Error("注入的 PanicHandler 未被调用")
	}
}

// TestSetPanicHandlerIgnoresNil 传 nil 时保持原实现不变。
//
// 这一点很关键：nil 一旦被写入，后续每次任务 panic 都会在 recover 分支里
// 调用空函数指针，直接把「任务失败」升级成「进程崩溃」。
func TestSetPanicHandlerIgnoresNil(t *testing.T) {
	restorePanicHandler(t)

	sentinel := false
	SetPanicHandler(func(string, interface{}, []byte) { sentinel = true })

	SetPanicHandler(nil)

	if PanicHandler == nil {
		t.Fatal("SetPanicHandler(nil) 把 handler 置成了 nil —— 任务 panic 会升级为进程崩溃")
	}

	wrapWithRecover("spec", func() { panic("x") })()

	if !sentinel {
		t.Error("SetPanicHandler(nil) 之后原 handler 应保持不变")
	}
}

// TestInitIsIdempotent Init 重复调用只创建一次实例
func TestInitIsIdempotent(t *testing.T) {
	first := requireCron(t)

	Init()

	if c != first {
		t.Error("Init 重复调用不应重建实例（已注册的任务会全部丢失）")
	}
}

// TestAddJobRegistersAndRejectsBadSpec 任务注册的成功与失败路径
func TestAddJobRegistersAndRejectsBadSpec(t *testing.T) {
	requireCron(t)

	ran := false
	id, err := AddJob("0 3 * * *", func() { ran = true })
	if err != nil {
		t.Fatalf("合法 cron 表达式不应报错: %v", err)
	}
	if id == 0 {
		t.Error("注册成功应返回非零 EntryID，调用方据此可反注册")
	}
	if ran {
		t.Error("注册不应立即执行任务体（凌晨任务在注册时就跑起来是灾难）")
	}

	if _, err := AddJob("这不是 cron 表达式", func() {}); err == nil {
		t.Error("非法 cron 表达式必须返回错误，而不是静默注册一个永不触发的任务")
	}
}

// TestStartAndStop Start 之后必须能 Stop，且 Stop 可重复调用。
//
// Stop 不可重入会让「优雅退出时重复触发」变成 panic。
func TestStartAndStop(t *testing.T) {
	requireCron(t)

	Start()
	Stop()
	Stop() // 重复调用不应 panic
}

// TestStopWhenNotInitialized 实例为 nil 时 Stop 不应 panic。
//
// 服务在初始化早期失败（如配置校验不通过）时会直接走到退出清理逻辑，
// 此时 cron 可能还没 Init，Stop 必须容忍 nil。
//
// 还原时特意还原成**非 nil**：见文件上方关于 once 的约定 ——
// 把 nil 放回去会让之后所有用例在 nil 上 panic。
func TestStopWhenNotInitialized(t *testing.T) {
	alive := requireCron(t)

	c = nil
	t.Cleanup(func() { c = alive })

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("c 为 nil 时 Stop 不应 panic: %v", r)
		}
	}()
	Stop()
}
