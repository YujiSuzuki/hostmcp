.PHONY: build build-all build-host clean test install install-host

# Versioning
# バージョン管理
# Get the version from the latest git tag
# 最新のgitタグからバージョンを取得
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags="-X 'github.com/YujiSuzuki/hostmcp/internal/cli.Version=${VERSION}' -X 'github.com/YujiSuzuki/hostmcp/internal/client.clientVersion=${VERSION}'"

# Build for current platform
# 現在のプラットフォーム向けにビルド
build:
	@echo "Building hostmcp version ${VERSION}..."
	CGO_ENABLED=0 go build ${LDFLAGS} -o hostmcp .

# Build for all platforms
# 全プラットフォーム向けにビルド（クロスコンパイル）
build-all:
	@echo "Building hostmcp version ${VERSION} for all platforms..."
	mkdir -p dist
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ${LDFLAGS} -o dist/hostmcp_windows_amd64.exe .
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ${LDFLAGS} -o dist/hostmcp_darwin_arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build ${LDFLAGS} -o dist/hostmcp_darwin_amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -o dist/hostmcp_linux_amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ${LDFLAGS} -o dist/hostmcp_linux_arm64 .

# Cross-build for the host OS
# ホストOS向けクロスビルド
#
# Read host OS/arch from .sandbox/.host-os (written at container startup).
# Located by walking up from this Makefile's own directory rather than a
# fixed number of ".." levels: hostmcp is also usable as a standalone tool
# (github.com/YujiSuzuki/hostmcp) with no ai-sandbox workspace above it at
# all, and when it is nested inside one, the nesting depth is not fixed by
# anything in this repo — it depends entirely on that workspace's layout.
# A hardcoded relative path is only ever correct for one specific depth.
# $(realpath ..) is also resolved against make's invocation CWD, not this
# Makefile's directory, so a hardcoded path would additionally break under
# `make -C` or a different invocation directory; walking from
# $(MAKEFILE_LIST) avoids that too.
# .sandbox/.host-os からホストOS/アーキテクチャを読み取る（コンテナ起動時に
# 書き出される）。固定の".."階層数を決め打ちするのではなく、このMakefile
# 自身のディレクトリから上位に向かって探索する: hostmcpはai-sandbox
# ワークスペースを介さない単体ツール（github.com/YujiSuzuki/hostmcp）としても
# 使われ、その場合は上位のどこにも.sandboxが存在しない。組み込まれる場合も、
# ネスト深さはこのリポジトリ側では何も保証されておらず、組み込み先の
# ワークスペースのレイアウトに完全に依存する。固定の相対パスは特定の1つの
# 深さでしか正しく動作しない。また $(realpath ..) は make の呼び出し時の
# カレントディレクトリを基準に解決されるため、`make -C` や別ディレクトリ
# からの呼び出しでも同様に壊れる — $(MAKEFILE_LIST) から辿ればこれも回避できる。
MAKEFILE_DIR := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
HOST_OS_FILE := $(shell d="$(MAKEFILE_DIR)"; while [ "$$d" != "/" ]; do \
	[ -f "$$d/.sandbox/.host-os" ] && { echo "$$d/.sandbox/.host-os"; break; }; \
	d=$$(dirname "$$d"); \
	done)
ifeq ($(origin HOST_OS),command line)
  # User forced HOST_OS explicitly (see the $(error ...) message below) —
  # skip file detection entirely and trust their value.
  # ユーザーがHOST_OSを明示指定した場合（下記の$(error ...)メッセージ参照）—
  # ファイル探索はせず、指定値をそのまま信頼する。
  HOST_ARCH ?= $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')
else ifdef HOST_OS_FILE
  HOST_OS ?= $(shell sed -n '1p' $(HOST_OS_FILE))
  HOST_ARCH ?= $(shell sed -n '2p' $(HOST_OS_FILE))
else ifneq ($(wildcard /.dockerenv),)
  # Inside the AI Sandbox container (same /.dockerenv check
  # install-hostmcp.sh uses for its own container guard) but
  # .sandbox/.host-os is missing. `uname` here would report the container's
  # own OS (e.g. linux), not the actual host (e.g. darwin) that
  # `make install-host` later copies the binary to. Fail loudly instead of
  # guessing.
  # AI Sandboxコンテナ内（install-hostmcp.sh自身のコンテナ判定と同じ
  # /.dockerenv チェック）だが .sandbox/.host-os が見つからない場合。
  # ここでunameを使うとコンテナ自身のOS（例: linux）が返り、後で
  # `make install-host` がバイナリをコピーする先の実際のホスト（例: darwin）
  # とは異なってしまう。推測に頼らず、ここでは明確に失敗させる。
  $(error .sandbox/.host-os not found while running inside the AI Sandbox container. Cross-building for the host OS requires this file (written by init-host-env.sh at container startup) -- re-run container startup, or check whether .sandbox/.host-os was deleted/moved. To bypass, set HOST_OS/HOST_ARCH explicitly, e.g.: make build-host HOST_OS=darwin HOST_ARCH=arm64)
else
  # Not inside the AI Sandbox container (e.g. hostmcp built standalone,
  # directly on the host with no container layer) — uname here correctly
  # reflects the host, since there's no container/host OS mismatch to guard
  # against.
  # AI Sandboxコンテナ内ではない場合（例: hostmcpをコンテナを介さず
  # ホスト上で単体ビルドしている場合）— この場合はコンテナ/ホスト間の
  # OS不一致を気にする必要がないため、unameがそのままホストの情報として正しい。
  HOST_OS ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
  HOST_ARCH ?= $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')
