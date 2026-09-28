// gen 是新业务模块的脚手架生成器。
//
// # 为什么需要它
//
// 接一个新模块要手写 8 处：model / dto / repository / service / controller
// 五个后端文件、router.go 里的四处注册、前端 api 与列表页，外加建表 SQL 与菜单。
// 手抄现成模块是唯一办法，而手抄必然漂移 —— 漏 swagger 注解、漏 tenantID、
// 漏权限码登记，这三样恰好都有机械检查能兜，**但没有东西在源头上帮你生成对**。
//
// 对一套「为业务快速开发做准备」的框架来说，这是最大的瓶颈：
// 每接一个模块都要重复一遍易错的机械劳动。
//
// # 用法
//
//	go run ./cmd/gen -name order_item -title 订单项
//	go run ./cmd/gen -name order_item -title 订单项 -dry-run
//
// 生成后会自动跑 `go build ./...` 与 `swag init` 自检 —— 脚手架最容易出的错是
// 生成一份编译不过的骨架；而 router 新增了路由却不再生成文档，会被 CI 的
// swagger 覆盖率测试拦下。这两步都由生成器自动完成。
//
// # 刻意不做的事
//
//   - **不生成菜单 SQL**：菜单涉及权限码与层级摆放（挂在哪一级目录下），
//     那是产品决策不是机械动作。生成器改为打印一段可直接改的模板。
//   - **不覆盖已有文件**：任一目标存在就整体失败。覆盖业务代码的代价
//     远大于让用户删掉目录重来。
package main

import (
	"flag"
	"fmt"
	"os"
)

const usage = `用法:
  go run ./cmd/gen -name <模块名> -title <中文名> [-dry-run]

参数:
  -name     模块名：小写字母开头，只含小写字母/数字/下划线（如 order_item）
            它会成为包名、文件名、表名与路由前缀（/api/v1/order_item）
  -title    中文名（如 订单项），用于注释与前端文案
  -dry-run  只打印将生成的内容，不写任何文件

示例:
  go run ./cmd/gen -name order_item -title 订单项
`

func main() {
	name := flag.String("name", "", "模块名（小写字母/数字/下划线）")
	title := flag.String("title", "", "中文名（用于注释与前端文案）")
	dryRun := flag.Bool("dry-run", false, "只打印将生成的内容，不写文件")

	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	if *name == "" || *title == "" {
		flag.Usage()
		os.Exit(2)
	}

	if err := run(*name, *title, *dryRun); err != nil {
		fmt.Fprintf(os.Stderr, "\n生成失败: %v\n", err)
		os.Exit(1)
	}
}

func run(name, title string, dryRun bool) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf(
			"模块名 %q 不合法。要求小写字母开头，之后只含小写字母/数字/下划线（1~31 字符）。\n"+
				"它会被直接拼进文件名、Go 标识符与路由路径，放宽约束会生成编译不过的代码", name)
	}

	d := newModuleData(name, title)

	const routerPath = "router/router.go"
	if _, err := os.Stat(routerPath); err != nil {
		return fmt.Errorf("找不到 %s（请在仓库根目录执行）: %w", routerPath, err)
	}

	files, err := renderModule(d)
	if err != nil {
		return err
	}

	patched, err := applyRouterPatch(d, routerPath)
	if err != nil {
		return err
	}

	if dryRun {
		if err := writeFiles(files, true); err != nil {
			return err
		}
		fmt.Printf("\n===== %s（整文件，含本次插入）=====\n%s", routerPath, patched)
		fmt.Println("\n[dry-run] 未写入任何文件")
		return nil
	}

	fmt.Printf("生成模块 %s（%s）:\n", d.Name, d.Title)
	if err := writeFiles(files, false); err != nil {
		return err
	}

	// router.go 的写入放在模块文件之后：若前面已经失败，就不会留下
	// 「router 注册了但模块文件缺失」的坏状态（那会直接编译不过）。
	if err := os.WriteFile(routerPath, []byte(patched), 0o644); err != nil {
		return err
	}
	fmt.Printf("  更新 %s（4 处锚点）\n", routerPath)

	fmt.Println("\n自检：go build ./... + swag init")
	if err := selfCheck(); err != nil {
		return err
	}
	fmt.Println("自检通过。")

	nextSteps(d)
	return nil
}
