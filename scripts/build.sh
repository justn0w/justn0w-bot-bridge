#!/usr/bin/env bash
# 构建脚本：编译服务到 bin/server
set -euo pipefail

cd "$(dirname "$0")/.."

mkdir -p bin
go build -o bin/server ./cmd/server

echo "构建完成: bin/server"
