BIN        := model-loader
PKG        := ./cmd/model-loader
OUT        := bin/$(BIN)
INSTALLDIR := $(HOME)/.local/bin

# Version override via VERSION env or git describe fallback.
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS    := -X github.com/quantmind-br/model-loader/internal/cli.Version=$(VERSION) \
              -X github.com/quantmind-br/model-loader/internal/cli.BuildDate=$(BUILD_DATE)

.PHONY: build install tests

build:
	go build -ldflags "$(LDFLAGS)" -o $(OUT) $(PKG)

install:
	GOBIN=$(INSTALLDIR) go install -ldflags "$(LDFLAGS)" $(PKG)

tests:
	go test ./...
