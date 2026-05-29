#!/bin/bash
set -e

mkdir -p output/bin
echo "Building server..."
go build -o output/bin/server ./cmd/server/main.go
echo "Build success!"
