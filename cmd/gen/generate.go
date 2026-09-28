package main

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// 模板分隔符刻意用 [[ ]] 而不是默认的 {{ }}：
// 前端模板里满是 Vue 的插值（`{{ row.status }}`），用默认分隔符会让
// 每个插值都要转义成 `{{"{{"}}`，可读性极差且极易漏。
var tmpl = template.Must(
	template.New("module").Delims("[[", "]]").ParseFS(templateFS, "templates/*.tmpl"),
)

// 模块名约束：小写字母开头，只含小写字母/数字/下划线。
// 直接拼进文件名、Go 标识符与路由路径，放宽会生成编译不过的代码。
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,30}$`)

type moduleData struct {
	Name   string // 蛇形（也是文件名、表名、路由前缀）：order_item
	Pascal string // 大驼峰（Go 导出标识符）：OrderItem
	Camel  string // 小驼峰（变量名）：orderItem
	Title  string // 中文名（注释与前端文案）：订单项
	Table  string // 表名
}

func newModuleData(name, title string) moduleData {
	return moduleData{
		Name:   name,
		Pascal: pascalCase(name),
		Camel:  camelCase(name),
		Title:  title,
		Table:  name,
	}
}

func pascalCase(s string) string {
	var b strings.Builder
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' }) {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	return b.String()
}

func camelCase(s string) string {
	p := pascalCase(s)
	if p == "" {
		return p
	}
	return strings.ToLower(p[:1]) + p[1:]
}

// renderedFile 一个待写出的文件。
type renderedFile struct {
	// relPath 相对仓库根
	relPath string
	content string
	// lf 为 true 时按 LF 写出（不转 CRLF）。
	// sql/migrations 既有文件在索引里是 LF，且 CI 的 check-migrations.sh
	// 会把它们原样喂给 mysql —— 与多数保持一致，避免无意义的全文件 diff。
	lf bool
}

// renderModule 渲染模块自身的全部文件（不含 router 注册）。
func renderModule(d moduleData) ([]renderedFile, error) {
	// 迁移文件名用当天的日期：cmd/migrate 按文件名排序应用，
	// 日期前缀让新模块的迁移自然排在已有迁移之后。
	migrationName := fmt.Sprintf("sql/migrations/%s-%s-table.sql", time.Now().Format("2006-01-02"), d.Name)

	backend := []struct {
		path     string
		template string
		lf       bool
	}{
		{fmt.Sprintf("internal/module/%s/model/%s.go", d.Name, d.Name), "model", false},
		{fmt.Sprintf("internal/module/%s/dto/%s_dto.go", d.Name, d.Name), "dto", false},
		{fmt.Sprintf("internal/module/%s/repository/%s_repository.go", d.Name, d.Name), "repository", false},
		{fmt.Sprintf("internal/module/%s/service/%s_service.go", d.Name, d.Name), "service", false},
		{fmt.Sprintf("internal/module/%s/controller/%s_controller.go", d.Name, d.Name), "controller", false},
		{migrationName, "migration", true},
		{fmt.Sprintf("web/src/api/%s.ts", d.Name), "api", false},
		{fmt.Sprintf("web/src/views/%s/index.vue", d.Name), "view", false},
	}

	out := make([]renderedFile, 0, len(backend))
	for _, f := range backend {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, f.template, d); err != nil {
			return nil, fmt.Errorf("渲染模板 %s 失败: %w", f.template, err)
		}
		out = append(out, renderedFile{relPath: f.path, content: buf.String(), lf: f.lf})
	}
	return out, nil
}

// anchors router.go 里的插入点。用锚点注释而不是按内容匹配现有代码：
// 后者会在有人重排路由时静默失灵（插到错误位置或直接匹配不到）。
const (
	anchorImports     = "gen:imports"
	anchorPerms       = "gen:perms"
	anchorControllers = "gen:controllers"
	anchorRoutes      = "gen:routes"
)

// applyRouterPatch 把新模块的注册代码插入 router.go 的四个锚点。
//
// 返回被修改后的文件内容；若四个锚点没找齐则报错 —— 宁可失败，
// 也不要生成一个「模块存在但路由没注册」的半成品（那种情况下
// 新模块的接口全是 404，而代码看起来一切正常）。
func applyRouterPatch(d moduleData, routerPath string) (string, error) {
	raw, err := os.ReadFile(routerPath)
	if err != nil {
		return "", err
	}
	// 统一按 LF 处理，交给 go/format 排版后再恢复原行尾
	hadCRLF := bytes.Contains(raw, []byte("\r\n"))
	src := strings.ReplaceAll(string(raw), "\r\n", "\n")

	inserts := []struct {
		anchor string
		lines  []string
	}{
		{
			anchor: anchorImports,
			lines: []string{
				fmt.Sprintf("\t%sController \"go-admin/internal/module/%s/controller\"", d.Camel, d.Name),
			},
		},
		{
			anchor: anchorPerms,
			lines: []string{
				fmt.Sprintf("\tperm%sList   = \"%s:list\"", d.Pascal, d.Name),
				fmt.Sprintf("\tperm%sAdd    = \"%s:add\"", d.Pascal, d.Name),
				fmt.Sprintf("\tperm%sEdit   = \"%s:edit\"", d.Pascal, d.Name),
				fmt.Sprintf("\tperm%sDelete = \"%s:delete\"", d.Pascal, d.Name),
			},
		},
		{
			anchor: anchorControllers,
			lines: []string{
				fmt.Sprintf("\t%sCtrl := %sController.New%sController()", d.Camel, d.Camel, d.Pascal),
			},
		},
		{
			anchor: anchorRoutes,
			lines: []string{
				fmt.Sprintf("\t\t%s := authorized.Group(\"/%s\")", d.Camel, d.Name),
				"\t\t{",
				fmt.Sprintf("\t\t\tprotected(%s, http.MethodPost, \"\", perm%sAdd, %sCtrl.Create)",
					d.Camel, d.Pascal, d.Camel),
				fmt.Sprintf("\t\t\tprotected(%s, http.MethodPut, \"\", perm%sEdit, %sCtrl.Update)",
					d.Camel, d.Pascal, d.Camel),
				fmt.Sprintf("\t\t\tprotected(%s, http.MethodDelete, \"/:id\", perm%sDelete, %sCtrl.Delete)",
					d.Camel, d.Pascal, d.Camel),
				fmt.Sprintf("\t\t\tprotected(%s, http.MethodGet, \"/:id\", perm%sList, %sCtrl.FindByID)",
					d.Camel, d.Pascal, d.Camel),
				fmt.Sprintf("\t\t\tprotected(%s, http.MethodGet, \"/list\", perm%sList, %sCtrl.FindList)",
					d.Camel, d.Pascal, d.Camel),
				"\t\t}",
				"",
			},
		},
	}

	lines := strings.Split(src, "\n")
	for _, ins := range inserts {
		idx := -1
		for i, l := range lines {
			if strings.Contains(l, ins.anchor) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return "", fmt.Errorf(
				"router.go 里找不到锚点 %q —— 它被删了或改动了。\n"+
					"锚点注释是生成器唯一的插入依据（按内容匹配现有代码会在重排后静默失灵），请恢复它", ins.anchor)
		}
		// 幂等：锚点后紧邻的几行里已经有该模块的注册就不再插
		if containsModuleRegistration(lines, ins.anchor, d) {
			continue
		}
		merged := make([]string, 0, len(lines)+len(ins.lines))
		merged = append(merged, lines[:idx+1]...)
		merged = append(merged, ins.lines...)
		merged = append(merged, lines[idx+1:]...)
		lines = merged
	}

	patched := strings.Join(lines, "\n")

	// 用 go/format 而不是 exec gofmt：不依赖 PATH，失败信息也更直接
	formatted, err := format.Source([]byte(patched))
	if err != nil {
		return "", fmt.Errorf("插入注册代码后 router.go 语法不合法（这通常是生成器的 bug）: %w", err)
	}

	if hadCRLF {
		return strings.ReplaceAll(string(formatted), "\n", "\r\n"), nil
	}
	return string(formatted), nil
}

// containsModuleRegistration 判断某个锚点附近是否已经注册过该模块，
// 让「对同一模块重复执行」成为无副作用操作。
func containsModuleRegistration(lines []string, anchor string, d moduleData) bool {
	var probe string
	switch anchor {
	case anchorImports:
		probe = fmt.Sprintf("module/%s/controller", d.Name)
	case anchorPerms:
		probe = fmt.Sprintf("\"%s:list\"", d.Name)
	case anchorControllers:
		probe = fmt.Sprintf("New%sController()", d.Pascal)
	case anchorRoutes:
		probe = fmt.Sprintf("authorized.Group(\"/%s\")", d.Name)
	}

	// 只在锚点之后的 30 行内找：搜整个文件会把**别处**的同名片段
	// （比如另一个模块恰好也叫这个前缀）误判成「已经注册过了」
	for i, l := range lines {
		if !strings.Contains(l, anchor) {
			continue
		}
		end := i + 30
		if end > len(lines) {
			end = len(lines)
		}
		return strings.Contains(strings.Join(lines[i:end], "\n"), probe)
	}
	return false
}

// writeFiles 写出全部文件。任一目标已存在即整体失败 ——
// 覆盖已有业务代码的代价远大于让用户删掉目录重来。
func writeFiles(files []renderedFile, dryRun bool) error {
	existing := make([]string, 0)
	for _, f := range files {
		if _, err := os.Stat(f.relPath); err == nil {
			existing = append(existing, f.relPath)
		}
	}
	if len(existing) > 0 {
		return fmt.Errorf("以下文件已存在，拒绝覆盖：\n  %s\n"+
			"确认要重新生成请先删除它们（生成器不会动已有的业务代码）",
			strings.Join(existing, "\n  "))
	}

	if dryRun {
		for _, f := range files {
			fmt.Printf("\n===== %s =====\n%s", f.relPath, f.content)
		}
		return nil
	}

	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f.relPath), 0o755); err != nil {
			return err
		}
		content := f.content
		if !f.lf {
			// 与其他 .go / .vue 文件保持一致用 CRLF（索引里是 LF，git 会归一化）
			content = strings.ReplaceAll(content, "\n", "\r\n")
		}
		if err := os.WriteFile(f.relPath, []byte(content), 0o644); err != nil {
			return err
		}
		fmt.Printf("  生成 %s\n", f.relPath)
	}
	return nil
}

// selfCheck 生成后立刻验证：编译 + 再生成 swagger 文档。
//
// 编译是脚手架的核心价值所在：**生成器最容易出的错是生成一份编译不过的
// 骨架**，而那要等用户自己 go build 才发现。在这里跑一次，失败就直接报出来。
//
// swagger 必须再生成：router/router.go 新增了 5 条路由，而 CI 有双向门禁
// （router/swagger_routes_test.go）——「路由有、文档无」会直接红。
// 自动跑掉这一步，用户就不会在推送后被 CI 拦下来补文档。
func selfCheck() error {
	cmd := exec.Command("go", "build", "./...")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("生成后 `go build ./...` 失败（生成器或模板有问题）:\n%s", out)
	}

	swag, err := exec.LookPath("swag")
	if err != nil {
		fmt.Println("  ⚠️ 未找到 swag，swagger 文档未再生成。")
		fmt.Println("     路由已注册但文档没有，CI 的 swagger 覆盖率测试会红 ——")
		fmt.Println("     推送前请手动执行: go install github.com/swaggo/swag/cmd/swag@latest && make swagger")
		return nil
	}
	abs, err := filepath.Abs(".")
	if err != nil {
		return err
	}
	cmd = exec.Command(swag, "init", "-g", "cmd/server/main.go", "-o", "docs")
	cmd.Dir = abs
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("swag init 失败:\n%s", out)
	}
	fmt.Println("  swagger 文档已再生成（docs/）")
	return nil
}

// nextSteps 打印生成器**没有**自动完成的部分。
//
// 建表迁移已自动生成（幂等的 CREATE TABLE IF NOT EXISTS）；
// 刻意不自动生成的只有菜单 SQL：菜单涉及权限码与层级摆放（挂在哪一级目录下），
// 那是产品决策，不是机械动作。给一段可直接改的模板比猜一个位置更负责。
func nextSteps(d moduleData) {
	fmt.Printf(`
还需要手工完成的三件事：

1) 改字段（如果 name/status/sort 不够用）
   三处**一起改**，漏一处就是运行期不一致：
     · internal/module/%s/model/%s.go          —— Go 侧字段
     · sql/migrations/*-%s-table.sql           —— 建表语句
     · sql/init.sql                            —— 全新部署走它，不走迁移
   （加列、加索引这类无法幂等操作的写法，参考 2026-09-16-post-tenant.sql）

2) 跑迁移（开发库）：go run ./cmd/migrate

3) 挂菜单 + 授权（sys_menu 插入四条，permission 分别填）
     %s:list / %s:add / %s:edit / %s:delete
   （这四个权限码已由路由注册自动登记，菜单里写错不会报错但会静默失效 ——
    而写成 * 会让持有该菜单的角色拿到全接口通行证，已被代码显式拒绝）

前端路由由后端菜单动态生成，无需改 web/src/router/。
`, d.Name, d.Name, d.Name, d.Name, d.Name, d.Name, d.Name)
}
