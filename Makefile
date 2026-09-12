.PHONY: build run test vet fmt tidy clean

BINARY := bin/server

# 编译二进制到 bin/
build:
	go build -o $(BINARY) ./cmd/server

# 本地运行
run:
	go run ./cmd/server

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
