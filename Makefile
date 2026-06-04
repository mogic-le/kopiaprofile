# Makefile for kopiaprofile
#
# This file is the entry point for local development. It mirrors the
# commands run in CI (.github/workflows/test-os.yml) so that running
# `make test` locally is equivalent to `act`/running the workflow on a
# single matrix entry.

BINARY       := kopiaprofile
PACKAGE      := ./...
GO           ?= go
LDFLAGS      := -s -w

# Default: show available targets.
.PHONY: help
help:
	@echo "kopiaprofile — Makefile"
	@echo
	@echo "  make build       — compile the binary into ./$(BINARY)"
	@echo "  make test        — run unit tests (short)"
	@echo "  make test-ci     — run tests with race detector + coverage"
	@echo "  make lint        — run golangci-lint"
	@echo "  make fmt         — gofmt + goimports"
	@echo "  make tidy        — go mod tidy"
	@echo "  make clean       — remove build artefacts"
	@echo "  make integration — run the rustfs-backed integration test"
	@echo "  make snapshot    — build a goreleaser snapshot (no publish)"
	@echo "  make version     — print the version string"

.PHONY: build
build:
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BINARY) .

.PHONY: test
test:
	$(GO) test -short $(PACKAGE)

.PHONY: test-ci
test-ci:
	@# Go 1.25+ requires the `covdata` tool for atomic coverage.
	@# Skip coverage on toolchains that don't have it.
	@if $(GO) env GOTOOLDIR >/dev/null 2>&1 && [ -x "$$($(GO) env GOTOOLDIR)/covdata" ]; then \
		echo "==> running tests with race + coverage"; \
		$(GO) test -race -coverprofile=coverage.out -covermode=atomic $(PACKAGE); \
		$(GO) tool cover -func=coverage.out | tail -1; \
	else \
		echo "==> running tests with race (no covdata in toolchain; skipping coverage)"; \
		$(GO) test -race $(PACKAGE); \
	fi

.PHONY: lint
lint:
	golangci-lint run --timeout=5m

.PHONY: fmt
fmt:
	gofmt -s -w .
	goimports -w .

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: clean
clean:
	rm -f $(BINARY) coverage.out
	rm -rf dist build

.PHONY: integration
integration:
	bash testdata/integration-test.sh

.PHONY: snapshot
snapshot:
	goreleaser release --clean --snapshot --skip=publish,validate

.PHONY: version
version:
	@$(GO) run . version
