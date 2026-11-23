# Version stamping — reads from git; CI injects the same flags.
GIT_VERSION := $(shell git describe --tags --match 'v*' --always 2>/dev/null || echo "dev")
GIT_COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
GIT_DATE    := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

JIG_LDFLAGS := -X github.com/intentd/tools/version.Version=$(GIT_VERSION) \
               -X github.com/intentd/tools/version.Commit=$(GIT_COMMIT) \
               -X github.com/intentd/tools/version.Date=$(GIT_DATE)

BINARY_NAME ?= jig
OUTPUT_DIR ?= $(HOME)/.jig/bin

.PHONY: build test vet fmt examples validate-examples

build:
	gofmt -w .
	go build -ldflags "$(JIG_LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME) ./cmd/$(BINARY_NAME)
	@echo "✅ Jig built successfully"

test:
	go test ./...

# The examples suite (examples/jig.yaml imports the others), run by the jig
# just built: a kind cluster, and a second one for app-image, deleted after.
# REPORT=<file> writes the report there too (CI sends it to the job summary).
examples: build
	$(OUTPUT_DIR)/$(BINARY_NAME) run examples $(if $(REPORT),--report="$(REPORT)")
	mv examples/library/go.mod.txt examples/library/go.mod || true
	GOWORK=off go -C examples/library run ./runjig ../../examples/library/testdata/jig.yaml
	GOWORK=off go -C examples/library test -tags=integration -v ./integration/
	mv examples/library/go.mod examples/library/go.mod.txt || true

# Every example checked offline, with what a run would do.
validate-examples: build
	@for d in examples examples/*/; do $(OUTPUT_DIR)/$(BINARY_NAME) validate $$d || exit 1; done

vet:
	go vet ./...

fmt:
	gofmt -w .
