# Makefile for github.com/codeownersnet/opencode-go-sdk
#
# Targets:
#   make pull       Fetch upstream openapi.json -> opencode-spec.json
#   make pull-local Fetch spec from a local opencode server (OPENCODE_SERVER)
#   make generate   Run oapi-codegen to produce types.gen.go + client.gen.go
#   make test       Run go test
#   make regen      pull + generate + test (the full refresh loop)
#   make check      Validate generation is up to date (no drift)
#   make fmt        Format all non-generated Go files
#   make lint       Run golangci-lint (if installed)
#   make clean       Remove generated files

SHELL := /bin/bash

# Upstream spec source. Pinned to anomalyco/opencode dev branch.
SPEC_FILE := opencode-spec.json
SPEC_NORMALIZED := opencode-spec.normalized.json

# oapi-codegen config
OAPI_CONFIG := oapi-codegen.yaml
OAPI := oapi-codegen

# Go tooling
GO := go

.PHONY: pull pull-local normalize generate test regen check fmt lint clean help

help:
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

## pull: Fetch upstream openapi.json from anomalyco/opencode dev branch
pull:
	@echo ">> Fetching upstream spec"
	@$(GO) run scripts/pull-and-normalize.go --pull
	@echo ">> Path count: $$(python3 -c 'import json;print(len(json.load(open("$(SPEC_FILE)"))["paths"]))' 2>/dev/null || echo 'unknown')"

## pull-local: Fetch spec from a running local opencode server
pull-local:
	@echo ">> Fetching spec from $${OPENCODE_SERVER:-http://127.0.0.1:4096}"
	@$(GO) run scripts/pull-and-normalize.go --pull

## normalize: Convert 3.1 constructs to 3.0-compatible JSON
normalize:
	@$(GO) run scripts/pull-and-normalize.go --normalize

## generate: Normalize spec + run oapi-codegen to produce opencode.gen.go
generate: normalize
	@echo ">> Generating opencode.gen.go (types + client)"
	@$(OAPI) -config $(OAPI_CONFIG) -generate types,client -response-type-suffix Output -o opencode.gen.go $(SPEC_NORMALIZED)
	@echo ">> Generation complete"

## test: Run go test
test:
	@$(GO) build ./...
	@$(GO) test ./...

## regen: Full refresh — pull upstream spec, regenerate, test
regen: pull generate test
	@echo ">> Regen complete. Review with: git diff --stat"

## check: Verify generated files are in sync with the spec (no drift)
check: generate
	@echo ">> Checking for drift..."
	@git diff --exit-code opencode.gen.go $(SPEC_FILE) || \
		(echo ">> ERROR: Generated files are out of sync with $(SPEC_FILE). Run 'make regen' and commit." && exit 1)
	@echo ">> Generated files are in sync."

## fmt: Format all non-generated Go files
fmt:
	@find . -name '*.go' ! -name '*.gen.go' ! -path './vendor/*' -exec gofmt -s -w {} \;
	@echo ">> Formatted non-generated files"

## lint: Run golangci-lint (if installed)
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --exclude '\.gen\.go$$'; \
	else \
		echo ">> golangci-lint not installed; skipping"; \
	fi

## clean: Remove generated files
clean:
	rm -f opencode.gen.go
	@echo ">> Removed generated files (run 'make generate' to regenerate)"
