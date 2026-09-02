# Development entry points. CI runs these same targets, so a green
# `make check` locally means a green pipeline.

GO ?= go
COVER_MIN ?= 90
COVER_PROFILE ?= cover.out

# The layer that lives here and is not part of this module.
#
# It is forge's CSV transport, written against the published plugin surface and
# nothing else, and it is a module of its own for exactly that reason: a layer
# in this module could reach past the surface without anybody noticing, and one
# outside it cannot. Every gate below runs over it as well, which is what keeps
# the surface a promise rather than a paragraph.
LAYERS ?= x/csv

# How long each fuzz target is given. A budget rather than a threshold: a run
# that finds nothing is what a healthy one looks like, so what this buys is that
# a change which breaks a codec is caught by the next push rather than by
# whoever is unlucky.
FUZZ_TIME ?= 30s

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
	cd $(LAYERS) && golangci-lint fmt

.PHONY: fmt-check
fmt-check: require-golangci ## Fail, with a diff, if any source file is not formatted.
	golangci-lint fmt --diff
	cd $(LAYERS) && golangci-lint fmt --diff

.PHONY: vet
vet: spec-vet ## Run go vet over the module, in both of the builds it is written for.
	$(GO) vet ./...
	cd $(LAYERS) && $(GO) vet ./...

# The other half of a spec-form declaration.
#
# Generation writes a stub file under the marker package's build tag, mirroring
# the API the untagged build gets, so that a caller compiles either way. Nothing
# else in this Makefile builds with that tag — `go vet ./...` uses the default
# one — so a stub file that is wrong is a file no gate reads. It fails at the
# next `forge generate`, in somebody else's checkout, which is the worst place
# for it.
#
# Named packages rather than ./..., because the tag means something only where a
# declaration is written under it, and the rest of the module has no such file.
.PHONY: spec-vet
spec-vet: ## Type-check the packages that carry spec-form declarations, under the marker tag.
	$(GO) vet -tags forgespec ./examples/... ./internal/racetest/matrix
	cd $(LAYERS) && $(GO) vet -tags forgespec ./ledger

.PHONY: lint
lint: require-golangci ## Run golangci-lint over the module.
	golangci-lint run
	cd $(LAYERS) && golangci-lint run

.PHONY: vuln
vuln: ## Report known vulnerabilities the module's code can reach.
	@if ! command -v govulncheck >/dev/null 2>&1; then \
		echo "govulncheck is required by this target. Install the pinned version with:" >&2; \
		echo "  make tools" >&2; \
		exit 1; \
	fi
	govulncheck ./...
	cd $(LAYERS) && govulncheck ./...

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
	cd $(LAYERS) && $(GO) test ./...

.PHONY: race
race: ## Run the test suite under the race detector.
	$(GO) test -race ./...
	cd $(LAYERS) && $(GO) test -race ./...

.PHONY: cover
cover: ## Run the test suite and enforce the coverage floor.
	COVER_MIN=$(COVER_MIN) COVER_PROFILE=$(COVER_PROFILE) ./scripts/cover.sh
	cd $(LAYERS) && COVER_MIN=$(COVER_MIN) COVER_PROFILE=$(COVER_PROFILE) ../../scripts/cover.sh

# Not part of `check`: benchmarks take longer than a gate anybody runs before
# every commit should, and the thing they protect against arrives in a review
# rather than in a keystroke. CI runs it on its own.
.PHONY: bench
bench: ## Run the benchmarks and hold each one to its recorded budget.
	./scripts/bench.sh

# The comparisons that need somebody else's library, in a module of their own so
# that the module everybody builds keeps the dependencies it has. Nested, which
# the go command reads as not part of the module above it, so `./...` at the
# root does not reach them.
#
# Gated on the allocation figures like everything else, and run separately
# because it fetches a dependency tree that has nothing to do with building
# forge.
#
# Run for longer than the module's own benchmarks are. What is measured here is
# a single check rather than a pass over a thousand elements, so a fixed cost
# paid once divided by two hundred iterations is still a byte per operation —
# and a budget written around that would be a budget about the run length.
.PHONY: bench-against
bench-against: ## Measure the generated code against the libraries it replaces.
	cd benchmarks/validator && \
		BENCH_BUDGET=../../scripts/budget-against.txt BENCH_TIME=20000x ../../scripts/bench.sh