endif
build-host:
	@echo "Building hostmcp version ${VERSION} for ${HOST_OS}/${HOST_ARCH}..."
	mkdir -p dist
	CGO_ENABLED=0 GOOS=${HOST_OS} GOARCH=${HOST_ARCH} go build ${LDFLAGS} -o dist/hostmcp_${HOST_OS}_${HOST_ARCH} .
	@echo "✓ Built: dist/hostmcp_${HOST_OS}_${HOST_ARCH}"

# Detect where hostmcp is already installed, so install-host overwrites that
# same binary instead of silently landing in an unrelated location. Mirrors
# install-hostmcp.sh's _resolve_hostmcp_bin(): PATH lookup, then the two
# known default install dirs (~/go/bin, ~/.local/bin), then `go env GOPATH`
# for a customized GOPATH. Empty if none of those locations has a hostmcp
# binary (e.g. a brand-new host) — DEST then falls back to dist/ below.
# hostmcpの既存インストール先を検出し、install-hostが無関係な場所ではなく
# 同じバイナリを上書きするようにする。install-hostmcp.sh の _resolve_hostmcp_bin()
# と同じ探索順: PATH上のhostmcp → 既知の2つのデフォルトインストール先
# （~/go/bin, ~/.local/bin）→ カスタムGOPATH向けに `go env GOPATH`。
# これらの場所のいずれにもhostmcpバイナリが存在しない場合（新規ホストなど）は
# 空になり、DESTは下記の dist/ にフォールバックする。
DETECTED_HOSTMCP_BIN := $(shell sh -c ' \
	if p=$$(command -v hostmcp 2>/dev/null) && [ -n "$$p" ]; then echo "$$p"; \
	elif [ -x "$$HOME/go/bin/hostmcp" ]; then echo "$$HOME/go/bin/hostmcp"; \
	elif [ -x "$$HOME/.local/bin/hostmcp" ]; then echo "$$HOME/.local/bin/hostmcp"; \
	elif command -v go >/dev/null 2>&1 && [ -x "$$(go env GOPATH)/bin/hostmcp" ]; then echo "$$(go env GOPATH)/bin/hostmcp"; \
	fi')
ifneq ($(strip $(DETECTED_HOSTMCP_BIN)),)
  # Strip the trailing slash that $(dir ...) leaves, so "${DEST}/hostmcp"
  # below doesn't end up with a double slash.
  # $(dir ...) が付ける末尾スラッシュを取り除く。付けたままだと下記の
  # "${DEST}/hostmcp" が二重スラッシュになってしまうため。
  DEST ?= $(patsubst %/,%,$(dir $(DETECTED_HOSTMCP_BIN)))
else
  DEST ?= dist
endif

# Install cross-built binary to host OS (run on host OS)
# クロスビルドしたバイナリをホストOSにインストール（ホストOS上で実行）
# Usage: make install-host [DEST=/usr/local/bin]
# DEST defaults to hostmcp's existing install location if one is detected
# above, otherwise dist/.
# DESTは、上記で既存インストール先が検出された場合はそこがデフォルトになり、
# 検出できなければ dist/ になる。
install-host:
	@set -e; \
	BINARY="dist/hostmcp_${HOST_OS}_${HOST_ARCH}"; \
	if [ ! -f "$$BINARY" ]; then \
		echo "Error: $$BINARY not found. Run 'make build-host' first."; \
		exit 1; \
	fi; \
	mkdir -p ${DEST}; \
	cp $$BINARY ${DEST}/hostmcp; \
	chmod +x ${DEST}/hostmcp; \
	echo "✓ Installed to ${DEST}/hostmcp"; \
	ls -la ${DEST}/hostmcp


# Clean build artifacts
# ビルド成果物をクリーンアップ
clean:
	rm -f hostmcp
	rm -rf dist/

# Run tests
# テストを実行
test:
	go test -v ./...

# Run tests with coverage
# カバレッジレポート付きでテストを実行
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run tests for a specific package
# 特定パッケージのテストを実行
test-security:
	go test -v ./internal/security

test-config:
	go test -v ./internal/config

test-docker:
	go test -v ./internal/docker

test-mcp:
	go test -v ./internal/mcp

# Install locally (DevContainer: /home/node/.local/bin, otherwise: $GOPATH/bin)
# ローカルにインストール（DevContainerの場合は/home/node/.local/binに永続化、それ以外は$GOPATH/binに配置）
install:
ifdef DEVCONTAINER
	@mkdir -p /home/node/.local/bin
	@CGO_ENABLED=0 go build ${LDFLAGS} -o /home/node/.local/bin/hostmcp .
	@echo "✓ Installed to /home/node/.local/bin/hostmcp"
	@echo "  (persisted in DevContainer volume)"
else
	CGO_ENABLED=0 go install ${LDFLAGS} .
	@echo "✓ Installed to $$GOPATH/bin/hostmcp"
endif



# Run the server (development)
# サーバーを起動（開発モード）
# Requires a config file. If not yet created, run: make init
# 設定ファイルが必要です。未作成の場合は先に: make init
run:
	go run . serve --workspace .

# Generate config file from built-in template
# 組み込みテンプレートから設定ファイルを生成
init:
	go run . init --workspace .

# Format code
# コードをフォーマット
fmt:
	go fmt ./...

# Lint code
# コードをリント
lint:
	golangci-lint run
