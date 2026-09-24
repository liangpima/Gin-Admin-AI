.PHONY: build run clean lint test check check-backend check-frontend swagger migrate migrate-status

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

test:
	go test ./...

# check 与 CI（.github/workflows/ci.yml）保持一致：提交前跑一遍即可等价于 CI
check: check-backend check-frontend

check-backend: lint test
	@echo "后端检查通过"

check-frontend:
	cd web && npm run typecheck
	cd web && npm run lint
	cd web && npm run test
	cd web && npx vite build
	@echo "前端检查通过"

# 执行未应用的数据库迁移（读取 config/config.yaml 里的 database.* 配置）
migrate:
	go run ./cmd/migrate

# 只查看迁移状态，不执行任何 SQL
migrate-status:
	go run ./cmd/migrate -status

swagger:
	swag init -g cmd/server/main.go -o docs

deps:
	go mod tidy

help:
	@echo "Available commands:"
	@echo "  make build          - Build the application"
	@echo "  make run            - Run the application"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make lint           - Run go vet + golangci-lint"
	@echo "  make test           - Run tests"
	@echo "  make check          - 跑一遍 CI 的全部检查（后端 + 前端），提交前建议执行"
	@echo "  make migrate        - Apply pending DB migrations"
	@echo "  make migrate-status - Show DB migration status"
	@echo "  make swagger        - Generate swagger docs"
	@echo "  make deps           - Tidy go modules"
