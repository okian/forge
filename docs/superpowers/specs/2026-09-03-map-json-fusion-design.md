# Map[S, Json[T]] — the fused JSON writer (Stage 2)

Stage 1 shipped the mapper: `Map[S, T]` generates `TFromS`, with members
settled by the ladder, by `from` tags, by one hint function, or by `ignore`.
Stage 2 composes it with the JSON append codec into the thing neither offers
alone: writing T's document straight from an S, with no T ever built.

## 1. The declaration

```go
//forge:map
//forge:json omitzero=true
type UserWire Map[User, Json[Person]]
```

Resolution already handles this shape: the bridge carries `User` beside the
stack and descends into `Json[Person]`, leaving the stack `[Map, Json]` over
subject `Person`. What changes is composition: the `bridges` rule, which today
keeps a bridge alone, admits exactly one company — a stack that is precisely
`[Map, Json]`. Anything else over or under a bridge stays FRG1009, with the
hint updated to say the one exception.

Both layers read their own directives, exactly as a multi-layer stack already
does: `//forge:map ignore=...` configures the mapping, `//forge:json
omitzero=true` the wire shape.

## 2. What is generated

Three things, in the declaration's file:

1. **The constructor**, exactly as stage 1 writes it: `PersonFromUser(src
   *User) Person`. The fusion adds to the mapper; it does not replace it.
2. **Person's own codec**, exactly as a `Json[Person]` declaration would write
   it (`AppendJSON`, `MarshalJSON`, `UnmarshalJSON`, `UnmarshalJSONBorrowed`),
   shared per package the way the codec already shares nested structs. It is
   what the fused writer is held equal to, and it is the read half — the
   fusion is write-only, and decoding lands on T as it always did.
3. **The fused writers**, package functions named from all three parts:

```go
func AppendPersonJSONFromUser(dst []byte, src *User) ([]byte, error)
func WritePersonJSONFromUser(w io.Writer, src *User) (int64, error)
```

Package functions rather than methods on `UserWire`, for stage 1's reason: a
mapping's product is free functions, and the declared type stays an empty
struct. The names follow the codec's initialism rules (`JSON`, not `Json`).

## 3. How the fusion works

The append codec already writes each member of T from an expression — today
always a walk from the value being encoded. The mapping's settle table says
what each member holds *before any T exists*:

| settled by | fused expression |
| ---------- | ---------------- |
| field match / tag pin | `src.Contact` |
| method match / tag pin | `src.NickName()` |
| hint assignment | the hint's right-hand side, respelled |
| ignore | T's zero value for that member, written as the codec writes zeros |

The strict hint grammar is what makes this sound: a hint is a pure expression,
and a pure expression inlines into the writer exactly where the member's value
would have been read. Nothing else of the codec changes — names, ordering,
omitzero, escaping and the wire runtime are the codec's own, driven by T's
json tags as they always are.

### The seam between the two layers

The mapping layer owns the settle table; the codec owns the wire. The seam is
one internal entry point on the jsoncodec package:

```go
// Fused writes T's document from expressions instead of from a held T.
// reads maps each of T's member names to the Go expression whose value the
// member carries — expressions already spelled against the parameter the
// signature binds. The exact shape of the parameter argument (name and type
// spelling travel together) is settled in the implementation plan.
func Fused(ctx *plugin.Context, reads map[string]string, param ...) (plugin.Unit, error)
```

The mapping layer builds `reads` from its plan — the same bindings the
constructor is written from — and asks the codec for the writers. The surface
ledger (`internal/layers/surface_test.go`) gains the entry: mapping reaches
jsoncodec for "the fused writer the codec builds from a mapping's bindings",
the same way builder reaches failures.

A member of T that is itself a struct is written by the codec's existing
per-type appenders — the fusion rewrites only the top level's reads, because
below the top level there is a real value (whatever `src.X` returned) to walk.

## 4. What is refused

Everything stage 1 refuses, everything the codec refuses for T, and:

| case | why |
| ---- | --- |
| `Map[S, X[T]]` for any X but Json | FRG1009 still: a bridge composes with Json and nothing else |
| `Json` written over `Map` (`Json[Map[...]]`) | the bridge is outermost by construction; anything over it is FRG1009 |
| a hint expression the codec cannot inline | already impossible: the grammar admits only pure expressions |

No new diagnostic codes are expected: the fusion is a composition of two
layers whose refusals both already exist.

## 5. What gates it

1. **Byte equality**: for every fixture pair and every fuzzed value,
   `AppendPersonJSONFromUser(nil, src)` equals `PersonFromUser(src)` followed
   by `AppendJSON(nil)` — bytes and error verdict both. This runs in the same
   compile-and-run temp module as stage 1's reference suite, and rides the
   codec's existing differential fuzzing where the fixtures overlap.
2. **No T allocated**: the fused append performs zero allocations beyond
   buffer growth, held by a benchmark row in the budget gate beside the
   codec's own writers.
3. **The shadow suite**: a row for the fused stack `["Map", "Json"]`, so a
   subject named after any identifier the fused writers bind still compiles.
4. **The whole gate**: `make check` (coverage floor included), the race
   matrix untouched, `make example` a no-op, `make fresh` clean.

## 6. Out of scope, on purpose

- Reading JSON into an S (the reverse fusion): decode lands on T, and
  `SFromT` is a separate `Map[T, S]` declaration if wanted.
- `Map[S, Collection[...]]` or any container under the bridge: a container is
  a value that exists, and the mapper's whole point is a value that does not.
- Streaming multiple sources; `WritePersonJSONFromUser` writes one document.
