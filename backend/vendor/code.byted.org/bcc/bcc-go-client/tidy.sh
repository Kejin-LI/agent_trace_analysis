#!/bin/bash

cur="$(pwd)"
tidy1=$cur"/internal/sidecar"
tidy2=$cur"/internal/sidecar/idl"
tidy3=$cur"/internal/core/internal/testscript"

echo "tidy bcc-client"
go mod tidy
echo "tidy bcc sidecar"
cd $tidy1 ; go mod tidy
echo "tidy bcc sidecar idl"
cd $tidy2 ; go mod tidy
echo "tidy test script"
cd $tidy3 ; go mod tidy