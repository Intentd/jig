# Version stamping — reads from git; CI injects the same flags.
GIT_VERSION := $(shell git describe --tags --match 'v*' --always 2>/dev/null || echo "dev")
GIT_COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
GIT_DATE    := $(shell git log -1 --format=%cI 2>/dev/null || date -u +"%Y-%m-%dT%H:%M:%SZ")

JIG_LDFLAGS := -X github.com/intentd/tools/version.Version=$(GIT_VERSION) \
               -X github.com/intentd/tools/version.Commit=$(GIT_COMMIT) \
               -X github.com/intentd/tools/version.Date=$(GIT_DATE)

BINARY_NAME ?= jig
OUTPUT_DIR ?= $(HOME)/.jig/bin
PKG        ?= ./runjig

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: prepare
prepare:
	@[ -f go.mod.txt ] && mv go.mod.txt go.mod || true
	@[ -f go.sum.txt ] && mv go.sum.txt go.sum || true
	@mkdir -p $(OUTPUT_DIR)

.PHONY: build
build: prepare tidy
	go build -ldflags "$(JIG_LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME) $(PKG)
	@echo "✅ jig built"

.PHONY: run
run: tidy
	go run $(PKG)