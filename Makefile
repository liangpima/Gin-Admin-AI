.PHONY: build run clean lint test check check-backend check-frontend swagger migrate migrate-status

APP_NAME := go-admin
BUILD_DIR := ./dist

build:
	go build -o $(BUILD_DIR)/$(APP_NAME) ./cmd/server

run:
	go run ./cmd/server

clean:
	rm -rf $(BUILD_DIR)

lint:
	go vet ./...

test:
	go test ./...

# check 与 CI（.github/workflows/ci.yml）保持一致：提交前跑一遍即可等价于 CI
check: check-backend check-frontend

check-backend: lint test
	@echo "后端检查通过"

check-frontend:
	cd web && npx vue-tsc --noEmit
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
	@echo "  make lint           - Run go vet"
	@echo "  make test           - Run tests"
	@echo "  make check          - 跑一遍 CI 的全部检查（后端 + 前端），提交前建议执行"
	@echo "  make migrate        - Apply pending DB migrations"
	@echo "  make migrate-status - Show DB migration status"
	@echo "  make swagger        - Generate swagger docs"
	@echo "  make deps           - Tidy go modules"
