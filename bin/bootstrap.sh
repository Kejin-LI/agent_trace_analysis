#!/bin/bash
# TCE 服务启动脚本
set -e

# 进入产物目录
cd /opt/tiger/agentic_trace_server

echo "Starting agentic_trace_server..."

# 启动 Go 二进制
# 如果需要指定配置文件，可以在这里加上传参
exec ./output/bin/server