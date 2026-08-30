# forge

[![ci](https://github.com/okian/forge/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/okian/forge/actions/workflows/ci.yml)
[![codeql](https://github.com/okian/forge/actions/workflows/codeql.yml/badge.svg?branch=main)](https://github.com/okian/forge/actions/workflows/codeql.yml)
[![go reference](https://pkg.go.dev/badge/github.com/okian/forge.svg)](https://pkg.go.dev/github.com/okian/forge)
[![go report card](https://goreportcard.com/badge/github.com/okian/forge)](https://goreportcard.com/report/github.com/okian/forge)
[![go version](https://img.shields.io/github/go-mod/go-version/okian/forge?logo=go&label=go)](go.mod)
[![licence](https://img.shields.io/badge/licence-MIT-blue.svg)](LICENSE)

Type-driven code generation for Go.

A type declaration is the spec:

```go
type Persons Collection[Ring[Json[Person]]]
```

`forge` reads that declaration, resolves it into a layer stack — `Collection` →
`Ring` → `Json` → `Person` — and emits a concrete type with the combined API:
ring-buffer storage, query methods specialised to `Person`'s fields, a
reflection-free JSON codec, and the standard library interfaces those layers
imply.

Because the spec is Go, the compiler and `gopls` check it. Renaming `Person`
propagates; deleting it is a build failure rather than a stale comment.

Generated code imports the standard library and the subject's own dependencies,
and nothing else. There is no runtime package to version-skew against.

## Status

Early development. The public surface is unstable.

## Requirements

Go 1.27 or newer, for `encoding/json/v2` and generic methods.

## Install

```
go install github.com/okian/forge/cmd/forge@latest
```

Or run it straight out of the module, which is the form to prefer in a
`go:generate` directive so contributors need nothing installed:

```
go run github.com/okian/forge/cmd/forge generate ./...
```

## Usage

```
forge generate ./...              # resolve declarations and write generated files
forge check ./...                 # validate declarations and verify freshness (CI gate)
forge explain ./model -t Persons  # print the resolved stack, shapes, and methods
forge list                        # registered layers, kinds, and option schemas
forge doctor                      # diagnose toolchain and editor configuration
forge version                     # version and build info
```

Generated files are named `zz_forge_*.go` and are meant to be committed, so
builds and editors work with no tool installed.

## Development

Every gate CI runs is a `make` target, so a green `make check` locally means a
green pipeline:

```
make check       # formatting, go vet, golangci-lint, tests with the coverage floor
make test        # tests only
make race        # tests under the race detector
make cover       # tests plus the coverage floor (90% of statements)
make fmt         # rewrite sources with gofumpt and gci
make lint        # golangci-lint on its own
make vuln        # govulncheck over reachable code
make tidy-check  # fail if go.mod or go.sum would change under `go mod tidy`
make build       # build ./cmd/forge into ./bin
make help        # list every target
```

The linting and vulnerability targets need their tools on `PATH`, and fail
rather than skipping if one is missing. The Makefile pins both versions and CI
installs exactly those, so the tool that gates a pull request and the tool on
your machine cannot drift:

```
make tools     # golangci-lint and govulncheck, at the pinned versions
```

Linting is more than `go vet`. `.golangci.yml` runs gofumpt and gci as the
formatter, revive's full rule set, and written-down budgets for cognitive
complexity, cyclomatic complexity and function length — because a generator is a
pile of switches over a type graph, which is exactly the shape that rots
quietly. [`CONTRIBUTING.md`](CONTRIBUTING.md) explains the budgets and what to
do when a change needs more room than one allows.

From inside a checkout, run the command straight out of the working tree:

```
go run ./cmd/forge generate ./...
```

## Contributing

[`CONTRIBUTING.md`](CONTRIBUTING.md) has the gates, the definition of done, and
the commit conventions.

For a bug, the declaration that misbehaved is the reproduction — send that and
the file `forge` wrote for it. What the tool should have done with it is decided
against a design document that is not published yet, so an issue is a better
first move than a patch for anything that changes behaviour.

## Security

Report a vulnerability privately through GitHub's
[advisory form](https://github.com/okian/forge/security/advisories/new) rather
than in a public issue. [`SECURITY.md`](SECURITY.md) says what makes a report
useful and what is in scope.

## Licence

[MIT](LICENSE). Generated code carries no licence obligation from `forge`: it
imports the standard library and the subject's own dependencies, and nothing
else.
