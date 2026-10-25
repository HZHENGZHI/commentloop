SHELL := /bin/bash

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO ?= go
BIN_DIR := bin
LDFLAGS := -s -w -X main.version=$(VERSION)
BUILD_FLAGS := -trimpath -ldflags="$(LDFLAGS)"

.PHONY: all build run start stop status dev-backend dev-web test vet tidy cross clean

all: build

web/node_modules/.package-lock.json: web/package-lock.json
	cd web && npm ci

web-build: web/node_modules/.package-lock.json
	cd web && npm run build

build: web-build
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build $(BUILD_FLAGS) -o $(BIN_DIR)/commentloop .

run: build
	./$(BIN_DIR)/commentloop run

start: build
	./$(BIN_DIR)/commentloop start

stop:
	./$(BIN_DIR)/commentloop stop

status:
	./$(BIN_DIR)/commentloop status

# Two terminals: backend here, `make dev-web` in another.
DEV_FLAGS ?= --dev

dev-backend:
	$(GO) run . --no-open $(DEV_FLAGS)

dev-web:
	cd web && npm run dev

DIR ?= .
ADDR ?= 127.0.0.1:4571
OPEN ?=

# 审阅模式 + 前后端热重载：
#   - React/CSS 改动：vite build --watch 重建，浏览器自动刷新
#   - Go 改动：debugrun 重新编译并热重启后端，浏览器 WebSocket 自动重连
# 用法：make debug DIR=~/notes   或   make debug DIR=~/notes OPEN=--no-open
debug: web-build
	@set -euo pipefail; \
	(cd web && npm run build:watch) & watcher=$$!; \
	trap 'kill $$watcher 2>/dev/null || true' EXIT INT TERM; \
	$(GO) run ./cmd/debugrun -- --debug --web-dir web/dist --addr "$(ADDR)" $(OPEN) "$(DIR)"

# 只热重载 Go 后端（不启动前端 build watch）。
debug-backend: web-build
	$(GO) run ./cmd/debugrun -- --debug --web-dir web/dist --addr "$(ADDR)" $(OPEN) "$(DIR)"

test:
	$(GO) test ./...
	cd web && npm run build

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

cross: web-build
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build $(BUILD_FLAGS) -o $(BIN_DIR)/commentloop-darwin-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build $(BUILD_FLAGS) -o $(BIN_DIR)/commentloop-darwin-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build $(BUILD_FLAGS) -o $(BIN_DIR)/commentloop-linux-arm64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(BUILD_FLAGS) -o $(BIN_DIR)/commentloop-linux-amd64 .

clean:
	rm -rf $(BIN_DIR) web/dist
	mkdir -p web/dist
	touch web/dist/.gitkeep
