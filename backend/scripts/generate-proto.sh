#!/bin/sh
set -eu
# Run from backend in golang:1.26.7 (Debian); generated files are committed.
apt-get update -qq
apt-get install -y -qq protobuf-compiler
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
protoc -I proto --go_out=. --go_opt=module=github.com/fix-gateway/backend \
  --go-grpc_out=. --go-grpc_opt=module=github.com/fix-gateway/backend \
  proto/realtime/v1/realtime.proto
