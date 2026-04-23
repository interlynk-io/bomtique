# SPDX-FileCopyrightText: 2026 Interlynk.io
# SPDX-License-Identifier: Apache-2.0

BINARY := bomtique
PKG    := github.com/interlynk-io/bomtique
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

GO      ?= go
GOFLAGS ?=

# Tools installed via `make tools`. Pinned versions keep CI reproducible.
STATICCHECK_VERSION  := 2025.1.1
GOLANGCI_LINT_VERSION := v2.3.0

.PHONY: all build test test-race lint vet fmt fmt-check cover fuzz clean tidy tools ci help

all: build

## build: compile the bomtique binary into ./bin.
build:
	mkdir -p bin
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/bomtique

## test: run the full test suite.
test:
	$(GO) test $(GOFLAGS) -count=1 ./...

## test-race: run the full test suite with the race detector.
test-race:
	$(GO) test $(GOFLAGS) -count=1 -race ./...

## vet: run go vet.
vet:
	$(GO) vet ./...

## fmt: format all Go sources with gofmt.
fmt:
	gofmt -s -w .

## fmt-check: fail if any Go source is not gofmt-clean.
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
	  echo "The following files are not gofmt-clean:"; \
	  echo "$$unformatted"; \
	  exit 1; \
	fi

## lint: run staticcheck and golangci-lint (install with `make tools`).
lint:
	staticcheck ./...
	golangci-lint run ./...

## cover: run tests with coverage and print a summary.
cover:
	$(GO) test -count=1 -covermode=atomic -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -20

## fuzz: run all fuzz targets for 30 seconds each.
fuzz:
	@set -e; \
	for pkg in $$($(GO) list ./...); do \
	  for f in $$($(GO) test -list 'Fuzz.*' $$pkg 2>/dev/null | grep '^Fuzz' || true); do \
	    echo "fuzzing $$pkg / $$f"; \
	    $(GO) test $$pkg -run '^$$' -fuzz "^$$f$$" -fuzztime 30s; \
	  done; \
	done

## tidy: `go mod tidy`.
tidy:
	$(GO) mod tidy

## tools: install pinned dev tooling into GOBIN.
tools:
	$(GO) install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

## ci: the check set CI runs (build, vet, fmt-check, tests, race).
ci: build vet fmt-check test test-race

## clean: remove build artifacts.
clean:
	rm -rf bin dist coverage.out

## help: list targets.
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
