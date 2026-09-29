.PHONY: build run clean lint test coverage check check-backend check-frontend check-deploy check-migrations check-secrets check-race check-vuln swagger migrate migrate-status deps help swagger-baseline

APP_NAME := go-admin
BUILD_DIR := ./dist

build:
	go build -o $(BUILD_DIR)/$(APP_NAME) ./cmd/server

run:
	go run ./cmd/server

clean:
	rm -rf $(BUILD_DIR)

# lint 与 CI 的 lint 任务保持一致（go vet + golangci-lint）。
#
# 这里显式检查 golangci-lint 是否存在：否则「make check 等价于 CI」这句约定
# 会在本地悄悄失效 —— 开发者在本地看到绿色，推上去才被 CI 拦住。
# 注意必须用 v2：v1 的发布二进制用 go1.24 构建，面对 go.mod 要求的 go 1.25
# 会在加载阶段直接失败（原因详见 .golangci.yml 顶部注释）。
lint:
	go vet ./...
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "缺少 golangci-lint，无法完成与 CI 等价的静态检查。"; \
		echo "安装：go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2"; \
		exit 1; }
	golangci-lint run --timeout=5m

# 快速跑测试，不带覆盖率门槛
test:
	go test ./...

# 跑完整测试并检查「包均覆盖率」门槛。阈值与口径写在脚本头部注释里。
#
# 用 `bash scripts/...` 而不是 `./scripts/...`：仓库里的可执行位在 Windows
# 检出后不一定保留，显式调 bash 更稳。
#
# check-backend 用它而不是 test —— 跑一遍测试就同时拿到门槛检查，
# 不必把测试跑两遍。
coverage:
	bash scripts/check-coverage.sh

# check 与 CI（.github/workflows/ci.yml）保持一致：提交前跑一遍即可等价于 CI
#
# 这里只放**本地能跑**的项。三条不进 check，各有理由：
#   · check-race   —— 需要 cgo/gcc，本项目的 Windows 开发机没有 MinGW
#   · check-vuln   —— 需要联网查漏洞库，放进提交前检查会让它变慢且不稳定
#   · （CI 全都会跑）
check: check-backend check-frontend check-deploy check-migrations check-secrets

check-backend: lint coverage
	@echo "后端检查通过"

# 迁移在**真实 MySQL** 上执行：init.sql → cmd/migrate 应用全部迁移 →
# 再跑一次必须是 no-op → 把全部 .sql 原样重放 3 轮断言无 ERROR（幂等）。
#
# 此前 CI 对迁移只做 `go build ./cmd/migrate`：证明它能编译，仅此而已。
# 而「迁移写错」（漏判 information_schema、重复建索引、引用还不存在的列）
# 编译期全都看不出来，要等上线执行时才炸。
#
# 需要本机 MySQL 与 mysql 客户端；CI 上用 mysql:5.7 service。
# 连接参数与临时库名见 scripts/check-migrations.sh 头部注释。
check-migrations:
	bash scripts/check-migrations.sh
	@echo "迁移检查通过"

# 密钥泄漏扫描（**全历史**）。
#
# 针对的是「以后有人提交一个真实密钥进来」——Dockerfile 烘焙默认密钥那条
# 已经修了（H12），但那只修了当前状态，没有持续防线。
#
# 扫描**全历史**是重点：密钥一旦提交过就删不掉了（历史会一直带着它），
# 所以检查必须在合并前拦住，而不是等发现了再去改写历史。
#
# ⚠️ 刻意**不用** `gitleaks dir .`：实测它扫出 235 处「泄漏」，全部来自
# web/node_modules 与 .mimocode 里的第三方测试夹具 —— 507 MB 的噪音。
# 那些目录本来就不入库（.gitignore），扫它们没有任何意义。
# `git` 模式天然只扫被跟踪的内容，正好是我们要的范围。
# 提交前的拦截由 pre-commit 钩子负责（见 .pre-commit-config.yaml）。
check-secrets:
	@command -v gitleaks >/dev/null 2>&1 || { \
		echo "缺少 gitleaks，无法做密钥泄漏扫描。"; \
		echo "安装：go install github.com/zricethezav/gitleaks/v8@latest"; \
		echo "（注意是 zricethezav 路径：模块已迁移，用 gitleaks/gitleaks 会因 go.mod 路径不符而失败）"; \
		exit 1; }
	gitleaks git . --no-banner --redact
	@echo "密钥泄漏扫描通过"

