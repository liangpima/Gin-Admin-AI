package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 规则 2 的机械化校验：Service 层不得依赖 HTTP 框架。
//
// 规则原文是「禁止：直接返回 HTTP 响应、操作 gin.Context、引入 net/http 相关依赖」。
// 依赖一旦引入就收不回去了（改起来要动一整个模块），所以放在这里当门禁。
//
// 注意排除 `_test.go`：测试需要用 httptest 构造请求、用 net/http 拿状态码常量，
// 那是测试脚手架，不是生产依赖。
func TestServicesDoNotDependOnHTTP(t *testing.T) {
	pkgDirs, err := filepath.Glob(filepath.Join("..", "..", "*", "service"))
	if err != nil {
		t.Fatalf("定位 service 目录失败: %v", err)
	}
	if len(pkgDirs) == 0 {
		t.Fatal("没有找到任何 service 目录，断言形同虚设")
	}

	forbidden := []struct {
		path   string
		reason string
	}{
		{"github.com/gin-gonic/gin", "Service 不得操作 gin.Context"},
		{"net/http", "Service 不得关心 HTTP 语义（响应码、Request 等）"},
	}

	scanned := 0
	for _, dir := range pkgDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("读取 %s/%s 失败: %v", dir, name, err)
			}
			scanned++
			src := stripGoComments(string(raw))

			for _, imp := range forbidden {
				if strings.Contains(src, `"`+imp.path+`"`) {
					t.Errorf("%s/%s 引入了 %s（违反规则 2：%s）", dir, name, imp.path, imp.reason)
				}
			}
		}
	}

	if scanned == 0 {
		t.Fatal("没有扫描到任何 service 源文件，断言形同虚设")
	}
}

// stripGoComments 去掉整行注释与行尾注释（同 controller 包的实现，两边独立）。
func stripGoComments(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "//"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	return strings.Join(lines, "\n")
}
