# forge

[![ci](https://github.com/okian/forge/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/okian/forge/actions/workflows/ci.yml)
[![codeql](https://github.com/okian/forge/actions/workflows/codeql.yml/badge.svg?branch=main)](https://github.com/okian/forge/actions/workflows/codeql.yml)
[![codecov](https://codecov.io/gh/okian/forge/branch/main/graph/badge.svg)](https://codecov.io/gh/okian/forge)
[![go reference](https://pkg.go.dev/badge/github.com/okian/forge.svg)](https://pkg.go.dev/github.com/okian/forge)
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

## Five minutes

Start with a struct you already have:

```go
package model

// Person is an ordinary struct, written without regard for forge.
type Person struct {
	ID   int
	Name string
	Age  int
}
```

Add one declaration beside it, in an ordinary file:

```go
package model

import "github.com/okian/forge"

//forge:collection sort=Name,Age index=ID
type Persons forge.Collection[Person]
```

Run the generator:

```
go run github.com/okian/forge/cmd/forge generate ./model
```

Two files appear. `model/zz_forge_persons.go` is this declaration's, and
`model/zz_forge_shared.go` holds what the whole package's declarations reach
into — one copy of it however many ask. `Persons` now has a query surface named
after the fields it was built from:

```go
held := model.NewPersons(one, two, three)

held.Len()             // 3
held.Names()           // []string
held.SortedByAge()     // []Person
held.ByID()            // map[int]Person

for _, name := range held.Seq().
	Filter(func(p model.Person) bool { return p.Age >= 18 }).
	Map(func(p model.Person) string { return p.Name }).
	Collect() {
	// one pass, nothing materialised in between
}
```

No helper package, no `func(Person) string` at every call site, and no
reflection. Commit both files — the declaration's own file calls into the shared
one, so a package holding one without the other does not build — and builds and
editors then work with nothing installed.

The declaration went in an ordinary file because its underlying type really is
`[]Person` — anything in the package may index it and range over it. Ask what
you got:

```
go run github.com/okian/forge/cmd/forge explain ./model -t Persons
```

## Spec form

Not every stack has an underlying type an author may write to. A ring buffer's
head index, a set's deduplication and a lock's exclusion are invariants a raw
write would quietly break, so a declaration holding one goes in a **spec file**:
an ordinary Go file under the `forgespec` build tag.

```go
//go:build forgespec

package model

import "github.com/okian/forge"

// Recent is the last thousand people, encoded as JSON in one pass.
//
//forge:ring cap=1000
type Recent forge.Ring[forge.Json[Person]]
```

One directive, and nothing here needs it. A layer generates because the
declaration names it; a `//forge:` comment is how an *option* reaches one. This
declaration says where the capacity is decided: written here, `NewRecent()` takes
nothing and the size is a fact about the type; left out, the constructor takes it
and the caller decides. Neither is more correct, which is why it is an option.

The build tag is what lets one name mean two things. Under the tag, `Recent` is
the marker instantiation, which is what `gopls` resolves while you are looking
at the declaration. In the ordinary build the spec file is out of the build
entirely and forge's own output declares `Recent` — the ring struct and its
methods — so the name resolves either way and the compiler checks the spec.

`forge generate` writes both halves:

- `zz_forge_recent.go`, under `//go:build !forgespec`: the type, the ring's
  methods, the JSON codec over them, and the interface assertions they earn.
- `zz_forge_stubs.go`, under `//go:build forgespec`: the same API with panicking
  bodies, so a caller compiles in the tagged build too.

Nothing calls a stub. It exists so that the two builds agree about what `Recent`
can do, and `make vet` type-checks both — a stub that has drifted is a file that
would otherwise fail in somebody else's checkout.

Which form a declaration needs is not a judgement call: `forge` refuses an
inline declaration whose stack cannot survive a raw write, and the diagnostic
says to move it. `forge list` prints the answer per layer in its **Declare**
column.

## Usage

```
forge generate ./...              # resolve declarations and write generated files
forge check ./...                 # validate declarations and verify freshness (CI gate)
forge explain ./model -t Persons  # print the resolved stack, shapes, and methods
forge list                        # registered layers, kinds, and option schemas
forge doctor                      # diagnose toolchain and editor configuration
forge version                     # version and build info
```

A package gets one generated file, `forge.gen.go`, and it is meant to be
committed, so builds and editors work with no tool installed. A package holding
a spec-form declaration gets a second, `forge_stubs.gen.go`, which stands in for
the first under the build tag the spec is written behind — exactly one of the
two is in any build.

A refusal names the declaration, says what is wrong and says what to do about
it, under a code you can look up:
[`docs/diagnostics.md`](docs/diagnostics.md) lists every one.

## Example

[`examples/people`](examples/people) is a whole arrangement in one package:
three subjects, five declarations over them, and the files `forge` wrote from
them, committed beside the source the way they are meant to be.

Its `Persons` is the declaration shown above, unchanged. The other four go
further than anything here: a bounded ring under *eight* layers, a smaller ring
behind a mutex, a closed set over a named integer, and a directory whose
elements encode in full and log with their secret masked. The package
documentation walks them in that order. It also names the one place this package
comes out other than a reader would guess, and says where it is written up — an
example is worth reading for what a tool really does. The tests beside it read
as usage.

## Layers of your own

The layers above are the ones forge ships. A layer is a plugin claiming one
marker type, and writing one takes no change to forge:

```go
package main

import (
	"github.com/okian/forge/driver"

	"example.com/mylayers/csv"
)

func main() {
	catalog := driver.Builtins()
	catalog.MustRegister(csv.New())

	driver.Main(catalog)
}
```

That binary takes the same command line as `forge`, walks the same packages and
writes the same files — and a declaration naming your marker composes with the
built-in layers. The interface is
[`plugin`](https://pkg.go.dev/github.com/okian/forge/plugin), which documents
what a layer is asked and in what order, where a method goes, and which of
forge's own machinery is deliberately not published.
[`docs/writing-a-layer.md`](docs/writing-a-layer.md) walks through a whole one.

[`x/csv`](x/csv) is that arrangement, built and held to every gate in this
repository. It is a CSV transport — `WriteCSVTo`, `ReadCSVFrom` and `CSVHeader`
over the subject's own fields — in a module of its own, importing `plugin` and
the standard library and nothing else. Its
[worked example](x/csv/ledger) is three declarations over one subject with the
files it wrote committed beside them.

It also shows the other half of the arrangement. `forge.Csv` is a marker forge
publishes with no generator behind it, so

```go
//forge:csv
type Entries forge.Csv[forge.Collection[Entry]]
```

type-checks against plain `forge` today, and `forge generate` reports it as work
that is not done yet. Registering a layer that claims the marker takes it over
from the placeholder, so nothing in the declaration changes when the layer
arrives. Every marker `forge list` calls *staged* is open to be claimed that
way.

## Documentation

| Where                                                    | What it is                                                                    |
| -------------------------------------------------------- | ----------------------------------------------------------------------------- |
| this file                                                | install, the two forms of declaration, the verbs                              |
| [`ARCHITECTURE.md`](ARCHITECTURE.md)                     | the pipeline, what each stage is answerable for, and the two facts that shaped it |
| [`docs/writing-a-layer.md`](docs/writing-a-layer.md)     | a layer start to finish, and what is deliberately not published               |
| [`docs/diagnostics.md`](docs/diagnostics.md)             | every `FRG` code, one line each, generated from the registry                  |
| [`plugin`](https://pkg.go.dev/github.com/okian/forge/plugin) | the reference for a layer: what it is asked, in what order, and what it answers with |
| [`examples/people`](examples/people)                     | five declarations over three subjects, with the generated files committed     |
| [`x/csv`](x/csv)                                         | a layer in its own module, written against `plugin` and nothing else          |
| [`CONTRIBUTING.md`](CONTRIBUTING.md)                     | the gates, the definition of done, and the commit conventions                 |

## Development

Every gate CI runs is a `make` target, so a green `make check` locally means a
green pipeline:

```
make check           # formatting, go vet, golangci-lint, tests with the coverage floor
make test            # tests only
make race            # tests under the race detector
make cover           # tests plus the coverage floor (90% of statements)
make bench           # benchmarks, each held to its budget in scripts/budget.txt
make fmt             # rewrite sources with gofumpt and gci
make lint            # golangci-lint on its own
make vuln            # govulncheck over reachable code
make tidy-check      # fail if go.mod or go.sum would change under `go mod tidy`
make build           # build ./cmd/forge into ./bin
make example         # regenerate the worked example under examples/
make layers-example  # regenerate the worked example under x/csv/
make diagnostics     # regenerate docs/diagnostics.md from the registry
make help            # list every target
```

The gates among those — `check`, `fmt`, `lint`, `vet`, `test`, `race`, `cover`,
`tidy-check`, `vuln` — cover `x/csv` as well as this module, because a layer
written against the published surface is only a promise if the same gates hold
it. The rest are about this module alone: `build` builds `forge`, `bench` runs
this module's benchmarks, and each worked example has its own regeneration
target because each is written by a different binary.

`make bench` is a gate rather than a report. Every benchmark declares what it
may spend in [`scripts/budget.txt`](scripts/budget.txt) and the run fails if one
spends more, so a change that costs an allocation per element turns up in the
review that introduced it. Allocations are what is held to a budget, because
they are a property of the code; timings are printed and never gated, since a
shared runner varies by more than most regressions do.

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
