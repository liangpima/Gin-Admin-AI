package router

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 本文件把「swagger 文档」与「实际注册的路由」拉齐比对，并作为**可执行的门禁**。
//
// 为什么需要它，而 `make swagger` + `git diff --exit-code -- docs/` 不够：
// 那道门禁只查「生成物是否与注解一致」。如果新接口**一条 swagger 注解都没写**，
// 重新生成出来的 docs 与仓库里的完全相同 —— `git diff` 为空，门禁静默通过，
// 于是文档覆盖率可以一路下滑而无人察觉。
//
// 此前这项核对是一个放在 `runtime/cov/` 下的一次性 Python 脚本，而
// `runtime/` 在 .gitignore 里 —— 也就是说它**从未入库**，不受任何门禁约束，
// 换台机器就没了。这里把它重写成 Go 测试，跟随 `go test ./...` 一起在 CI 上跑。
//
// 两个方向都要查，且都是**阻断项**：
//  1. 文档里有、实际没注册 → 点「Try it out」必然 404（最典型的成因是
//     `@Router` 里重复写了 basePath：`/api/v1/api/v1/system/user`）。
//  2. 实际有、文档里没写 → 文档覆盖率下降，接口使用者无从得知。
//
// ⚠️ testsupport 不参与：本测试不碰数据库与 Redis。router.Setup 只做路由注册，
// 各 Controller 构造 Service/Repository 时只是捕获 database.DB 引用，不发起查询。

// ginPathParamRe 把 gin 的路径参数写法 `:id` / `*filepath` 归一成 OpenAPI 的 `{id}`。
//
// 两套写法本来就不同（gin 用 `:name`，OpenAPI 用 `{name}`），
// 不归一化会把每一条带参数的路由都报成「文档路径错误」—— 全是假阳性。
var ginPathParamRe = regexp.MustCompile(`[:*]([A-Za-z_][A-Za-z0-9_]*)`)

func normalizeRoutePath(p string) string {
	return ginPathParamRe.ReplaceAllString(p, "{$1}")
}

// swaggerDoc 只取比对需要的字段。
type swaggerDoc struct {
	BasePath string `json:"basePath"`
	Paths    map[string]map[string]json.RawMessage
}

// ignoredRoute 判断是否属于「非业务路由」，不参与比对。
//
// ⚠️ 不能把 "/" 放进前缀表：`"/api/v1/x".startswith("/")` 恒为真，
// 会把所有路由都过滤掉，然后报出「全部都有文档」这种漂亮但完全错误的结论。
// （原 Python 脚本初版正是这么写的，靠打印计数才看出来。）
var ignoredPrefixes = []string{"/swagger", "/uploads", "/health"}

