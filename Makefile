.PHONY: build run test vet fmt tidy clean mcp

BINARY := bin/server
MCP_BINARY := bin/mcp

# 编译二进制到 bin/
build:
	go build -o $(BINARY) ./cmd/server

# 本地运行
run:
	go run ./cmd/server

# 编译订单 MCP 服务到 bin/。它不自己监听端口，
# 由 MCP 客户端（如 Claude Code）通过 stdio 拉起：
#   claude mcp add order-bot -- ./bin/mcp
mcp:
	go build -o $(MCP_BINARY) ./cmd/mcp

# 运行全部测试
test:
	go test ./...

# 静态检查
vet:
	go vet ./...

# 格式化代码
fmt:
	gofmt -l -w .

# 整理依赖
tidy:
	go mod tidy

# 清理编译产物
clean:
	rm -rf bin
