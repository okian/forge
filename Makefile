# Development entry points. CI runs these same targets, so a green
# `make check` locally means a green pipeline.

GO ?= go
COVER_MIN ?= 90
COVER_PROFILE ?= cover.out
GOLANGCI_VERSION ?= v2.13.2

.DEFAULT_GOAL := check

.PHONY: help
help: ## List available targets.
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN { FS = ":.*?## " } { printf "  %-18s %s\n", $$1, $$2 }' | sort

.PHONY: build
build: ## Build the forge command into ./bin.
	$(GO) build -o bin/forge ./cmd/forge

.PHONY: fmt
fmt: ## Rewrite sources with gofmt.
	gofmt -l -w .

.PHONY: fmt-check
fmt-check: ## Fail if any source file is not gofmt-clean.
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed on:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: vet
vet: ## Run go vet over the module.
	$(GO) vet ./...

.PHONY: lint
lint: ## Run golangci-lint over the module.
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "golangci-lint is required by this target. Install the pinned version with:" >&2; \
		echo "  $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)" >&2; \
		exit 1; \
	fi
	golangci-lint run

.PHONY: golangci-version
golangci-version: ## Print the pinned golangci-lint version.
	@echo $(GOLANGCI_VERSION)

.PHONY: test
test: ## Run the test suite.
	$(GO) test ./...

.PHONY: cover
cover: ## Run the test suite and enforce the coverage floor.
	COVER_MIN=$(COVER_MIN) COVER_PROFILE=$(COVER_PROFILE) ./scripts/cover.sh

.PHONY: check
check: fmt-check vet lint cover ## Run every gate CI runs.

.PHONY: tidy
tidy: ## Reconcile go.mod and go.sum with the imports in the tree.
	$(GO) mod tidy

.PHONY: clean
clean: ## Remove build and coverage artefacts.
	rm -rf bin $(COVER_PROFILE)
