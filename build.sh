#!/bin/bash
# SCM 编译脚本
set -e

# 设置 Go 环境（如果是 SCM 基础镜像通常已设置，这里确保一下）
export GO111MODULE=on
export GOPROXY=https://goproxy.cn,direct

# 禁用 CGO，编译纯静态二进制，避免 GLIBC 版本不兼容问题
export CGO_ENABLED=0
export GOOS=linux
export GOARCH=amd64

echo "Building agentic_trace_server (static binary, CGO disabled)..."
mkdir -p output/bin

# 编译为静态二进制 (-ldflags '-s -w' 减小体积)
go build -ldflags '-s -w -extldflags "-static"' -o output/bin/server ./cmd/server/

# 拷贝启动脚本到产物目录
cp -r bin output/
chmod +x output/bin/bootstrap.sh

echo "Build success. Output is in output/bin/"