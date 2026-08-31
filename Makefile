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

# Not part of `check`: benchmarks take longer than a gate anybody runs before
# every commit should, and the thing they protect against arrives in a review
# rather than in a keystroke. CI runs it on its own.
.PHONY: bench
bench: ## Run the benchmarks and hold each one to its recorded budget.
	./scripts/bench.sh

# Regeneration is a target rather than a step of the build, which is the whole
# arrangement forge is for: the output is committed, and it is remade when a
# declaration changes rather than when somebody types make.
#
# The header must record the tool rather than the commit. `go build` stamps a
# pseudo-version derived from the checkout, and a target that used one would
# write a version into a committed file that changed on every commit — a file
# in every review, saying nothing in any of them. `go run` records no VCS stamp
# and so writes (devel); -buildvcs=false says that this is required rather than
# incidental, and keeps the target honest if it is ever built rather than run.
#
# What still moves is the fingerprint, which is fed by the Go version as well:
# regenerating on a newer toolchain rewrites the `inputs` line and nothing else.
# That is why the acceptance test compares bodies rather than bytes.
.PHONY: example
example: ## Regenerate the worked example under examples/.
	$(GO) run -buildvcs=false ./cmd/forge generate ./examples/...

# The verb compares each file's recorded fingerprint against what its
# declaration would produce now, so it composes nothing and writes nothing.
#
# Not a gate, and not in `check`. Two reasons, both about this repository rather
# than about the verb. The fingerprint records the forge version, and `go run`
# stamps every development build (devel) — so a change to forge's own generator
# leaves every fingerprint where it was and this would say the example is fine.
# And the fingerprint records the Go version, while go.mod names a minimum: two
# people on different patch releases cannot both see this pass, and the only
# byte that differs between them is the fingerprint itself.
#
# What does gate it is the acceptance test in cmd/forge, which regenerates
# through the real pipeline and compares every file, so it catches the generator
# changing as well as the declarations. This target is for running the verb by
# hand against real committed output.
.PHONY: fresh
fresh: ## Run forge check over the worked example under examples/.
	$(GO) run -buildvcs=false ./cmd/forge check ./examples/...

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