# 数据竞争检测。**刻意不并入 check**。
#
# 它需要 cgo（gcc）：本项目开发机是 Windows 且没有 MinGW，`go env CGO_ENABLED`
# 为 0，本地跑不了 —— 这条实际只在 CI（ubuntu-latest 自带 gcc）上执行。
# 也因为它会让测试耗时涨 2~5 倍，不适合放进提交前的快速检查。
check-race:
	@test "$$(go env CGO_ENABLED)" = "1" || { \
		echo "-race 需要 cgo 与 gcc，当前 CGO_ENABLED=$$(go env CGO_ENABLED)。"; \
		echo "Windows 上需安装 MinGW-w64 并把 gcc 放进 PATH；否则只能在 CI 上跑。"; \
		exit 1; }
	go test -race ./...

# 依赖漏洞扫描（标准库 + 直接依赖 + 调用链分析）。
#
# 需要联网查漏洞库（vuln.go.dev），所以不进 check。
check-vuln:
	@command -v govulncheck >/dev/null 2>&1 || { \
		echo "缺少 govulncheck。安装：go install golang.org/x/vuln/cmd/govulncheck@latest"; \
		exit 1; }
	govulncheck ./...
	@echo "依赖漏洞扫描通过"

# 部署文件静态校验（docker-compose / Dockerfile / nginx / 应用配置）。
#
# 此前它只是个「可以手动跑一下」的脚本，既不在 CI 里也不在 make check 里 ——
# 一个没人跑、也不被任何门禁调用的校验，等于不存在。
# 里面几条断言针对的都是「不报错、不影响功能、只有抓响应头或构建镜像才看得出来」
# 的失效，例如 nginx 的 add_header 不继承上层导致的 X-Frame-Options 丢失。
#
# 需要 PyYAML：pip install pyyaml
check-deploy:
	@command -v python3 >/dev/null 2>&1 || { \
		echo "缺少 python3，无法校验部署文件（CI 在 ubuntu-latest 上执行这一步）。"; \
		exit 1; }
	@python3 -c "import yaml" 2>/dev/null || { \
		echo "缺少 PyYAML，无法校验部署文件。"; \
		echo "安装：pip install pyyaml（CI 上由 workflow 安装）"; \
		exit 1; }
	python3 deploy/validate.py
	@echo "部署文件校验通过"

check-frontend:
	cd web && npm run typecheck
	cd web && npm run lint
	cd web && npm run format:check
	cd web && npm run test
	cd web && npm run build
	@echo "前端检查通过"

# 执行未应用的数据库迁移（读取 config/config.yaml 里的 database.* 配置）
migrate:
	go run ./cmd/migrate

# 只查看迁移状态，不执行任何 SQL
migrate-status:
	go run ./cmd/migrate -status

swagger:
	swag init -g cmd/server/main.go -o docs

# 固化 swagger 基线（仅供**有意的**破坏性变更使用，见 scripts/check_breaking_changes.py）。
# 流程：破坏性变更提交带 [BREAKING] → 合并后跑本目标 → 基线随变更同提交入库。
swagger-baseline:
	cp docs/swagger.json docs/swagger-baseline.json
	@echo "swagger 基线已更新（docs/swagger-baseline.json），请与破坏性变更同一提交入库"

deps:
	go mod tidy

help:
	@echo "Available commands:"
	@echo "  make build            - Build the application"
	@echo "  make run              - Run the application"
	@echo "  make clean            - Clean build artifacts"
	@echo "  make lint             - Run go vet + golangci-lint"
	@echo "  make test             - Run tests（不含覆盖率门槛，用于快速迭代）"
	@echo "  make coverage         - Run tests + 包均覆盖率门槛检查"
	@echo "  make check            - 跑一遍 CI 的本地可跑部分（后端 + 前端 + 部署 + 迁移 + 密钥扫描）"
	@echo "  make check-deploy     - 只跑部署文件静态校验（需 PyYAML）"
	@echo "  make check-migrations - 在真实 MySQL 上跑迁移（幂等 + 工具可用），需本机 MySQL"
	@echo "  make check-secrets    - 密钥泄漏扫描（全历史），需 gitleaks"
	@echo "  make check-race       - 数据竞争检测，需 cgo/gcc（本机 Windows 无 MinGW，实际只在 CI 跑）"
	@echo "  make check-vuln       - 依赖漏洞扫描，需 govulncheck + 联网"
	@echo "  make migrate          - Apply pending DB migrations"
	@echo "  make migrate-status   - Show DB migration status"
	@echo "  make swagger          - Generate swagger docs"
	@echo "  make deps             - Tidy go modules"
