package controller

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 分层铁律的**机械化**校验（AGENTS.md 规则 1 / 规则 2）。
//
// 为什么值得写成测试，而不是靠 code review：
// 第三轮审查的结论里，「缺陷清单与修复计划之间没有任何机械校验」被记为
// 比任何单点修复都更值钱的一条 —— 一个 High 级的授权缺陷就这样在
// 「发现」与「计划」之间蒸发了。同样的道理适用于分层铁律：
// `UploadCert` 把扩展名白名单、大小限制、MkdirAll、文件落盘、文件名生成
// 整套业务规则写在 Controller 里，**不违反任何编译器或 linter 的规则**，
// 只是没人恰好 review 到那一行。
//
// 把规则变成会红的断言，它就不再依赖「有人想起来」。

// forbiddenCalls Controller 层不得出现的调用及其原因（规则 1）。
//
// 注意这里只禁「业务动作」，不禁 `c.FormFile`、`c.ShouldBindJSON` 这类
// **参数接收**动作 —— 那正是 Controller 该做的事。
var forbiddenCalls = []struct {
	name   string
	re     *regexp.Regexp
	reason string
}{
	{"直连存储实现", regexp.MustCompile(`\bupload\.[A-Z]`), "存储的读写删应由 Service 调用（Reload/Delete/GetURL/Upload…）"},
	{"自己操作文件系统", regexp.MustCompile(`\bos\.(MkdirAll|Create|Remove|OpenFile)\b`), "落盘路径与权限属于业务规则"},
	{"自己做落盘", regexp.MustCompile(`\bio\.Copy\b`), "文件搬运属于业务规则"},
	{"自己保存上传文件", regexp.MustCompile(`\bSaveUploadedFile\b`), "同 fileService.Upload / configService.UploadCert"},
	{"自己开事务", regexp.MustCompile(`\.Transaction\(`), "事务边界属于 Service（规则 2）"},
	{"自己构造业务错误", regexp.MustCompile(`\bcommon\.New(Biz|NotFound|Forbidden)Error\b`), "业务错误应由 Service 返回，Controller 只用 FailWith 收口"},
}

// forbiddenImports Controller 层不得引入的依赖及其原因。
var forbiddenImports = []struct {
	path   string
	reason string
}{
	{"go-admin/pkg/upload", "Controller 不应直连存储实现"},
	{"gorm.io/gorm", "Controller 不应直连数据库（规则 1）"},
	{"go-admin/internal/module/system/repository", "Controller 不应直连 Repository（规则 1）"},
}

// stripComments 去掉整行注释与行尾注释。
//
// 必须先剥注释再扫描：这些模式在**说明为什么不能这么写**的注释里
// 经常出现（本文件自身就是例子），不剥会满屏误报。
func stripComments(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "//"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	return strings.Join(lines, "\n")
}

// TestControllersHaveNoBusinessLogic 遍历 controller 包的非测试源码，
// 断言其中不含任何业务动作。
func TestControllersHaveNoBusinessLogic(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取包目录失败: %v", err)
	}

	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		scanned++
		src := stripComments(string(raw))

		for _, f := range forbiddenCalls {
			if loc := f.re.FindStringIndex(src); loc != nil {
				line := strings.Count(src[:loc[0]], "\n") + 1
				t.Errorf("%s:%d 出现了 %s（%s）—— 违反规则 1，应下沉到 Service",
					name, line, f.name, f.reason)
			}
		}
		for _, imp := range forbiddenImports {
			if strings.Contains(src, `"`+imp.path+`"`) {
				t.Errorf("%s 引入了 %s（%s）", name, imp.path, imp.reason)
			}
		}
	}

	// 防止「目录读空了但测试照样绿」：扫描不到文件时必须有感知
	if scanned == 0 {
		t.Fatal("没有扫描到任何 controller 源文件，断言形同虚设")
	}
}

// TestStripCommentsRemovesTrailingComment 校验扫描器的前置条件。
//
// stripComments 是上面那条断言的可信度基础 —— 它若不生效（例如正则写错、
// 或改成只匹配行首注释），`// 不要写 os.MkdirAll` 这类**说明性注释**
// 会被当成真实调用，测试立刻变成满屏误报，然后被人加白名单绕过。
func TestStripCommentsRemovesTrailingComment(t *testing.T) {
	src := "func f() {\n\t// os.MkdirAll(x)\n\tx := 1 // upload.Delete(p)\n}"
	got := stripComments(src)

	if strings.Contains(got, "os.MkdirAll") || strings.Contains(got, "upload.Delete") {
		t.Errorf("注释未被剥除干净: %q", got)
	}
	// 代码本体必须保留
	if !strings.Contains(got, "x := 1") {
		t.Errorf("剥注释时误删了代码: %q", got)
	}
}
