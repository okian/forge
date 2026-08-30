# Development entry points. CI runs these same targets, so a green
# `make check` locally means a green pipeline.

GO ?= go
COVER_MIN ?= 90
COVER_PROFILE ?= cover.out

# Tool versions live here rather than in the workflow, so that the tool CI runs
# and the tool a contributor runs cannot drift apart. `make tools` installs
# exactly these, and the workflow asks the Makefile which version to fetch.
GOLANGCI_VERSION ?= v2.13.2
GOVULNCHECK_VERSION ?= v1.7.0

.DEFAULT_GOAL := check

.PHONY: help
help: ## List available targets.
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN { FS = ":.*?## " } { printf "  %-20s %s\n", $$1, $$2 }' | sort

.PHONY: build
build: ## Build the forge command into ./bin.
	$(GO) build -o bin/forge ./cmd/forge

# Formatting and linting both go through golangci-lint, so that the formatter
# the config names — gofumpt for the rules gofmt left to taste, gci for import
# grouping — is the one that runs, rather than whatever gofmt alone would do.
.PHONY: require-golangci
require-golangci:
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "golangci-lint is required by this target. Install the pinned version with:" >&2; \
		echo "  make tools" >&2; \
		exit 1; \
	fi

.PHONY: fmt
fmt: require-golangci ## Rewrite sources with gofumpt and gci.
	golangci-lint fmt

.PHONY: fmt-check
fmt-check: require-golangci ## Fail, with a diff, if any source file is not formatted.
	golangci-lint fmt --diff

.PHONY: vet
vet: ## Run go vet over the module.
	$(GO) vet ./...

.PHONY: lint
lint: require-golangci ## Run golangci-lint over the module.
	golangci-lint run

.PHONY: vuln
vuln: ## Report known vulnerabilities the module's code can reach.
	@if ! command -v govulncheck >/dev/null 2>&1; then \
		echo "govulncheck is required by this target. Install the pinned version with:" >&2; \
		echo "  make tools" >&2; \
		exit 1; \
	fi
	govulncheck ./...

.PHONY: golangci-version
golangci-version: ## Print the pinned golangci-lint version.
	@echo $(GOLANGCI_VERSION)

.PHONY: govulncheck-version
govulncheck-version: ## Print the pinned govulncheck version.
	@echo $(GOVULNCHECK_VERSION)

.PHONY: tools
tools: ## Install the pinned development tools into GOBIN.
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	$(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

.PHONY: test
test: ## Run the test suite.
	$(GO) test ./...

.PHONY: race
race: ## Run the test suite under the race detector.
	$(GO) test -race ./...

.PHONY: cover
cover: ## Run the test suite and enforce the coverage floor.
	COVER_MIN=$(COVER_MIN) COVER_PROFILE=$(COVER_PROFILE) ./scripts/cover.sh

.PHONY: check
check: fmt-check vet lint cover ## Run every gate CI runs.

.PHONY: tidy
tidy: ## Reconcile go.mod and go.sum with the imports in the tree.
	$(GO) mod tidy

.PHONY: tidy-check
tidy-check: ## Fail if go.mod or go.sum would change under `go mod tidy`.
	$(GO) mod tidy -diff

.PHONY: clean
clean: ## Remove build and coverage artefacts.
	rm -rf bin $(COVER_PROFILE)
