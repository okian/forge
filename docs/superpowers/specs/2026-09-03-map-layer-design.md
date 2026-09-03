# Map: a generated constructor from one type's values to another's

Status: design, decisions settled in conversation 2026-09-03. Stage 1 covers
the mapper; stage 2, sketched at the end, fuses it with the JSON codec.

## 1. What it is for

A service that reads one shape and answers with another writes the bridge by
hand: a constructor that copies eleven fields, renames two, and derives one.
Nothing checks that a twelfth field added to the target is ever set. The
mapping is mechanical exactly where it is boring, and dangerous exactly where
it is quiet — a field nobody copies arrives zero, in a value every round-trip
test builds the same wrong way.

The declaration names the two types and forge writes the constructor:

```go
//forge:map
type UserPerson Map[User, Person]
```

`Map[S, T]` is a marker like the others: two phantom parameters, no runtime
content. S — the source — may be a struct or an interface; T — the target —
is a struct. What comes out is a constructor in the declaring package:

```go
// PersonFromUser builds a Person from what a User holds.
func PersonFromUser(src *User) Person
```

A constructor rather than a method on T, so T's method set is untouched and T
need not be declared in the mapping's package. The name is derived from both
types, so two mappers onto one target coexist without a collision case:
`PersonFromUser`, `PersonFromRecord`. When S is an interface the parameter is
`src S` rather than `*S`, since an interface value is already a reference.

## 2. How members are matched

Every exported field of T must be **settled**, and there are exactly three
ways to settle one: matched automatically, assigned by a hint, or ignored on
purpose. A field settled no way is a diagnostic naming it — a silently zero
member is the failure this layer exists to remove, and no test that builds its
expectation through the same mapper would ever see it.

**Candidates.** For a struct S, the candidates are its exported fields and its
methods that take nothing and return one value; for an interface S, the
methods of that shape. A field of T named N is matched against candidates in
four rungs, first match wins:

1. a field of S named exactly N;
2. a method of S named exactly N;
3. the one candidate whose folded name equals folded N (lowercased with
   underscores dropped — the comparison the tag diagnostics already use), if
   exactly one;
4. nothing — the field must be hinted or ignored.

Two candidates on the same rung — two fields folding to one name, or a field
and a method both named N exactly (which Go forbids on one type, but not
across an embedding) — are **ambiguous**, and ambiguity is a diagnostic
rather than a preference: the hint is one line, and a guess would be a wire
between two types that nobody wrote.

**Types.** A matched pair is taken only when the candidate's type is
*assignable* to the field's. No numeric conversions, no string↔[]byte, no
elementwise slice mapping in this stage: a conversion is a decision about
values, and decisions about values are written in hints where the compiler
checks them. A name match whose types do not assign is reported as exactly
that, so the author learns the hint to write rather than hunting for why a
field went unmatched.

**Ignoring.** The declaration says which fields are left out on purpose:

```go
//forge:map ignore=Internal,Cached
type UserPerson Map[User, Person]
```

Ignored fields stay zero and the generated doc comment says so. An ignore
naming a field that does not exist, or one that auto-matching would have
settled, is a diagnostic — an option that does nothing is a sentence that
lies.

**Unexported fields of T.** Settable only when T is declared in the mapping's
package; a foreign T with unexported fields is refused the way the json layer
refuses one, and for the same reason: a constructor that cannot reach half the
value builds half a value and says nothing.

## 3. Hints

A hint is ordinary Go in the spec file, never linked into a real build and
never called — but **compiled**, under the `forgespec` tag, against the real
types. That is the whole reason to demand real Go rather than a comment
grammar: a hint that names a field wrongly or assigns across mismatched types
fails the build the author is already running, not the generator's parser.

```go
//forge:map hint
func personFromUser(src *User, dst *Person) {
	dst.Name = src.First + " " + src.Last
	dst.Age = int(src.AgeDays / 365)
}
```

The signature is fixed — `func(src *S, dst *T)` with those parameter roles,
any names — and the body is held to a deliberately narrow grammar: **plain
assignments whose left side is a field of dst**, one field per statement.
Locals, branches, loops and calls with side effects are refused with a
diagnostic saying where the boundary is. Narrow on purpose, twice over: the
left-hand sides are how forge knows which fields the hint settles, and a
conditional assignment would make "settled" a run-time question; and stage 2
inlines each right-hand side into the fused JSON writer, which only a pure
expression survives.

