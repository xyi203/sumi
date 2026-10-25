# 变量定义，方便后续维护
BINARY_NAME=bin/server
MAIN_PATH=main.go

.PHONY: run build generate swagger help test test-integration test-integration-down

TEST_PG_PORT?=55432
TEST_REDIS_PORT?=56379
TEST_DB_DSN?=postgres://postgres:sumi@localhost:$(TEST_PG_PORT)/sumi?sslmode=disable
TEST_REDIS_URL?=redis://localhost:$(TEST_REDIS_PORT)/0

# 1. 生成 SQL 代码
# 这里直接在根目录调用 sqlc，避免嵌套 make 带来的路径混乱
generate:
	@echo "Generating SQLC code..."
	sqlc generate

swagger:
	@echo "Generating Swagger docs..."
	swag init -g swagger.go -o docs

# 2. 启动 API 服务（AUTO_MIGRATE=true 时先执行迁移）
run:
	go run $(MAIN_PATH)

# 3. 编译二进制文件
build:
	@echo "Building binary..."
	go build -o $(BINARY_NAME) $(MAIN_PATH)

test:
	go test ./...

# Integration tests need a real PostgreSQL and Redis: the logic they guard lives in
# SQL and cache invalidation. Starts throwaway containers and runs the suite, which
# migrates the database itself; the containers stay up for the next run (use
# test-integration-down to remove them).
test-integration:
	@docker start sumi-test-pg 2>/dev/null || docker run -d --name sumi-test-pg \
		-e POSTGRES_PASSWORD=sumi -e POSTGRES_DB=sumi -p $(TEST_PG_PORT):5432 postgres:16-alpine
	@docker start sumi-test-redis 2>/dev/null || docker run -d --name sumi-test-redis \
		-p $(TEST_REDIS_PORT):6379 redis:7-alpine
	@echo "Waiting for PostgreSQL..."
	@until docker exec sumi-test-pg pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done
	TEST_DB_DSN="$(TEST_DB_DSN)" TEST_REDIS_URL="$(TEST_REDIS_URL)" go test ./... -count=1

test-integration-down:
	-docker rm -f sumi-test-pg sumi-test-redis

# 帮助信息
help:
	@echo "Usage:"
	@echo "  make generate  - Run sqlc generate in /sqlc directory"
	@echo "  make swagger   - Generate Swagger docs with swaggo/swag"
	@echo "  make run       - Start the API server (AUTO_MIGRATE=true migrates first)"
	@echo "  make build     - Build the server binary"
