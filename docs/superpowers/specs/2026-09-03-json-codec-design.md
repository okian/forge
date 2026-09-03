# A generated JSON codec, written from scratch

Status: design, approved in conversation 2026-09-03. Supersedes the emission
strategy described in `internal/layers/jsoncodec/doc.go`.

This describes what the `jsoncodec` layer should write instead of what it writes
now: a serializer of its own rather than a caller of one. The measurements
quoted throughout were taken on an Apple M4 Pro under go1.27.1, and every one of
them is reproducible from a module named at the end of its section.

## 1. Why replace it

The layer's own documentation says the reason to declare it is not speed. That
turns out to understate the position. Measured against the reflective
`encoding/json/v2` path on one ten-member subject, a thousand elements at a
time:

| | ns/op | B/op | allocs | vs v2 |
|---|---|---|---|---|
| reflective `json.Marshal` | 756,577 | 49,094 | 5,002 | 1.00x |
| what forge writes today | 887,474 | 177,161 | 6,000 | 0.85x |

Encoding is slower than reflection and allocates three and a half times as
much. The cause is not reflection being cheap. It is that
`encoding/json/v2` stopped being a reflective encoder in the sense the phrase
suggests: `optimizeCommon` appends straight into the encoder's buffer and
bypasses `WriteToken` entirely (`v2/arshal_default.go:142, 228, 478, 577, 681,
828, 1190, 1508`, with `const optimizeCommon = true` at `:35`). The standard
library is already an append-into-bytes codec. Generated code that writes a
token at a time is paying a dispatcher the thing it competes with has deleted.

Three other things were found while measuring, and each is a defect rather than
a tradeoff.

**The generated decoders return unbalanced on error.** `Person.UnmarshalJSONFrom`
returns with the decoder at depth 1, `Recent.UnmarshalJSONFrom` at depth 2, and
the next value read from a shared decoder is then misparsed. Nothing catches it:
`encoding/json/v2` checks that a method consumed exactly one value only when the
method returned nil (`v2/arshal_methods.go:371`).

**A container marshals as `{}` under `encoding/json`.** `Recent.MarshalJSONTo`
has a pointer receiver (`examples/people/forge.gen.go:417`), and the v1
compatibility path calls a pointer-receiver method only on an addressable value.
A `Recent` value therefore falls through to reflective marshalling of a struct
whose fields are all unexported:

```
v1 Marshal( Recent)     = {}                          err=nil
v2 Marshal( Recent)     = [{"ID":1,"Name":"ada",...}]
v1 Marshal(Doc{Recent}) = {"R":{}}                     err=nil
```

`Credentials` escapes it only because its marshaller happens to have a value
receiver (`:1165`). `Roster` (`:677`) has the same exposure.

**Eight tag options are handled wrongly, most of them silently.** They are
listed in section 12 as stage 0, and they land before anything here, because
nothing should be built on a tag parser that ignores `omitEmpty`.

Reproduce section 1 from `scratchpad/emitbench` and `scratchpad/recv`.

## 2. The contract

Every subject gets four functions and no others:

```go
func (v Person)  AppendJSON(dst []byte) ([]byte, error)
func (v Person)  MarshalJSON() ([]byte, error)
func (v *Person) UnmarshalJSON(data []byte) error
func (v *Person) UnmarshalJSONBorrowed(data []byte) error
```

A container declared over one gets the same four, over the whole array, and the
streaming pair as well — the two halves neither the container nor the subject
could provide alone:

```go
func (c Persons)  AppendJSON(dst []byte) ([]byte, error)
func (c Persons)  MarshalJSON() ([]byte, error)
func (c *Persons) UnmarshalJSON(data []byte) error
func (c *Persons) UnmarshalJSONBorrowed(data []byte) error
func (c *Persons) WriteTo(w io.Writer)  (int64, error)
func (c *Persons) ReadFrom(r io.Reader) (int64, error)
```

The value receiver on the writing half is not incidental. A pointer receiver
there is what makes a container marshal as `{}` today, per section 1, so the
writing half takes a value receiver wherever the type permits one and the
reading half takes a pointer.

`AppendJSON` is the implementation; everything else reaches it. Nothing takes an
options argument, because the struct tag is the only thing that decides what a
value becomes, and admitting a second source of truth is what the previous
design spent most of its complexity on.

**Output is byte-identical to `json.Marshal(v, json.Deterministic(true))`** for
every subject the layer accepts. `Deterministic` is in that sentence because
v2's default map order is Go map iteration order and varies between runs, so no
output can be identical to v2's default for a map of more than one member —
v2's own is not identical to itself. Sorting is the only testable choice and it
is what v2 does when asked.

**Input accepts exactly what v2 accepts and refuses exactly what v2 refuses**:
syntax, UTF-8, duplicate member names across known and unknown members alike,
escaped member names, the number grammar, and merge semantics into a populated
destination.