The right-hand side may mention `src` however it likes — arithmetic, calls to
pure functions, conversions — and is lifted into the constructor verbatim,
with `src` and `dst` respelled to the identifiers the generated body binds
(allocated out of the subject's way, as every generated identifier now is).

One hint per declaration. A second is a diagnostic naming both, because two
hints assigning one field is the same ambiguity rule 2 refuses. A hint
assigning a field that auto-matching already settled **wins over** the match —
the author wrote it down — but earns a note in the generated doc comment
naming the override, so the shadowing is visible where the reader is.

The hint function itself is claimed by the run: it exists only under the spec
tag, forge reads it, and the ordinary build never sees it. This is the pattern
the spec form already established for declarations, extended to one function.

## 4. What is refused

| refusal | why |
|---|---|
| a T field settled no way | the silent-zero member, the layer's reason to exist |
| two candidates on one rung | a guess would be a wire nobody wrote |
| a name match that does not assign | conversion is a hint's decision |
| hint statements beyond `dst.F = expr` | settlement must be static, and stage 2 must inline |
| two hints for one pair; two assignments to one field | same rule as two candidates |
| foreign T with unexported fields | a constructor cannot build half a value |
| S or T not a struct (or S not an interface) | there are no members to match |
| ignore naming a missing or already-settled field | an option that does nothing lies |

## 5. Generated shape

One constructor per declaration, in the declaration's file, with the doc
comment carrying the ledger: which fields matched by name, which came from the
hint, which were ignored. The ledger is the review surface — a mapping is
exactly the kind of code nobody reads until it is wrong.

```go
// PersonFromUser builds a Person from what a User holds.
//
// Matched by name: ID, Email. From the hint: Name, Age. Ignored: Internal.
func PersonFromUser(src *User) Person {
	held := Person{
		ID:    src.ID,
		Email: src.Email,
	}
	held.Name = src.First + " " + src.Last
	held.Age = int(src.AgeDays / 365)
	return held
}
```

The declared type `UserPerson` is generated as an empty struct carrying the
declaration's documentation, so explain and the fingerprint machinery have
their anchor; it holds no state and no methods in this stage.

No error return. A hint has nowhere to fail in the grammar above, and a
constructor that cannot fail is one every caller can use in an expression.
The day a conversion needs to fail is the day it is written as ordinary code
beside the mapper, not inside it.

## 6. Stage 2, sketched: `Map[S, Json[T]]`

With the append codec landed, the mapper composes with it into the method the
two could not offer alone: writing T's document straight from an S, with no T
ever built.

```go
//forge:map
type UserWire Map[User, Json[Person]]
```

adds, beside the constructor:

```go
func AppendJsonOfUser(dst []byte, src *User) ([]byte, error)
func WriteJsonOfUser(w io.Writer, src *User) (int64, error)
```

The fusion is a re-plumbing rather than a new codec: the json emitter already
writes each member from an expression, and the mapping table says what that
expression is — `src.ID` for a matched member, the hint's right-hand side for
a hinted one, nothing for an ignored one (which is settled-as-zero and writes
the zero). The strict hint grammar is what makes this sound: a pure expression
inlines into the writer exactly where the field's value would have been read.

Two properties gate the stage, both testable without an oracle:

1. **Byte equality**: `AppendJsonOfUser(nil, src)` equals
   `PersonFromUser(src)` followed by `AppendJSON(nil)`, for every fixture and
   under the per-fixture differential fuzzing the codec already runs.
2. **No T allocated**: zero allocations beyond the buffer's growth, held by
   the same budget gate as the codec's own writers.

Only one level composes: the second argument is `T` or `Json[T]`, and nothing
else. `Map[S, Collection[...]]` is refused — a container is a value that
exists, and the mapper's whole point is a value that does not.

Open for stage 2's own review: whether the fused pair belongs on the declared
type (`UserWire.AppendJson(dst, src)`) rather than as free functions, and what
the names are once forge's initialism rules have their say.

## 7. Plumbing this touches

- **A two-parameter marker.** Every marker today takes one type argument, and
  the composition grammar nests them in that slot. Map's first argument is a
  plain type reference, never a stack; its second is a subject or, in stage 2,
  exactly `Json[T]`. The stack parser learns this shape once, for this marker,
  rather than growing a general grammar nobody else needs.
- **Hint discovery** joins the spec loader: functions under the `forgespec`
  tag carrying `//forge:map hint`, matched to declarations by their parameter
  types, refused when they match none.
- **The naming shield applies**: `src`, `dst`, `held` and everything else the
  constructor binds are allocated through the same locals mechanism the other
  layers use, and the map row joins the shadow test.
- **The ledger doc comment** is emitted, not asserted by golden files: the
  agreement-style test builds a fixture package, compiles it, and checks the
  mapped values equal a hand-written reference mapping — the same
  compile-and-run pattern the codec fixtures use.

## 8. Test regime

1. A fixture package of S/T pairs covering every rung of the match ladder,
   every refusal in the table, interface sources, and hint overrides —
   compile-and-run, comparing generated constructors against hand-written
   reference mappings.
2. Diagnostics asserted by code and message substring, refused_test style.
3. The shadow test row for the map layer's bound identifiers.
4. Stage 2 adds the byte-equality property and the allocation budget rows.
