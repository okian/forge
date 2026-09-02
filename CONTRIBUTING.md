# Contributing

Thanks for looking. `forge` is early, and the fastest way to help is to try the
declaration syntax on a real type and say where it did not fit.

## Before you start

What the tool should do is decided against a design document and a work
breakdown that are not published yet, so for anything that changes behaviour,
open an issue before writing the patch. That is not a formality: the answer is
often that the behaviour is already specified differently, and finding that out
after the work is a bad trade of your time.

For a bug, an issue with the declaration that misbehaved and the output it
produced is worth more than a patch — the declaration is the reproduction, and
it needs no design decision to be useful.

Before changing anything in `internal/`, read
[`ARCHITECTURE.md`](ARCHITECTURE.md). It says what each stage is answerable for
and what it may not know, which is the part that is expensive to rediscover — and
it names the two language-level facts the whole shape follows from. If you are
writing a layer rather than changing forge, you need none of it:
[`docs/writing-a-layer.md`](docs/writing-a-layer.md) and the `plugin` package
are the whole surface.

## The gates

Every gate CI runs is a `make` target, so a green `make check` locally means a
green pipeline:

```
make tools     # install the pinned golangci-lint and govulncheck
make check     # formatting, go vet, golangci-lint, tests with the coverage floor
```

The individual targets are worth knowing when something fails:

| Target            | What it holds you to                                       |
| ----------------- | ---------------------------------------------------------- |
| `make fmt`        | rewrites sources with gofumpt and gci                       |
| `make fmt-check`  | fails with a diff when a file is not formatted              |
| `make vet`        | `go vet ./...`                                              |
| `make lint`       | golangci-lint, including the complexity budgets below       |
| `make test`       | the suite                                                   |
| `make race`       | the suite under the race detector                           |
| `make cover`      | the suite plus the coverage floor (90% of statements)       |
| `make size`       | the dictionary and the binary, against their size budgets   |
| `make tidy-check` | fails if `go.mod` or `go.sum` would change under `go mod tidy` |
| `make vuln`       | govulncheck over reachable code                             |

`make help` lists them all.

Every gate above covers two modules: this one, and the layer under
[`x/csv`](x/csv) that is written against the published `plugin` surface. The
layer is a module of its own so that it cannot reach past that surface without
the go command noticing, and it is held to the same gates so that the surface is
a promise rather than a paragraph. `go test ./...` at the root does not reach
it — the go command reads a nested module as not part of the one above it — so
run the `make` target rather than the `go` command.

The targets that are not gates stay with this module: `make build` builds
`forge`, and `make bench` measures this module's own benchmarks.

Three more regenerate committed output rather than checking it, and each is run
when the thing it is written from changes: `make example` and
`make layers-example` for the two worked examples — one target each, because
each is written by a different binary — and `make diagnostics` for the index of
diagnostic codes. None of them is a gate; what gates them is `make check`, whose
acceptance tests regenerate through the real pipeline and compare. Committed
output that has gone stale still compiles, so nothing fails until somebody reads
it and believes it.

## Definition of done

The standard every change in this tree is held to:

- `gofumpt`-clean and `go vet ./...` clean.
- Unit tests for the new package, and the golden suite green.
- Godoc on every exported identifier.
- Deterministic behaviour: no map-iteration order reaching the output.

## Complexity budgets

`.golangci.yml` writes down what a generator tends to lose slowly. The numbers
are ratchets — each is the highest any function in the tree reaches today, so
they can only be lowered by refactoring or raised by a deliberate line in a
diff:

- cognitive complexity (`gocognit`) at 21
- cyclomatic complexity (`cyclop`) at 15
- function length (`funlen`) at 65 lines, comments excluded

Tests are exempt from all three. A table-driven test earns its length and its
branch count from the cases it covers, and shortening it deletes coverage.

If a change genuinely needs more room, raise the number in the same commit and
say why. What the budget is for is making that a decision rather than a drift.

## Commits

One task per commit where practical, in the form the history already uses:

```
feat: dispatch a command line to the work it names
```

A `feat:`, `fix:`, `build:`, `ci:`, `docs:`, `test:` or `refactor:` prefix, then
a lower-case sentence saying what the commit makes possible — not what files it
touched.

If somebody outside this repository would notice the change, add a line to
`Unreleased` in [`CHANGELOG.md`](CHANGELOG.md) in the same commit. Writing them
at release time means writing them from the log, which is how a changelog turns
into a worse copy of one. A change to what forge *writes* counts, whether or not
the API moved: it reaches every committed `forge.gen.go` in every checkout, and
the diff arrives in a review that has nothing to do with forge.

## Comments

The house style is that a comment says *why*, and says it in prose. A comment
restating the code below it is noise; a comment naming the decision the code
embodies is the part that survives a rewrite. The existing files are the
reference.

## Licence

Contributions are accepted under the [MIT Licence](LICENSE), which is the
licence this repository ships under.