**`format:` is the one deliberate divergence.** Forge honours it; v2 refuses it
in this release, type-wide, in both directions, even for the fields that carry no
tag:

```
err = json: cannot marshal from Go main.Untouched:
      Go struct field B has unsupported `format` tag option
```

The refusal is gated on `jsonflags.FormatTagSupported`, whose only setter is
internal (`jsonopts/options_format.go:118-124`), because `format` was withdrawn
from v2's first release pending typed struct tags (go.dev/issue/79071,
go.dev/issue/74472). Section 11 says how it is tested without an oracle.

**Not offered**: indentation, HTML and JS escaping, canonicalisation,
`WithMarshalers`, and the thirteen v1 legacy options. All of them belong to a
caller's encoder and there is no encoder. A caller who wants indented output
formats the bytes that come back.

**Memory**: `AppendJSON` allocates nothing beyond the growth of the buffer it
was handed. `WriteTo` is bounded by its flush window. `ReadFrom` is bounded by
the largest single element, not by the document — which is a stronger promise
than "streams", because it says what the bound is.

**Under `Json[Validate[T]]`**, a nil error from any reading function means every
rule the type declares holds, checked as the document was read; and on any error
the destination holds what it held before the call.

### Why `MarshalJSON` is emitted

It mentions only builtin types, so satisfying v2's interface costs no import,
and it makes v2 dispatch to this codec in every position it can reach. Without
it, a subject reached through `json.Marshal` is handled reflectively — which for
an ordinary subject is merely slower, but for a subject carrying `format:` is an
error, because the reflective path is the one that refuses. It also keeps one
codec per type rather than one per direction, which is the cost
`name.go:222-240` already regrets paying for text codecs.

The price is that the nested-in-someone-else's-struct path is slower than
reflection there (v2 validates and reformats the bytes it gets back: 0.78-0.87x
reflective, 12-13 allocations). That path is not the one being optimised and
correct-and-slow is the right side of that trade.

## 3. Decisions of record

| decision | resolution | why |
|---|---|---|
| surface | append-based, no jsontext in output | 3.1x to 6.2x encode and 2.5x to 3.1x decode against reflection, depending on subject shape; zero encode allocations in every case |
| caller options | not offered | removes the whole runtime-flag matrix and the `WithMarshalers` second body |
| `format:` | honoured, staged, closed set | author's requirement; divergence documented, tested through the experimental hatch |
| map order | always sorted | v2's default is not reproducible; sorted equals v2 under `Deterministic` |
| NaN, +-Inf | error unless `format:nonfinite` | conforms; no fixture in the repo depends on today's `"NaN"` |
| `time.Duration` | diagnostic demanding an explicit `format:` | v2 has no default representation; forge accidentally matched v1 |
| named scalar with `MarshalJSON` | honour the method | today forge writes the number and disagrees with every other reader |
| `omitzero` | `IsZero` -> `== zeroT` -> fieldwise -> `reflect.ValueOf().IsZero()` | retires FRG2010; reflect is already a transitive dependency of generated code |
| borrowing | separate opt-in function | v2 hands `UnmarshalJSON` a buffer it reuses, so the default must copy |
| stage 0 | its own change, landing first | hard breaks belong in an identifiable release, not inside a performance rewrite |

## 4. The append core

### Member names

A member's name and its punctuation are one string constant, so a member costs
one `append` and not three:

```go
dst = append(dst, `{"id":`...)
dst = strconv.AppendInt(dst, int64(v.ID), 10)
dst = append(dst, `,"name":`...)
```

The canonical quoted form is computed at generation time. A name needing no
escape — the overwhelming case — is baked; a name that does is baked in its
escaped form, since there is no caller whose escaping options could change it.

### Strings

The escaper writes exactly what v2 writes by default: `"`, `\`, and the C0
controls, with the five short forms and `\u00XX` for the rest. Not the three
HTML bytes, not U+007F, not U+2028 or U+2029 — those are a caller's option and
there is no caller. Invalid UTF-8 is an error rather than a substitution, which
is v2's default behaviour when `AllowInvalidUTF8` is off.

**Implemented and measured** against `jsontext.AppendQuote` on the same strings.
A byte-at-a-time loop loses on the case that matters most — long plain ASCII,
at 0.69x — because `jsontext` scans a word at a time. A word scan alone
overcorrects: 1.73x on plain text, and a loss on escape-dense content, where it
pays the setup before every escape and finds no run to skip.

What holds both is a word scan with a fallback. A word that is all ordinary
bytes skips eight; a word that is not moves the next attempt eight bytes along,
so a string escaping something every second byte stops paying for the attempt.
Median of seven:

| | ours | jsontext | |
|---|---|---|---|
| long plain ASCII | 31.2 | 105.1 | **3.37x** |
| multibyte | 185.8 | 211.6 | 1.14x |
| short escaped | 18.1 | 18.6 | 1.03x |
| short plain | 9.2 | 8.2 | 0.89x |
| escape every other byte | 506.8 | 424.8 | 0.84x |

The two figures under 1.0 are the honest cost: a short string is too short for a
word scan to help, and pathologically escape-dense content has no run to skip.
Both are the right side of the trade against 3.37x on ordinary text.

No `unsafe`. The word is assembled from eight indexed reads off a slice whose
bounds were checked once, which the compiler turns into a single load — so this
carries no dependency on byte order either.

One process note worth keeping, because it will recur. Flattening the loop to
satisfy the `nestif` linter put a function call in the per-byte path and cost
0.98x to 0.71x on short strings. The structure that satisfies both is a
four-arm switch with the word decision inside one arm: flat enough for the
linter, and no call in the hot path. Lint and the benchmark are both gates, and
one can be satisfied at the other's expense without anybody noticing.

### Numbers

`strconv.AppendInt` and `strconv.AppendUint` are byte-identical to v2 over
600,000 random values and are used directly.

Floats are the one place dropping `jsontext` costs something. v2's format is
`AppendFloat(dst, f, -1, bits)` with `'e'` chosen when `abs != 0 && (abs < 1e-6
|| abs >= 1e21)`, followed by rewriting `e-0X` to `e-X`, and `-0` staying `-0`
(`jsonwire/encode.go:213-236`). It is exported verbatim as
`jsontext.AppendFloat` (`jsontext/value.go:32-37`), and importing `jsontext`
solely for that one pure function would be defensible. This design reimplements
it — about fifteen lines — to keep the promise that generated output does not
depend on `jsontext` at all, with a differential fuzz target against
`jsontext.AppendFloat` as the safety net. **This is the one decision in this
document most worth revisiting on review**: the alternative costs one import of
a package containing no state and buys a stdlib guarantee for the fiddliest
formatting rule in JSON.

Pinned cases for either choice: `-0`, `1e20`, `1e21`, `1e-6`, `1e-7`, `1e-9`,
`5e-324`, `math.MaxFloat64`, `float32(1.0/3)`.

### `omitempty`

The append form makes this nearly free, where today it needs a second encoder
writing into a buffer (`encode.go:148-165`). Mark the length, write the member,
and take it back off the end if what was written was empty, using v2's own test —
the last two bytes against `ll`, `""`, `{}`, `[]`, with `len >= 3` required and a
preceding backslash disqualifying it (`jsontext/encode.go:252-283`):

```go
mark := len(dst)
dst = append(dst, `,"tags":[`...)
for i, one := range v.Tags {
    if i > 0 {
        dst = append(dst, ',')
    }
    if dst, err = appendString(dst, one); err != nil {
        return dst, err
    }
}
dst = append(dst, ']')
if len(dst)-mark == len(`,"tags":[]`) {
    dst = dst[:mark]
}
```

### `omitzero`

Four rungs, in order, first applicable wins:

1. the type declares `IsZero() bool` — call it, with v2's nil guards for pointer
   and interface receivers (`v2/fields.go:218-235`);
2. the type is comparable — `x != zeroT`, which sees unexported fields for free,
   at 1.6 ns;
3. the type is a non-comparable struct whose fields are visible — a generated
   fieldwise test, unrolled;
4. anything else — `reflect.ValueOf(x).IsZero()`, at 10.8 ns and no allocation.

Rung 4 admits `reflect` into generated source. That contradicts a sentence
`doc.go` currently sells the layer on, and the sentence was already imprecise:
`reflect` is a transitive dependency of `examples/people` today by way of
`encoding/json/v2`, and a binary linking that output already carries 260
`reflect.*` symbols. The honest form of the claim is that forge's own
encode and decode paths do no reflective work, except where a member's zero test
needs it, and the generated source says where.

Semantics rung 3 must preserve, each verified: `NonCmp{S: []int{}}` is **not**
zero, so the test is `s == nil` and never `len(s) == 0`; `[2][]int{{}, nil}` is
**not** zero; `struct{F float64}{-0.0}` **is** zero.

### Maps

Members come out in sorted key order, always. The idiom the emitter uses today,
`slices.Sorted(maps.Keys(m))`, is the entire allocation profile of the fast
path: 92.34 ns and 176 B in 6 allocations for three keys, and 21.34 ns and 64 B
in 3 allocations for a **nil** map. A pooled `[]string` with `slices.Sort` is
43.64/0/0 and 9.63/0/0. v2's own `Deterministic` path already pools
(`arshal_default.go:885`), so this matches the standard library rather than
inventing.

### Pools

Where a pool pays, measured (`scratchpad/weave`):

| | ns/op | B/op | allocs |
|---|---|---|---|
| `AppendJSON` into the caller's buffer | 39.7 | 0 | 0 |
| `MarshalJSON`, pooled scratch | 61.0 | 96 | 1 |
| `MarshalJSON`, nil and grow | 95.5 | 248 | 5 |
| `MarshalJSON`, sized guess | 87.2 | 256 | 1 |
| decode with escapes, pooled scratch | 356.5 | 70 | 4 |
| decode with escapes, no pool | 392.8 | 350 | 6 |
| decode with escapes, contended, pooled | 62.5 | 61 | 4 |
| decode with escapes, contended, no pool | 138.6 | 341 | 6 |

`AppendJSON` needs no pool, which is the point of handing the caller's buffer
back. A `sync.Pool` Get and Put pair costs 7.2 ns, so it earns its place only
where it replaces an allocation: `MarshalJSON`'s assembly buffer, the unescape
scratch, and `WriteTo`'s flush window. Under contention — which is what a pool
is for — it is 2.2x.

Two emission rules, both learned the hard way:

**Never return a slice that aliases a pooled buffer.** Copy first, then return
the buffer. Getting this backwards is a use-after-return that only `-race`
finds, and it was written by hand into the prototype without anyone noticing:

```
WARNING: DATA RACE
Write at 0x00c0002a9500 by goroutine 19:  weave.unescape()
Previous read at 0x00c0002a9500 by goroutine 26:  runtime.slicecopy()
                                                  weave.Person.MarshalJSON()
```

**Cap what goes back.** A buffer larger than 64 KiB is dropped rather than
pooled, so that one enormous document does not park its buffer for the life of
the process. This is the mistake easyjson caps at 32 KiB.

## 5. The scan core

### Entry points and the end of input

Only an entry point asserts that the document is one value with nothing after
it. A value nested inside another is followed by the rest of its parent, so the
nested decoder must not ask. The prototype got this wrong and its own agreement
test caught it: v2 refuses `{"id":1}}` and the hand-rolled reader accepted it.

```go
func atEnd(b []byte, i int) error {
    if skipSpace(b, i) != len(b) {
        return errSyntax
    }
    return nil
}
```

### Member dispatch

`switch string(name)` over the declared names. The compiler turns it into a
length switch and content comparisons, and the conversion allocates nothing by
compiler rule. A member name carrying an escape is unescaped into the scratch
buffer first, because `{"id":1}` must set `id` exactly as v2 sets it.

### Validation is not optional and costs 4.7%

The decisive measurement. Six decoders, each adding one guarantee, on a plain
document, median of six runs:

| decoder | ns/op | B/op | allocs |
|---|---|---|---|
| D0 unvalidated | 122.3 | 24 | 1 |
| D1 + JSON syntax | 129.0 | 24 | 1 |
| D2 + UTF-8 with byte offset | 125.5 | 24 | 1 |
| D3 + duplicate known members | 126.8 | 24 | 1 |
| D4 + duplicate unknown members | 127.7 | 24 | 1 |
| **D5 + escaped member names, fully conformant** | **128.1** | 24 | 1 |
| reflective `json.Unmarshal` | 391.2 | 0 | 0 |
| token stream, as forge writes today | 369.4 | 24 | 1 |

Full conformance costs 4.7% over an unvalidated scan, and the result is 2.9x
faster than what forge writes today. It holds on every document shape measured —
escaped, invalid-UTF-8-adjacent, unknown members, deeply nested unknown members —
where fully conformant lands at 128 to 185 ns against 391 to 553 for reflection.
The differential fuzzer against `encoding/json/v2`, agreeing on both the verdict
and the decoded value, cleared **49,478,923 executions with zero divergences**.

Reproduce from `scratchpad/decodecost/staged`.

### Duplicate names

Known members use a `uint64` bitset over member indices, which is what the
standard library itself does (`uintSet`, `v2/arshal_default.go:2002-2040`,
spilling to a slice past 64 members).

Unknown members need a set of arbitrary strings, and the obvious implementation
is the worst one. On a document with nested unknown members:

| | ns/op | B/op | allocs |
|---|---|---|---|
| span scan over recorded offsets | 173.9 | 24 | 1 |
| `map[string]struct{}` | 328.1 | 796 | 8 |
| open-addressed hash | 296.9 | 1272 | 4 |

The span scheme records each unknown name's offsets and compares against the
ones already recorded. It is quadratic in the number of unknown members and
linear in nothing else, which is the right shape: documents with many unknown
members are rare and documents with none — the common case — pay nothing. In
isolation it is 3.5, 10.3 and 27.6 ns for one, three and six unknown members,
against 50, 88 and 146 for the map, and it allocates nothing.

### Borrowing, and where `unsafe` earns its place

`unsafe` buys exactly one thing, and it is worth having. Strings are where
decode allocates; a scanned span with no backslash in it can be the destination
string rather than a copy of it. Measured on a string-heavy subject over 512
distinct documents, which is what it takes to stop v2's 256-slot string cache
from flattering the baseline:

| | ns/op | B/op | allocs | vs v2 |
|---|---|---|---|---|
| reflective | 523.4 | 188 | 9 | 1.00x |
| hand-rolled, copying | 181.7 | 188 | 10 | 2.88x |
| hand-rolled, borrowing | **120.0** | **0** | **0** | **4.36x** |

Nothing else considered for `unsafe` pays: the SWAR scan removes bounds checks
without it, `append(dst, s...)` is already free of a header copy, and
`m[string(b)]` lookups are already allocation-free by compiler rule.

Borrowing is offered and never default. `UnmarshalJSON` copies, because v2 hands
it a buffer v2 will reuse and the no-retain contract
(`v2/arshal_methods.go:112`) is not optional. `UnmarshalJSONBorrowed` aliases,
and says so where a reader will look:

```go
// UnmarshalJSONBorrowed fills v with strings and byte slices that point into
// data rather than copies of it. It is the quickest way in and the sharpest:
// data must outlive v and must not be modified, or v changes underneath its
// holder. Where that cannot be promised, UnmarshalJSON copies.
```

The hazard is real and it is easy to write a test that cannot see it — comparing
a borrowed string against a copy of itself never differs. Written correctly it
prints:

```
borrowed field changed under the caller: "Ada Lovelace" -> "Zda Lovelace"
```

`ReadFrom` cannot offer borrowing, because it recycles the buffer it reads into.

### Merge semantics and atomicity

Decode into a local seeded from the destination and assign on success:

```go
held := *v
n, err := decodePerson(data, &held, false)
if err != nil {
    return err
}
if err := atEnd(data, n); err != nil {
    return err
}
*v = held
return nil
```

This preserves the merge semantics v2 requires — a member the document does not
mention keeps the value the destination held (`v2/arshal.go:381-385`) — and it
costs nothing: in-place, zero-on-failure and scratch-and-assign all measure
between 156.8 and 161.8 ns, indistinguishable from each other and from no
validation at all.

It also removes a hazard that is live in the current design. A failed decode in
place leaves a value assembled from two documents, and that value can pass
`Validate()`:

```
InPlace after failed round 2: {ID:7 Name:REPLACED Email:ok@x}
  err = cannot read string from a JSON number
  Validate = <nil>
