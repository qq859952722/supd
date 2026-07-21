# supd Makefile
# REQ-C-015: 版本注入、前端构建嵌入

BINARY     := supd
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +%Y%m%d)
LDFLAGS    := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

.PHONY: all build dev test vet lint clean web

## 生产构建：先编译前端，再 go build 嵌入
all: web build

## 仅编译 Go（跳过前端，用于开发/CI）
build:
	go build -tags '!dev' $(LDFLAGS) -o $(BINARY) ./cmd/supd/

## 开发模式：不嵌入前端（使用 dev tag）
dev:
	go run -tags dev ./cmd/supd/ run

## 测试
test:
	go test ./...

## 静态检查
vet:
	go vet ./...

## Lint（需要 golangci-lint）
lint:
	golangci-lint run ./...

## 前端构建
web:
	cd web && pnpm install && pnpm build

## 清理
clean:
	rm -f $(BINARY)
	rm -rf web/dist
