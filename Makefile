BIN        := model-loader
PKG        := ./cmd/model-loader
OUT        := bin/$(BIN)
INSTALLDIR := $(HOME)/.local/bin

.PHONY: build install tests

build:
	go build -o $(OUT) $(PKG)

install:
	GOBIN=$(INSTALLDIR) go install $(PKG)

tests:
	go test ./...