```

No field in that struct is jointly attested by any single input. Any caller
writing `if err != nil { log(err) }` and then using the value gets it.

One thing this cannot do: a container being read into is not restored. v2's
slice arshaler sets the length to include the failing element
(`arshal_default.go:1588`) and its map arshaler inserts before checking the
error (`:1059` against `:1064`). The contract says so rather than implying
otherwise.

## 6. The validate weave

Under `Json[Validate[T]]` the rules are held as the document is read. Every rule
`validate` emits is a per-field rule — there are no whole-struct tag rules — so
every rule chain is emitted immediately after its member's value lands, and the
first failure ends the read:

```go
case "name":
    if seen&bitPersonName != 0 {
        return 0, errDupName
    }
    seen |= bitPersonName
    lo, hi, next, esc, err := scanString(b, i)
    if err != nil {
        return 0, err
    }
    if v.Name, err = take(lo, hi, esc); err != nil {
        return 0, err
    }
    i = next
    // validate:"required,max=64"
    switch {
    case v.Name == "":
        return 0, ValidationError{Path: "Name", Rule: "required", Want: "a value"}
    case len(v.Name) > 64:
        return 0, ValidationError{Path: "Name", Rule: "max=64", Want: "at most 64 characters"}
    }
```

This is what fail-fast buys, and it is the whole point: what follows a broken
member is never parsed.

```
doc = {"id":1,"name":"a","email":"a@b","age":9999,"tags":[[[[
  -> Age: max=150 wants at most 150
```

Not a syntax error. The garbage after `age` was never read. On a ten-thousand
element document whose five-thousandth element is invalid, that is 96,400 ns
against 191,200 for reading everything and validating afterwards, and 330 KB of
peak allocation against 2.49 MB.

### The absent member

A rule that asks for a value has to be answered by a member that never arrived
as well as by one that did, and there is no parse moment to hang that on. The
bitset that catches a duplicate name is the same bitset that says which names
were absent, so this costs a mask test rather than a second pass:

```go
if seen&bitPersonName == 0 {
    switch {
    case v.Name == "":
        return 0, ValidationError{Path: "Name", Rule: "required", Want: "a value"}
    case len(v.Name) > 64:
        return 0, ValidationError{Path: "Name", Rule: "max=64", Want: "at most 64 characters"}
    }
}
```

### What still happens at the end, and when nothing does

For a subject whose rules are all tag rules — the common case — nothing runs
after the loop except the absent-member tail. `Validate()` is not called.

Two things must wait for a complete value, because they are functions of one: a
per-field author hook (`ValidateEmail() error`, detected at
`validate/plan.go:264`) and a type's own declared `Validate() error` (delegated
at `plan.go:198-214`). Both are methods on the whole value and can read members
the loop has not reached. Where a subject has neither, the generated decoder
contains no end-of-value call at all, and the emitted comment says so.

### Nested subjects

A nested struct's own decoder validates inline and returns early; the parent
re-paths the error through the existing `nestedValidation` helper
(`layers/failures/shared/nested.go:27-38`) so it still reads `Home.City`.

Because the decoder emits the rule predicates rather than a call to `Validate()`,
a nested struct is not checked twice and its failures do not appear twice. That
dissolves a question the composition study had to leave open.

### One asymmetry, stated

The decode error names the **first** violation. `Validate()` still names **all**
of them, by accumulating into `ValidationErrors`. That difference is what buys
the abort, and it is documented rather than smoothed over.

### `regexp` rules cost more than the parse

Rule costs are not in one class:

| | ns/op |
|---|---|
| `required` and `max=64` | 1.6 |
| `regexp=^[^@[:space:]]+@[^@[:space:]]+$` | 158.2 |

Compilation is not the cost and is already hoisted — `validate/write.go:62-78`
emits a package-level `regexp.MustCompile` per rule, paid once at 1,220 ns. The
158 ns is the engine matching, per call, and it scales with input length:

| input bytes | `regexp` | written out |
|---|---|---|
| 8 | 82.5 | 4.8 |
| 16 | 160.0 | 9.3 |
| 64 | 575.0 | 31.9 |
| 256 | 2253.0 | 133.9 |

About 8.8 ns per byte against 0.52, a flat seventeen times at every length.
Forge knows the pattern when it writes the code, so for patterns it can prove it
understands it writes the match instead of compiling one:

```go
func matchPersonEmail(s string) bool {
    at := -1
    for i := 0; i < len(s); i++ {
        switch s[i] {
        case '@':
            if at >= 0 {
                return false
            }
            at = i
        case ' ', '\t', '\n', '\v', '\f', '\r':
            return false
        }
    }
    return at > 0 && at < len(s)-1
}
```

The subset is named and conservative: literals, anchors, character classes
including the POSIX ones, `+`, `*` and `?` applied to a single class, and
concatenation of those. Alternation of literals is a candidate. Anything with a
group under a quantifier, a capture whose value matters, or a non-greedy
operator keeps `regexp.MustCompile`, and the generated source says why — the way
the layer already says why it refused something.

Forge emits the proof with the code: one differential fuzz target per
written-out pattern, comparing the matcher against
`regexp.MustCompile(pattern)`, so equivalence is checked in the author's own
continuous integration against the author's own patterns. A pleasant consequence
is that the compiled pattern then exists only in the test binary, and production
output stops paying its 4,440 B of initialisation.

Reproduce section 6 from `scratchpad/weave`.

## 7. Streaming

### Writing

`WriteTo` appends into a pooled buffer and flushes on a threshold:

```go
if len(b) >= flushWindow {
    n, err := w.Write(b)
    counted += int64(n)
    if err != nil {
        return counted, err
    }
    b = b[:0]
}
```

A 4 KiB window measured 79,374 ns against 287,623 for the encoder route, so the
expectation for `examples/people` is `JSONWriteTo` moving from 170,407 ns with
6,784 B in 24 allocations to roughly 80,000 ns and one or two.

The `counting` writer wrapper survives in reduced form, because it still has to
report what the writer took (`examples/people/codec_test.go:254`), but the
`jsontext.NewEncoder` per call disappears.

### Reading

`ReadFrom` is the one piece of genuinely new work that dropping `jsontext`
creates, and the design is deliberate about its bound. It reads the framing
tokens itself and hands each element's complete bytes to that element's scanner,
refilling from the reader when the window runs out:

- `scanValueEnd(buf []byte, st *scanState) (end int, needMore bool)` finds the
  end of the next value, tracking nesting depth, string state and escape state
  so that a brace inside a string does not close the object it appears in, and
  so that a refill can resume mid-value.
- The buffer grows to the largest single element and no further, which is the
  bound the contract states.
- The element scanner is the same generated function `UnmarshalJSON` calls, so
  there is one scanner per subject and not two.

`stack_test.go:92` currently forbids the substring `append(` in the emitted
container codec. That assertion has to be replaced by the property it was
protecting — that a flush threshold exists and that `slices.Collect` does not —
because the new emitter appends by construction.

## 8. Tag coverage

Everything a struct tag says is decidable when the code is written. That is the
structural finding the whole design rests on, and it was verified item by item:
member names and their canonical quoted bytes, the flattened field set with its
breadth-first order, index paths, dominance and `id` numbering, the fold buckets
and which folded key is ambiguous, `omitzero`'s test shape, `omitempty`'s test
shape, `string`'s legality, every `format` value, and member order.

| option | what is emitted |
|---|---|
| name | one baked constant per member, fused with its punctuation |
| `-` | the member is not written and not matched |
| `case:strict` | already the emitted behaviour; accept the option and emit nothing new |
| `case:ignore` | exact `switch` first, then a `switch` on the folded name whose keys are computed at generation time; a folded key reaching two members emits v2's ambiguity error. Folding drops `_` and `-`, upcases ASCII, and takes the smallest rune of each `unicode.SimpleFold` set (`v2/fold.go:17-45`) |
| `embed` | the target's members flattened at the embed's position, with a nil guard for an embedded pointer |
| `omitzero` | the four-rung ladder of section 4 |
| `omitempty` | mark and retract, with v2's two-byte test |
| both together | the **or** of the two conditions. Forge currently lets `omitzero` win and drops `omitempty`, writing `"s":[]` where v2 omits the member |
| `string` | a quote, the number, a quote on the way out; unquote and a strict number grammar on the way in — no leading `+`, no `07`, no `1e2` into an integer, `null` to zero |
| `format:` | a call to the appender or parser named by the value; see stage 6 |

`RejectUnknownMembers`, `MatchCaseInsensitiveNames` and the rest of the caller
options are not honoured, per section 2.

## 9. What stays refused, and the way out

None of it is a tag feature.

| refusal | argument |
|---|---|
| interface-typed field, `chan`, `func` | the encoding depends on a type nobody knows until run time |
| `complex64`, `complex128`, `unsafe.Pointer` | no JSON form; v2 refuses too |
| a struct in another module with unreadable state and no text or JSON codec | generated code cannot read the fields. Shrinks by exactly two types once `format:` lands — `time.Time` and `time.Duration`, recognised by type identity as v2 recognises them (`arshal_time.go:42, 134`) |
| a type reaching itself with no struct in between | no finite member set |
| a type declaring one half of a codec | generating the pair would redeclare the half already there |
| a `format:` value outside the verified closed set | v2 rejects unknown values too; better as a diagnostic than a runtime error |

The escape hatch gets simpler rather than harder. Where a field cannot be
generated for, its bytes come from the standard library and are spliced in,
because v2's output is already valid compact JSON:

```go
b, err := json.Marshal(v.Anything)
if err != nil {
    return dst, err
}
dst = append(dst, b...)
```

Today's `fallback=stdlib` needs a second encoder for the same job.

## 10. Errors

An error names the rule or the syntax, the member, and the byte offset. It does
not imitate v2's error text, and no test may match v2's strings: the modal verb
is randomised once per process between "cannot" and "unable to"
(`v2/errors.go:322-333`).

One landmine, verified: a generated method must never return a self-built
`*jsonv2.SemanticError` without `GoType` set. The v1 shim copies `Type:
err.GoType` (`encoding/json/v2_inject.go:144-151`) and then calls
`e.Type.String()` (`v2_decode.go:148`), which is a nil dereference. Returning
the layer's own error type instead renders correctly under both packages and
keeps `errors.Is` and `errors.As` working through every wrapper.

## 11. The test regime

The oracle is the standard library, and the existing
`internal/layers/jsoncodec/testdata/agreement.go.txt` mechanism survives.

1. **Byte equality.** For every fixture, `AppendJSON`, `MarshalJSON` and the
   container's writers against `json.Marshal(twin, json.Deterministic(true))`.
   `codec_test.go:220` already fails when a fixture never reaches a comparator;
   extend its map so a new entry point cannot be added untested.
2. **Verdict and value agreement.** For every fixture and both reading
   functions, the same accept-or-refuse decision as v2 and the same resulting
   value, over a corpus of good and malformed documents.
3. **Differential fuzzing, per fixture.** The target that found nothing in
   49,478,923 executions becomes an obligation for every fixture shape rather
   than a one-off: agreement on verdict and on value. Added to
   `scripts/fuzz.sh`'s discovered set.
4. **The escaper and the float formatter** each get a differential fuzz target
   against `jsontext.AppendQuote` and `jsontext.AppendFloat`, comparing bytes
   **and** error, plus exhaustive tests over all 256 one-byte and all 65,536
   two-byte inputs.
5. **Written-out `regexp` matchers**, each against its compiled pattern.
6. **`-race`.** The pools are shared mutable state in generated output.
   `internal/racetest/matrix` is regenerated and `make race` is the proof, not a
   formality — a use-after-return race was written into the prototype by hand
   and only the detector saw it.
7. **Regression tests for the two live bugs**: a value marshalled through
   `encoding/json` must not come back `{}`, and a rejected value must leave the
   reader positioned so the next value on the same input reads correctly.
8. **Benchmarks must use distinct payloads.** Repeating one document hands v2 its
   256-slot string cache and silently invalidates the comparison; 512 documents
   only half-defeats it, so fixtures use several thousand.
9. **`format:` is tested through the experimental hatch** — an options value
   whose method set includes `ExperimentalSupportFormatTag() bool`, matched
   structurally by `jsonopts` — so there is a reflective oracle in tests even
   though users have none.
10. **The performance gate.** `scripts/budget.txt:66-70` is rewritten in the same
    change. Targets: `JSONEncode` at most 0.5x `JSONEncodeReflectively`;
    `JSONWriteTo` at most two allocations and 0.6x today's time; `JSONDecode` no
    worse than today and at most 0.6x reflective; zero allocations for
    `AppendJSON` and for borrowing decode. A map-bearing and a text-codec-bearing
    subject are added to the benchmarked example, because `examples/people` has
    neither today and the map-key win is otherwise unmeasurable by the gate.
11. **The rest of the gate unchanged**: `make check`, vet including `-tags
    forgespec`, lint at the existing thresholds, `COVER_MIN=90`, and the
    `cmd/forge` size ceiling. Note that `./scripts/bench.sh` fails today on
    `Validate` and `ValidateByHandWithThePattern` at 1 B/op against a budget of
    0; that failure predates this work and must not be read as a regression.

## 12. Staging

Each stage is independently shippable and independently reviewable.

**Stage 0 — the conformance diagnostics.** Its own change, landing first, no
emitter work. The `omitEmpty` and `omit_empty` near-misses; duplicate options
including `case:ignore` with `case:strict`; `nm,embed` and `,embed,omitzero`; an
unexported field carrying a json tag; a struct with no exported fields;
`omitzero` and `omitempty` as the **or**; `time.Duration` demanding an explicit
`format:`; and the `,embed` fixture that does not exist today. Every one is a
hard break for an author, and `diag` has no warning tier, which is the argument
for putting them in an identifiable release of their own.

**Stage 1 — the cores.** `AppendJSON` and the scanner, the runtime template, the
pools, the escaper, numbers, member dispatch, the validation ladder, the span
set, borrowing, `MarshalJSON`, `UnmarshalJSON`, `UnmarshalJSONBorrowed`. The
token emitter is deleted rather than kept as a fallback.

**Stage 2 — the validate weave.** Inline rule chains, the absent-member tail,
the hook and delegated-`Validate` tail where a subject has one, nested
re-pathing.

**Stage 3 — streaming.** `WriteTo` with its window; `ReadFrom` with
`scanValueEnd` and the largest-element bound.

**Stage 4 — presence semantics and `,string`.** The `omitzero` ladder in full,
retiring FRG2010; `omitempty`'s retract; `,string` both directions with its
legality diagnostics.

**Stage 5 — `case:ignore` and `case:strict`.** The folded switch and the
statically computed ambiguity table. `case:strict` is a free accept and may go
in stage 0 if a quick win is wanted.

**Stage 6 — `format:`**, in four sub-stages, each shippable:
6a `base64`, `base64url`, `base32`, `base32hex`, `base16`, `hex`, `array`, the
RFC 4648 length rule and the `[N]byte` mismatch error;
6b `nonfinite`, `emitnull`, `emitempty`;
6c the sixteen named `time` layouts, arbitrary layouts, and RFC 3339 validation;
6d `unix`, `unixmilli`, `unixmicro`, `unixnano`, the duration
`sec`/`milli`/`micro`/`nano` scales, and `iso8601` — about 120 lines of exact
fixed-point integer arithmetic, unexported in the standard library, and the only
part of `format:` with no reachable reference implementation. `micro` on
`MaxInt64` is `9223372036854775.807`, nineteen significant digits, so `float64`
is not available as a shortcut.

**Stage 7 — the embedded fallback.** `,embed` on `map[~string]T` and
`jsontext.Value`: emitted after every static member, each dynamic name checked
against the static set exactly and then fold-aware, with raw splicing for the
value variant.

**Stage 8 — written-out `regexp` matchers** and their differential targets.

## 13. What is deleted

`internal/layers/jsoncodec/encode.go` and `decode.go` are replaced rather than
extended. The buffered second encoder for `omitempty` goes. The `errNonSingular`
protocol, the balance-on-error concern and the `AvailableBuffer` dance go with
`jsontext`. `plan.go`, `name.go` and the condition engine stay: type
classification, refusals, ordering, cycle detection and naming are
strategy-independent and were never the problem.

`doc.go` is rewritten. Three of its claims are now wrong: that the reason to
declare the layer is not speed, that appending a value and writing it whole was
tried and is slower, and that the output contains no reflection. The first two
were true of the encoder-mediated design and are not true of this one; the third
was already imprecise.

## 14. Open on review

1. **The float formatter.** Reimplement v2's rule, or import `jsontext` for
   `AppendFloat` alone and keep the standard library's guarantee on the fiddliest
   formatting in JSON. This document chooses the former to honour "no jsontext
   in generated output", and it is the choice most worth arguing about.
2. **`MarshalJSON`.** Emitted here, for the reasons in section 2. The
   alternative is that a `format:`-bearing subject reached through `json.Marshal`
   errors.
3. **SWAR or table** for the escaper and the string scan. Left to per-shape
   benchmark rather than prescribed, which means the first implementation picks
   one and the gate keeps it honest.
