# Atrio — project commands.

GO ?= go
BIN_DIR := bin
BINARY := $(BIN_DIR)/atrio
PKG := github.com/cburgosro9303/atrio
VERSION ?= dev
LDFLAGS := -X '$(PKG)/cli.Version=$(VERSION)'

# Platforms covered by the cross-compile check and by the architecture test.
# This list is the single definition of the matrix: CI's cross-compile job calls
# `build-all` rather than restating the platforms (.github/workflows/ci.yml), and
# a test keeps internal/archtest in step with it. Locally the target is the early
# warning against a dependency that pulls in CGo; in CI it is the gate.
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

# CGO_ENABLED=0 is scoped to the build targets, where the cross-compile
# guarantee actually lives. It must NOT be global: outside darwin the toolchain
# rejects `-race` without cgo ("-race requires cgo"), so a global setting would
# break the mandatory race tests on Linux and Windows runners.
BUILD_ENV := CGO_ENABLED=0

# Recipes that mutate sources must not race against those that read them.
.NOTPARALLEL:

.PHONY: all build build-all test vet lint fmt fmt-check tidy verify clean

## all: the pre-commit sweep
all: verify

## verify: everything that must pass before a commit. Checks formatting rather
## than applying it, so it can fail — run `make fmt` to fix.
verify: fmt-check vet lint test

## build: compile the single atrio binary
build:
	$(BUILD_ENV) $(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/atrio

## build-all: cross-compile every supported platform; catches accidental CGo
build-all:
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch $(BUILD_ENV) $(GO) build -ldflags "$(LDFLAGS)" \
			-o $(BIN_DIR)/atrio-$$os-$$arch$$ext ./cmd/atrio || exit 1; \
	done

## test: full suite with the mandatory race detector
##
## -count=1 disables the test cache deliberately. The architecture test derives
## its facts from a `go list` subprocess, and Go's cache key cannot see that
## output — a cached run would report a green pass over a real violation.
test:
	$(GO) test -race -count=1 ./...

## vet: the standard Go correctness pass
vet:
	$(GO) vet ./...

## lint: golangci-lint, pinned as a module tool dependency
lint:
	$(GO) tool golangci-lint run ./...

## fmt: apply the configured formatters in place
fmt:
	$(GO) tool golangci-lint fmt ./...

## fmt-check: report formatting drift without touching files (for verify and CI)
fmt-check:
	$(GO) tool golangci-lint fmt --diff ./...

## tidy: reconcile go.mod and go.sum
tidy:
	$(GO) mod tidy

## clean: remove build output
clean:
	rm -rf $(BIN_DIR)
