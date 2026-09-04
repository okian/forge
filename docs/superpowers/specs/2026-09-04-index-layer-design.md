# Index: keyed storage with lookup maps over declared fields

Status: design, decisions settled in conversation 2026-09-04. Ships the
catalog's staged `Index` storage layer, multi-dimensional and removal-capable.

## 1. What it is for

A service that holds a working set and answers point queries writes the same
scaffolding by hand every time: a slice for iteration, a `map[int]*Person`
kept beside it, and the four places they have to agree — add, look up, remove,
reset — kept honest by nothing but attention. The declaration names the key
and forge writes the pair:

```go
//forge:index key=ID index=Name,Email
type Directory forge.Index[Person]
```

What comes out is a struct holding an insertion-ordered slice of entries
beside one map per declared dimension, with the primary lookup answering a
**stable pointer** — `ByID(k int) (*Person, bool)` — and each secondary
walking its value lazily: `ByName(v string) iter.Seq[Person]`.

## 2. The representation

```go
type directoryEntry struct{ elem Person; at int }
type Directory struct {
    order  []*directoryEntry          // the walk; swap-remove keeps holes out
    byID   map[int]*directoryEntry    // primary: key → entry
    byName map[string][]int           // secondary: value → primary keys
    byEmail map[string][]int
}
```

Four choices carry the design:

- **Entries are allocated once and never move.** The pointer a lookup answers
  stays good while neighbours come and go, which is what `map[key]*T` was
  asked to buy. The entry carries its slot in the walk order (`at`) so that
  removal swaps rather than searches: O(1) plus the buckets the element was
  filed in.
- **Secondaries hold keys, not elements.** `map[value][]key` resolves through
  the primary map — two hops per yielded element — so removal repairs only
  the removed element's own buckets and a replaced element's bucket moves are
  local. This is also why secondaries need the key unique (FRG3041): a key
  reaching several elements would walk each once per filing.
- **Every walk is over the slice.** Nothing generated ranges a map, so codecs
  and walks are deterministic (the T11/T46 doctrine). The walk order is the
  order of addition, less what removal has moved; the shape deliberately does
  not claim `Ordered`, so no `Backward` is promised.
- **The zero value is ready.** Helpers make maps on first use, so there is no
  `built()` guard and no `layer.Constructing` — under `Guarded` the held
  container needs no forwarded constructor.

## 3. Options and diagnostics

`key=` (required — FRG3011 when missing), `unique=` (default true),
`conflict=error|replace` (default error, unique only), `index=` (secondary
fields). Uniqueness is checked, not discovered — "a key that has to be unique
is a thing to check", per the collection template's own doctrine — so
`AppendSeq` returns `ErrXDuplicate` under the default and the constructor
panics, its arguments being the caller's own values. `conflict=replace` swaps
the element in place and moves its secondary bucket membership.

New codes: FRG3037 unkeyable field, FRG3038 unexported field, FRG3039 a
secondary repeating the key, FRG3040 `conflict=` beside `unique=false`,
FRG3041 `index=` beside `unique=false`, FRG4103 two lookups spelled into one
name (`Id` + `ID` → `ByID`).

## 4. Two recorded deviations from the PRD row

The §7.5 row reads `Keyed ⇒ Sized, Indexed, Streamable`. The layer ships as
`⇒ Sized, Streamable`, and the PRD is deliberately not edited (the Ring
`size` precedent):

- **`requires: Keyed` is dropped.** Nothing in the tree adds `Keyed` —
  `shape.Subject` seeds only `Structured` — so the requirement as staged
  could never compose. R7 says keys come from options, and the `key=` option
  is the key declaration; its absence and its unusability are diagnostics,
  not shape algebra.
- **`adds: Indexed` is dropped.** `Indexed` means the language itself reaches
  an element by position (collection/ordering.go: the declared type's
  underlying form is a slice). This container is a struct; claiming the
  capability would invite a layer above to emit `c[i]` over a type with no
  positions.

## 5. Concurrency

None in the layer, per §7.4 and RK7: `Guarded[Index[...]]` composes — the
surface moves onto the held type, `Do`/`RDo` scope the lookups, and the
pointer a lookup answers must not outlive the scope (the same class of caveat
as Snapshot's shallowness). The worked example carries a
`Guarded[Index[Json[Person]]]` declaration exercised under the race detector.

## 6. The emission split

The template (`internal/layers/index/tmpl`) holds everything field-independent
as compiling generic Go: the entry type, constructors, appends, `Len`, `All`,
and generic method helpers (`pick`, `found`, `noted`, `listed`, `delisted`,
`cut`, `spread`, `grouped`). Built per declaration: the container struct, the
placing method's per-dimension statements, `Remove`, `Reset`, and one
single-expression `ByX` per dimension — the collection layer's split, with
the ring layer's drop-and-rename choosing between the compiled answers
(`New`/`NewChecked`, `AppendSeq`/`AppendSeqChecked`; `place`, `placeChecked`
and `Reset` are placeholders every run rebuilds).
