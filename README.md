# forge

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
make check     # gofmt, go vet, golangci-lint, tests with the coverage floor
make test      # tests only
make cover     # tests plus the coverage floor (90% of statements)
make fmt       # rewrite sources with gofmt
make build     # build ./cmd/forge into ./bin
make help      # list every target
```

`make lint` needs `golangci-lint` on `PATH`, and fails rather than skipping if
it is missing. CI installs the same pinned version the Makefile names, so the
two cannot drift:

```
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(make -s golangci-version)
```

From inside a checkout, run the command straight out of the working tree:

```
go run ./cmd/forge generate ./...
```
