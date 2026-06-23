BINARY := babynas
VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build run clean dist

## 本机编译
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

## 本机运行（示例目录，按需替换）
run: build
	./$(BINARY) -audio ./sample/audio -video ./sample/video -db ./babynas.db

## 交叉编译到常见 NAS 平台（纯 Go，无需 CGO）
dist:
	mkdir -p dist
	# 群晖/威联通 x86_64
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 .
	# ARM64 NAS（部分群晖、树莓派、ARM 威联通）
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 .
	# ARMv7（老 ARM NAS）
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-armv7 .
	# macOS（本机测试）
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-darwin-arm64 .
	@echo "==> 产物已生成到 dist/"
	@ls -lh dist/

clean:
	rm -f $(BINARY)
	rm -rf dist
