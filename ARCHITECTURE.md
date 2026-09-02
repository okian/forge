# Architecture

How `forge` gets from a type declaration to a file on disk, and where each
decision lives.

This is a map for somebody about to change the code. It says what each stage is
answerable for and what it may not know, because the boundaries are the part
that is expensive to rediscover. What a layer author needs is a narrower
document — [`plugin`](https://pkg.go.dev/github.com/okian/forge/plugin) — and
what a user needs is the [README](README.md).

## The pipeline

One pass, front to back, with the expensive part happening once.

```
patterns
   │
   ▼
load ─────────── one go/packages session, bodies stripped
   │
   ▼
discover ─────── the AST scan for T[A] declarations and //forge: directives
   │
   ▼
resolve ──────── the instantiation walk: a stack of markers and a subject
   │
   ▼
model ────────── the subject: fields, tags, classification, reachable closure
   │
   ▼
options ──────── each directive checked against the layer's own schema
   │
   ▼
compose ──────── the rules, the shapes, the storage nobody wrote
   │
   ▼
generate ─────── each layer asked for its unit
   │
   ▼
merge ────────── units into files: imports, assertions, collisions, helpers
   │
   ▼
emit ─────────── format, header, fingerprint, bytes
   │
   ▼
write ────────── never rewrites a file whose bytes have not changed
```

Every verb walks the same path down to `model`, and that is deliberate:
`generate`, `check` and `explain` differ only in what they do with a resolved
declaration, and all three are wrong in the same way if they disagree about how
one is found. The shared path is `internal/cli/pipeline.go`, whose stages are
interfaces so a verb can be tested against declarations that were never on
disk.

The load is the dominant cost, so it happens exactly once for every layer and
every declaration in a run.

## The stages

| Stage      | Package             | Answerable for                                                                 |
| ---------- | ------------------- | ------------------------------------------------------------------------------ |
| load       | `internal/load`     | one `go/packages` session; body-stripped parsing; the two build configurations  |
| discover   | `internal/discover` | which declarations might be requests, and the raw `//forge:` directives on them |
| resolve    | `internal/resolve`  | following an instantiation inward to a stack of markers and one subject         |
| model      | `internal/subject`  | building `model.Struct` from a `types.Type`: classify, tags, closure, cycles    |
| tags       | `internal/tags`     | the single interpretation of a struct tag every layer reads                     |
| options    | `internal/options`  | checking what was written against what each layer declared it accepts           |
| compose    | `internal/compose`  | the composition rules, the shape walk, inserting storage nobody wrote           |
| layers     | `internal/layers`   | forge's own catalog, and the registry a run is given                            |
| templates  | `internal/templates`| rewriting a layer's compiling generic bodies into one subject's terms           |
| generate   | `internal/generate` | asking each layer for a unit, interface synthesis, collision policy             |
| merge      | `internal/merge`    | one package's units into one package's files                                    |
| emit       | `internal/emit`     | formatting, the generated header, the input fingerprint, determinism            |
| write      | `internal/cli`      | the verbs, the flags, and putting bytes beside the package they belong to       |
| diag       | `internal/diag`     | the `FRG` registry, positions, hints, and how a report is rendered              |

Five packages are not stages of that walk and are worth knowing anyway:

| Package              | What it is                                                                |
| -------------------- | ------------------------------------------------------------------------- |
| `internal/explain`   | the explain verb's report: the stack, the shapes per step, the methods     |
| `internal/scalars`   | what a subject earns from its own tags, rather than from a layer it names  |
| `internal/view`      | the type a decorator hands a caller inside a scope                        |
| `internal/shared/seq`| the lazy sequence a query surface hands back, emitted into the package     |
| `internal/goldentest`| the golden harness and the in-memory typecheck gate the matrices run on   |

Three more are not code that runs but the vocabulary everything else programs
against: `internal/model` is the types the stages pass between them,
`internal/shape` is what a layer exposes to the layer above it, and
`internal/layer` is what a layer *is*. Changing any of the three is a
design-level event rather than an implementation one.

## The two facts that shape everything

**`go/types` discards the instantiation.** For `type Persons Collection[Person]`,
`Named.Underlying()` is `[]Person` — `Collection` is not in the type graph at
all. So resolution cannot be a type-graph walk; it goes through the AST, finds
the `IndexExpr`, and asks `types.Info` what each argument resolved to. This is
why `internal/discover` exists as a stage of its own, and why a declaration
written as an alias is skipped rather than followed.

**A marker cannot be a generic alias to its own parameter.** Go rejects
`type Json[T any] = T`, so an element marker is a zero-sized phantom struct
rather than a slice — which means the underlying type of a declaration holding
one is `struct{}` rather than `[]Person`. That is why an element layer is never
*transparent*, and why a stack containing one has to be written in a spec file.

## Two forms of declaration

A declaration whose underlying type is the author's own goes in an ordinary
file:

```go
//forge:collection sort=Name
type Persons forge.Collection[Person]
```

Its underlying type really is `[]Person`, so anything in the package may index
it and range over it. That works only for a stack whose layers all uphold their
invariants over the raw type — a slice does, a ring's head index does not. What
decides is the optional `Transparent` interface, read through
`layer.TransparentLayer`: a layer that says nothing is taken to be opaque, which
is the safe direction, and an element layer is opaque whatever it says, because
its marker is a phantom struct rather than a slice. A stack that fails is
refused with a diagnostic saying to move the declaration under the build tag.

The other form is a **spec file**, under the marker build tag:

```go
//go:build forgespec

//forge:ring cap=1024
type Recent forge.Collection[forge.Ring[forge.Json[Person]]]
```

Two builds then exist, and both have to compile. The default build gets
`zz_forge_recent.go`, which declares the type and its methods. The tagged build
gets the spec file plus `zz_forge_stubs.go`, which mirrors the same API with
panicking bodies — so a call site compiles either way, and `gopls` resolves the
declaration while an author is looking at it.

## Composition

A stack is a list of markers, outermost first, over one subject. Each marker is
claimed by one layer, and a layer reports a **kind** that says where it may sit:

| Kind        | What it is                                    | Where                                  |
| ----------- | --------------------------------------------- | -------------------------------------- |
| `Storage`   | the representation: a slice, a ring, a tree   | at most one; owns the declared type    |
| `Refining`  | a surface over a representation               | above the storage                      |
| `Element`   | about one value: a codec, a check, a copy     | innermost, around the subject          |
| `Decorator` | wraps a representation                        | anywhere above the storage             |
| `Transport` | terminates the stack: bytes out, bytes in     | outermost, at most one                 |

Nothing says which layers may sit on which. Compatibility is expressed in
**capabilities** — `Sized`, `Ordered`, `Indexed`, `Keyed`, `Structured`,
`Encodable`, `Comparable`, `Streamable`, `Bounded`, `Concurrent` — and a layer
says what it requires of what is beneath it and what it adds above. That is
what keeps the compatibility matrix from growing with the square of the
catalog: a layer added today composes with every layer added tomorrow, and
neither one mentions the other.

A `Shape` carries the capabilities *and* the method surface, because a decorator
cannot wrap what it cannot enumerate. A decorator may also **withdraw** — a
lock takes iteration away rather than handing out a sequence somebody could
hold across the lock — and what is withdrawn is invisible to everything above.

Storage nobody wrote is filled in. `Collection[Person]` is
`Collection[Slice[Person]]`, which is what makes an inline declaration's
underlying type a real slice. A stack of nothing but element layers gets the
same. A decorator with nothing beneath it does not: it wraps a representation
rather than sitting over one, and inventing one would answer a real mistake
with silence.

## Where a method goes

Two receivers, and the difference is the whole of what a layer's kind means.

A method on the **declared type** goes in the unit's own declarations. There is
one declared type per declaration, so one file, and nothing to reconcile.

A method on the **subject** does not. Two declarations over one subject each ask
their element layers for the same thing, so it goes in `Unit.Provides` under a
key naming what it is about — and forge writes each key once, into the file the
package shares. A layer that gets this wrong is reported rather than obeyed: a
subject method written into two files is a package that does not build, and it
is better caught by a diagnostic than by the compiler.

## Method bodies are compiling Go

A storage layer's bodies are not strings and not `text/template`. They are
ordinary generic Go, in a `tmpl` subpackage compiled by the ordinary build,
which `internal/templates` rewrites into one subject's terms — `T` becomes
`Person`, the template's type name becomes the declaration's.

The reason is that forty method bodies held in strings are forty bodies nothing
type-checks. Held as Go, the compiler checks them on every build and a test can
call them directly. What it costs is a rewriter, and the rewriter is why the
template machinery is deliberately **not** part of the published plugin
surface: publishing it would freeze the shape of every template in the tree.

## Diagnostics are the product

A generator that fails with a stack trace has told the author nothing they can
act on. So every refusal is a `Diagnostic`: a registered `FRG` code, the
position of the *declaration* rather than of the generated file, a message
saying what is wrong, and a hint saying what to do about it.

The ranges place a failure from its number alone:

| Range     | About                                    |
| --------- | ---------------------------------------- |
| `FRG1xxx` | composition — the shape of the stack     |
| `FRG2xxx` | the subject and the type model           |
| `FRG3xxx` | directives and their options             |
| `FRG4xxx` | emission, synthesis and name collisions  |
| `FRG5xxx` | input, output and the toolchain          |
| `FRG6xxx`+| a layer, whoever wrote it                |

[`docs/diagnostics.md`](docs/diagnostics.md) lists every one, and a test holds
the list to the registry so it cannot drift.

## Determinism

Generated files are committed, so a run that produced different bytes from the
same input would put a diff in every review. Nothing in the model is keyed by a
map: options and tags are ordered slices with linear lookups, so determinism is
structural rather than a documented promise. Where a map is unavoidable, what
comes out of it is sorted before it reaches the output.

What moves on its own is the header. It records the Go version, and the Go
version is one of the fingerprint's inputs, so regenerating on a newer toolchain
rewrites two of its lines. That is why the acceptance tests compare bodies rather
than bytes, and why the freshness verb is not a gate here: two people on
different patch releases could not both see it pass.

The fingerprint is fed by the Go version rather than merely recording it because
a later gofmt really can print the same declarations as different bytes. What
the header buys is that a reader who finds a mismatch can tell which of the
three moved.

## Adding a layer

A layer is a plugin claiming one marker, and writing one takes no change to
forge. `plugin` is the surface;
[`driver`](https://pkg.go.dev/github.com/okian/forge/driver) is the dozen lines
a binary's `main` holds; [`x/csv`](x/csv) is a worked example of both, in a
module of its own so that it cannot reach past the surface without the go
command noticing.

Read [`plugin`'s package documentation](https://pkg.go.dev/github.com/okian/forge/plugin)
first. It says what a layer is asked and in what order, where a method goes,
which codes to take, and which of forge's own machinery is deliberately not
published. [`docs/writing-a-layer.md`](docs/writing-a-layer.md) walks one
through from the marker to the binary.
