BINARY  := otman
PKG     := ./cmd/otman
DIST    := dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/talvor/otman/internal/cli.Version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

export CGO_ENABLED := 0

.DEFAULT_GOAL := help

.PHONY: help build run test test-update cover vet fmt fmt-check lint tidy check release install clean

help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the otman binary into dist/
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) $(PKG)

run: build ## Build and run otman; pass arguments with ARGS="config show"
	./$(DIST)/$(BINARY) $(ARGS)

test: ## Run all tests
	go test ./...

test-update: ## Regenerate golden files
	go test ./internal/cli -update

cover: ## Run tests with a coverage report
	@mkdir -p $(DIST)
	go test -coverprofile=$(DIST)/coverage.out ./...
	go tool cover -func=$(DIST)/coverage.out

vet: ## Run go vet
	go vet ./...

fmt: ## Format all Go code
	gofmt -w .

fmt-check: ## Fail if any Go code is unformatted
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

lint: fmt-check vet ## Check formatting and run go vet

tidy: ## Tidy go.mod and go.sum
	go mod tidy

check: lint test ## Run everything CI runs

release: ## Build static binaries for linux and darwin on amd64 and arm64
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "building $(DIST)/$(BINARY)-$$os-$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o $(DIST)/$(BINARY)-$$os-$$arch $(PKG) || exit 1; \
	done

install: ## Install otman into GOBIN
	go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

clean: ## Remove build output
	rm -rf $(DIST)
	go clean