func ignoredRoute(path string) bool {
	if path == "/" {
		return true
	}
	for _, p := range ignoredPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// TestSwaggerCoversEveryBusinessRoute swagger 文档与实际路由必须完全对齐。
func TestSwaggerCoversEveryBusinessRoute(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "swagger.json"))
	if err != nil {
		t.Fatalf("读取 docs/swagger.json 失败（docs/ 是入库的生成物，缺失说明被误删）: %v", err)
	}

	var doc swaggerDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("解析 docs/swagger.json 失败: %v", err)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("docs/swagger.json 里没有任何 path —— 生成物可疑，不能当作「全部有文档」通过")
	}

	// 实际注册的路由
	r := Setup(gin.TestMode)
	registered := make(map[string]bool)
	for _, rt := range r.Routes() {
		registered[rt.Method+" "+normalizeRoutePath(rt.Path)] = true
	}

	// 文档声明的接口（补上 basePath：Swagger UI 会自动把它拼在每个 path 前）
	documented := make(map[string]string) // key -> 原始文档 path
	for path, ops := range doc.Paths {
		full := strings.ReplaceAll(doc.BasePath+path, "//", "/")
		for method := range ops {
			documented[strings.ToUpper(method)+" "+full] = path
		}
	}

	if len(registered) == 0 {
		t.Fatal("Setup 没有注册任何路由，用例前置条件不成立")
	}

	var documentedButMissing []string
	for key, origPath := range documented {
		if registered[key] {
			continue
		}
		if ignoredRoute(strings.SplitN(key, " ", 2)[1]) {
			continue
		}
		documentedButMissing = append(documentedButMissing,
			key+"  (文档 @Router 写的是 "+origPath+")")
	}

	var registeredButUndocumented []string
	for key := range registered {
		if _, ok := documented[key]; ok {
			continue
		}
		path := strings.SplitN(key, " ", 2)[1]
		if ignoredRoute(path) || !strings.HasPrefix(path, doc.BasePath) {
			continue
		}
		registeredButUndocumented = append(registeredButUndocumented, key)
	}

	sort.Strings(documentedButMissing)
	sort.Strings(registeredButUndocumented)

	if len(documentedButMissing) > 0 {
		t.Errorf("文档里有、实际没注册的接口 %d 条（点了必然 404）:\n  %s\n"+
			"最常见的原因是 @Router 里重复写了 basePath，例如 /api/v1/system/user 应为 /system/user",
			len(documentedButMissing), strings.Join(documentedButMissing, "\n  "))
	}
	if len(registeredButUndocumented) > 0 {
		t.Errorf("实际注册但文档未覆盖的业务接口 %d 条（缺 @Router/@Summary 等注解）:\n  %s\n"+
			"补齐注解后执行 make swagger 并提交 docs/",
			len(registeredButUndocumented), strings.Join(registeredButUndocumented, "\n  "))
	}

	// 计数打印出来是为了让「全部通过」这件事可核对 ——
	// 过滤条件写错时（例如误把 "/" 当前缀）这里会显示 0 条参与比对，
	// 一眼就能看出结论是假的。
	t.Logf("参与比对：真实注册路由 %d 条，文档声明接口 %d 条", len(registered), len(documented))
}

// TestSwaggerSecuritySchemesAreDefined 文档引用的安全方案必须都已定义。
//
// 未定义的安全方案不会报错：Swagger UI 的 Authorize 按钮不会为这些接口
// 附加 Authorization 头，表现是「Try it out」永远 401，而文档本身看起来完全正常。
// 本项目实际出现过 4 处（3 处写成 ApiKeyAuth、1 处写成 BearerApiAuth，
// 而 securityDefinitions 里只定义了 BearerAuth）。
func TestSwaggerSecuritySchemesAreDefined(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "swagger.json"))
	if err != nil {
		t.Fatalf("读取 docs/swagger.json 失败: %v", err)
	}

	var doc struct {
		SecurityDefinitions map[string]json.RawMessage `json:"securityDefinitions"`
		Paths               map[string]map[string]struct {
			Security []map[string][]string `json:"security"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("解析 docs/swagger.json 失败: %v", err)
	}

	if len(doc.SecurityDefinitions) == 0 {
		t.Fatal("securityDefinitions 为空，用例前置条件不成立")
	}

	undefined := make(map[string][]string) // 方案名 -> 引用它的接口
	for path, ops := range doc.Paths {
		for method, op := range ops {
			for _, requirement := range op.Security {
				for scheme := range requirement {
					if _, ok := doc.SecurityDefinitions[scheme]; ok {
						continue
					}
					undefined[scheme] = append(undefined[scheme], strings.ToUpper(method)+" "+path)
				}
			}
		}
	}

	if len(undefined) > 0 {
		var lines []string
		for scheme, refs := range undefined {
			sort.Strings(refs)
			lines = append(lines, scheme+" 被引用于: "+strings.Join(refs, ", "))
		}
		sort.Strings(lines)
		t.Errorf("文档引用了未定义的安全方案 %d 个:\n  %s\n"+
			"修正对应 Controller 的 @Security 注解（应为 BearerAuth），再执行 make swagger",
			len(undefined), strings.Join(lines, "\n  "))
	}
}
