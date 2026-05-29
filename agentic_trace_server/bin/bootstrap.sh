#!/bin/bash
set -e

cd /opt/tiger/agentic_trace_server
echo "Starting agentic_trace_server..."
exec ./output/bin/server
