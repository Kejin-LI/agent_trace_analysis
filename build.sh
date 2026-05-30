#!/bin/bash
# SCM 编译脚本
set -e

# 设置 Go 环境（如果是 SCM 基础镜像通常已设置，这里确保一下）
export GO111MODULE=on
export GOPROXY=https://goproxy.cn,direct

echo "Building agentic_trace_server..."
mkdir -p output/bin

# 编译为二进制
go build -o output/bin/server ./cmd/server/

# 拷贝启动脚本到产物目录
cp -r bin output/

echo "Build success. Output is in output/bin/"