# Not part of `check` either, and for the opposite reason to the benchmarks: a
# fuzz run has no natural end, so what it costs is whatever it is given. The
# gate is that every target survives its budget rather than that it finds
# nothing, since finding nothing is what a passing fuzz run always looks like.
#
# The corpus a run discovers is left in the build cache rather than committed. A
# case that actually fails is written into testdata by the go command, and that
# one belongs in the repository: it is a bug with a name.
.PHONY: fuzz
fuzz: ## Fuzz the codec targets against the standard library.
	FUZZ_TIME=$(FUZZ_TIME) ./scripts/fuzz.sh

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

# The layer's own worked example, regenerated by the layer's own binary rather
# than by forge's.
#
# It has to be that binary. The declarations name a marker forge publishes and
# has no generator for, so forge's own command reports them as work it has not
# done — which is the arrangement being demonstrated, and is why the example
# lives beside the layer rather than under examples/.
.PHONY: layers-example
layers-example: ## Regenerate the worked example beside the out-of-module layer.
	cd $(LAYERS) && $(GO) run -buildvcs=false ./cmd/forge-csv generate ./ledger

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
# What does gate it is the acceptance test in internal/cli, which regenerates
# through the real pipeline and compares every file, so it catches the generator
# changing as well as the declarations. This target is for running the verb by
# hand against real committed output.
.PHONY: fresh
fresh: ## Run forge check over the worked example under examples/.
	$(GO) run -buildvcs=false ./cmd/forge check ./examples/...

# The race matrix is generated like the example and for the same reason — it is
# code that has to be compiled and run, so it is committed — but it is written
# by forge's own harness rather than by the verb, because what it holds is one
# declaration per concurrent layer and that list comes from the catalog.
#
# Regenerating is how a new concurrent layer gets a stress test: the harness
# writes the declaration, the package and the test, and `make race` runs the
# result under the detector.
.PHONY: race-matrix
race-matrix: ## Regenerate the race matrix under internal/racetest/matrix.
	$(GO) test ./internal/racetest -update

# The index of diagnostics, which is a hundred lines of table generated from the
# registry rather than maintained by hand.
#
# Regenerated here and gated by the test that writes it, so a code added without
# a line in the document is a red build rather than a number nobody can look up.
# It runs from internal/cli because registration is a side effect of linking:
# the set is only complete where the whole tree is imported, and the command
# line is what imports it.
.PHONY: diagnostics
diagnostics: ## Regenerate the diagnostics index under docs/.
	$(GO) test ./internal/cli -run TestTheDiagnosticsIndexIsTheRegistry -update

.PHONY: check
check: fmt-check vet lint cover size ## Run every gate CI runs.

# What the tool weighs, which is a gate rather than a benchmark: the dictionary
# forge embeds is data somebody may upgrade, and an upgrade that brings a
# megabyte with it is one every user of the tool pays for without being asked.
.PHONY: size
size: ## Hold the embedded dictionary and the forge binary to their size budgets.
	./scripts/size.sh

.PHONY: tidy
tidy: ## Reconcile go.mod and go.sum with the imports in the tree.
	$(GO) mod tidy
	cd $(LAYERS) && $(GO) mod tidy

.PHONY: tidy-check
tidy-check: ## Fail if go.mod or go.sum would change under `go mod tidy`.
	$(GO) mod tidy -diff
	cd $(LAYERS) && $(GO) mod tidy -diff

.PHONY: clean
clean: ## Remove build and coverage artefacts.
	rm -rf bin $(COVER_PROFILE) $(LAYERS)/$(COVER_PROFILE)
