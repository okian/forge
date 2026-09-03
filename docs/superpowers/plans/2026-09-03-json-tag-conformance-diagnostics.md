# JSON Tag Conformance Diagnostics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `jsoncodec` refuse every json struct tag that `encoding/json/v2`
refuses, instead of silently ignoring eight of them.

**Architecture:** No emitter work. Seven of the eight defects are new
generation-time diagnostics raised from `(*planner).unsupported` and its
neighbours in `internal/layers/jsoncodec/plan.go`; the eighth is a two-line
correction to `(*writer).omitted` in `encode.go` where a `switch` makes
`omitzero` and `omitempty` exclusive when the standard library takes their
disjunction. Each defect gets a fixture type in
`testdata/codec/refused/refused.go` and a row in the existing table test.

**Tech Stack:** Go 1.27.1, `encoding/json/v2` as the conformance oracle, the
`internal/diag` code registry, the `plugin` façade layers report through.

**Spec:** `docs/superpowers/specs/2026-09-03-json-codec-design.md` — this plan
implements Stage 0 of section 12, which the spec and the author both place
before all other stages because nothing should be built on a tag parser that
ignores `omitEmpty`.

## Global Constraints

- Go 1.27.1; module `github.com/okian/forge`; `go.mod` declares `go 1.27.0`.
- Standard library only. The single non-test dependency is `golang.org/x/tools`;
  do not add another.
- `internal/tags` **parses and does not judge**, by explicit design
  (`internal/tags/json.go:60-71`). Every diagnostic in this plan belongs to the
  consumer — `internal/layers/jsoncodec` — and none of it changes `internal/tags`.
- Diagnostic codes 2001 through 2030 are taken. New codes in this plan are
  **2031, 2032, 2033** and are registered with `plugin.Register` beside the
  existing ones at `internal/layers/jsoncodec/plan.go:21-25`.
- Every refusal carries a hint saying what to do instead, because
  `TestWhatACodecRefusesToWrite` asserts the hint as well as the code and the
  message. A refusal with no way out fails that test.
- A diagnostic message opens with the field's name where there is one, so the
  complaint reads as being about something the author wrote
  (`plan.go:540-544`).
- Never assert against `encoding/json/v2` error *text*. Its modal verb is
  randomised once per process between "cannot" and "unable to"
  (`v2/errors.go:322-333`). Assert on conditions.
- Every one of these diagnostics is a hard break for an author: a package that
  generates today will refuse tomorrow, and `internal/diag` has no warning tier.
  That is why this is its own change and its own release.
- House style: prose comments that say why rather than what, no bullet lists in
  Go comments, and a sentence that could be read aloud. Match the surrounding
  file; `plan.go:735-741` is a representative sample.

---

### Task 0: Unblock git

**Files:** none — repository state only.

**Interfaces:**
- Consumes: nothing.
- Produces: a working repository, without which no later task can run its
  commit step.

- [ ] **Step 1: Confirm the fault**

Run: `git -C /Users/kian/projects/kian/forge status --short`
Expected: `fatal: this operation must be run in a work tree`

- [ ] **Step 2: Confirm the cause**

Run: `grep bare /Users/kian/projects/kian/forge/.git/config`
Expected: `	bare = true`

This is wrong: the repository has a working tree and a 66 KB index. The flag was
set mid-session on 2026-09-02 at 22:57 and no source file was modified — verified
by `find . -type f -not -path './.git/*' -newermt '-12 hours'` returning nothing.

- [ ] **Step 3: Repair it**

```bash
git -C /Users/kian/projects/kian/forge config core.bare false
```

- [ ] **Step 4: Verify the repository is intact**

Run: `git status --short && git log --oneline -3 && git branch --show-current`
Expected: a clean or spec-only working tree, the three commits ending
`2f60e3a refactor: keep the dictionary as words, and index it when it loads`,
and branch `main`.

- [ ] **Step 5: Branch, and commit the spec and this plan**

```bash
git checkout -b json-tag-conformance
git add docs/superpowers/specs/2026-09-03-json-codec-design.md \
        docs/superpowers/plans/2026-09-03-json-tag-conformance-diagnostics.md
git commit -m "docs: say what a generated JSON codec should be, and how to get there"
```

Commits will be attributed to `Kian Ostad <c@kianostad.com>`: the repository's
local config sets that and it overrides the global
`kian.ostad@rogontechnologies.com`.

---

### Task 1: Refuse a misspelled tag option

**Files:**
- Modify: `internal/layers/jsoncodec/plan.go` — add `knownOptions`,
  `normalizedOption`, `(*planner).misspelledOption`; call it first from
  `(*planner).unsupported` at `:742`
- Modify: `internal/layers/jsoncodec/testdata/codec/refused/refused.go` — add
  `MisspelledOption`
- Test: `internal/layers/jsoncodec/refused_test.go` — add one table row

**Interfaces:**
- Consumes: `optionCase`, `optionEmbed`, `optionOmitZero`, `optionOmitEmpty`,
  `optionString`, `optionFormat` (`plan.go:556-563`); `codeTagOption`
  (`plan.go:22`); `plugin.Field.Tag(jsonKey) (plugin.Tag, bool)`;
  `plugin.Tag.Options []plugin.TagOption` with fields `Name`, `Raw`.
- Produces: `func (p *planner) misspelledOption(field plugin.Field) (plugin.Diagnostic, bool)`,
  and `func normalizedOption(name string) string` reused by Task 2.

`encoding/json/v2` normalises a written option by lowercasing it and dropping
underscores, and refuses it when the normalised form is a real option spelled
some other way (`v2/fields.go:549-553`). An option that normalises to nothing
known is ignored (`:555-557`), so `zzz` stays legal and `omitEmpty` does not.
Forge currently ignores both, which means a field tagged `json:",omitEmpty"`
writes a member the author asked to omit.

- [ ] **Step 1: Add the fixture**

Append to `internal/layers/jsoncodec/testdata/codec/refused/refused.go`:

```go
// MisspelledOption writes omitempty in a spelling the standard library
// refuses. Ignoring it would write a member the author asked to leave out,
// which is the one kind of wrong a round trip through this same codec cannot
// see: both halves would agree the member belongs there.
type MisspelledOption struct {
	Tags []string `json:"tags,omitEmpty"`
}
```

- [ ] **Step 2: Write the failing test row**

In `internal/layers/jsoncodec/refused_test.go`, add to the `cases` map in
`TestWhatACodecRefusesToWrite`:

```go
		"MisspelledOption": {"FRG2008", "omitEmpty", "omitempty"},
```

- [ ] **Step 3: Run it to make sure it fails**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestWhatACodecRefusesToWrite/MisspelledOption' -v`
Expected: FAIL with `a codec was written for MisspelledOption`

- [ ] **Step 4: Implement the check**

In `internal/layers/jsoncodec/plan.go`, after the option constants at `:563`:

```go
// knownOptions are the options a json tag may carry, which is the set the
// standard library compares a misspelling against.
var knownOptions = []string{
	optionCase, optionEmbed, optionFormat,
	optionOmitEmpty, optionOmitZero, optionString,
}

// normalizedOption is the standard library's comparison for an option that
// might be one of the known ones written differently: lowercase, with
// underscores dropped (encoding/json/v2, fields.go:549-553).
func normalizedOption(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

// misspelledOption reports an option that is one of the known ones in every
// respect except how it is written.
//
// An option nobody recognises at all is left alone, because the standard
// library leaves it alone: a tag may carry a word this grammar has no meaning
// for and still be a tag. What it may not carry is omitEmpty, because a reader
// seeing that word expects the member to be left out and both this codec and
// the standard library would write it.
func (p *planner) misspelledOption(field plugin.Field) (plugin.Diagnostic, bool) {
	tag, ok := field.Tag(jsonKey)
	if !ok {
		return plugin.Diagnostic{}, false
	}

	for _, option := range tag.Options {
		if option.Name == "" {
			continue
		}
		normalized := normalizedOption(option.Name)
		for _, known := range knownOptions {
			if option.Name != known && normalized == known {
				return plugin.New(codeTagOption, field.Pos,
					"%s is written with %q, which is %q spelled another way",
					field.Name, option.Name, known).
					WithHint("%s", misspelledOptionHint(known)), true
			}
		}
	}

	return plugin.Diagnostic{}, false
}

// misspelledOptionHint says which spelling to use, because the option the
// author meant is the one thing they cannot read off the complaint.
func misspelledOptionHint(known string) string {
	return "the standard library reads this option only as " + strconv.Quote(known) +
		"; write it that way, or remove it"
}
```

Then make it the first check in `unsupported` at `:742`:

```go
func (p *planner) unsupported(field plugin.Field) (plugin.Diagnostic, bool) {
	if wrong, found := p.misspelledOption(field); found {
		return wrong, true
	}

	if option, ok := tagOption(field, optionFormat); ok {
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestWhatACodecRefusesToWrite/MisspelledOption' -v`
Expected: PASS

- [ ] **Step 6: Run the whole layer to check nothing else moved**

Run: `go test ./internal/layers/jsoncodec/`
Expected: ok

- [ ] **Step 7: Commit**

```bash
git add internal/layers/jsoncodec/plan.go \
        internal/layers/jsoncodec/refused_test.go \
        internal/layers/jsoncodec/testdata/codec/refused/refused.go
git commit -m "fix: refuse an option that is omitempty spelled another way"
```

---

### Task 2: Refuse a repeated tag option

**Files:**
- Modify: `internal/layers/jsoncodec/plan.go` — add
  `(*planner).repeatedOption`, call it from `unsupported`
- Modify: `internal/layers/jsoncodec/testdata/codec/refused/refused.go` — add
  `RepeatedOption` and `ContradictoryCase`
- Test: `internal/layers/jsoncodec/refused_test.go` — two table rows

**Interfaces:**
- Consumes: `plugin.Tag.Count(name string) int` (`internal/tags/tag.go:113`),
  which exists and has never been called; `knownOptions` from Task 1.
- Produces: `func (p *planner) repeatedOption(field plugin.Field) (plugin.Diagnostic, bool)`.

`encoding/json/v2` refuses a repeated option (`v2/fields.go:565`) and gives
`case:ignore` written beside `case:strict` its own message (`:563`). Forge's
`Tag.Lookup` resolves a repeat to its first occurrence, which is a lookup policy
and documented as one — the judgement was always meant to live here.

- [ ] **Step 1: Add the fixtures**

```go
// RepeatedOption writes one option twice. The standard library refuses it, and
// a tag that says a thing twice may mean either of two things, so a codec that
// took the first would be choosing on the author's behalf.
type RepeatedOption struct {
	Tags []string `json:"tags,omitempty,omitempty"`
}

// ContradictoryCase asks for a name to be matched both loosely and exactly.
// That is the repeat that reads as a contradiction rather than as a
// duplication, and it is the one the standard library names separately.
type ContradictoryCase struct {
	Name string `json:"name,case:ignore,case:strict"`
}
```

- [ ] **Step 2: Write the failing test rows**

```go
		"RepeatedOption":    {"FRG2008", "omitempty", "once"},
		"ContradictoryCase": {"FRG2008", "case", "once"},
```

- [ ] **Step 3: Run them to make sure they fail**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestWhatACodecRefusesToWrite/(RepeatedOption|ContradictoryCase)' -v`
Expected: FAIL for both with `a codec was written for ...`

Note that `ContradictoryCase` may instead fail by being reported as `FRG2008`
for `case:ignore` through the existing check at `:749`. That is still a failure
of this test — the message will not contain "once" — and the order of checks in
Step 4 is what fixes it.

- [ ] **Step 4: Implement the check**

```go
// repeatedOption reports an option written more than once.
//
// Once is a lookup policy question and internal/tags answers it by taking the
// first, which is the only answer a lookup can give. Whether a tag was allowed
// to say it twice is a different question and this is where it is asked: a tag
// carrying two values for one option describes two wire formats, and choosing
// between them is not a generator's decision to make quietly.
func (p *planner) repeatedOption(field plugin.Field) (plugin.Diagnostic, bool) {
	tag, ok := field.Tag(jsonKey)
	if !ok {
		return plugin.Diagnostic{}, false
	}

	for _, known := range knownOptions {
		if tag.Count(known) < 2 {
			continue
		}
		return plugin.New(codeTagOption, field.Pos,
			"%s writes %q more than once", field.Name, known).
			WithHint("%s", repeatedOptionHint), true
	}

	return plugin.Diagnostic{}, false
}

const repeatedOptionHint = "an option may be written once; " +
	"keep the one that says what the wire format is and remove the other"
```

Call it in `unsupported` immediately after `misspelledOption`, and **before**
the `optionFormat` and `optionCase` checks, so that a contradiction is reported
as a contradiction rather than as an unsupported `case`:

```go
	if wrong, found := p.repeatedOption(field); found {
		return wrong, true
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestWhatACodecRefusesToWrite/(RepeatedOption|ContradictoryCase)' -v`
Expected: PASS for both

- [ ] **Step 6: Run the whole layer**

Run: `go test ./internal/layers/jsoncodec/`
Expected: ok

- [ ] **Step 7: Commit**

```bash
git add internal/layers/jsoncodec/plan.go \
        internal/layers/jsoncodec/refused_test.go \
        internal/layers/jsoncodec/testdata/codec/refused/refused.go
git commit -m "fix: refuse a json tag that writes one option twice"
```

---

### Task 3: Refuse an embed option written with anything else

**Files:**
- Modify: `internal/layers/jsoncodec/plan.go` — add
  `(*planner).impureEmbed`, call it from `unsupported`
- Modify: `internal/layers/jsoncodec/testdata/codec/refused/refused.go` — add
  `NamedEmbed` and `DecoratedEmbed`
- Test: `internal/layers/jsoncodec/refused_test.go` — two table rows

**Interfaces:**
- Consumes: `optionEmbed`; `plugin.Tag.Name string`; `plugin.Tag.Options`.
- Produces: `func (p *planner) impureEmbed(field plugin.Field) (plugin.Diagnostic, bool)`.

`encoding/json/v2` refuses an `embed` written with a name or with any other
option: "cannot have any options other than `embed` specified"
(`v2/fields.go:140-147`). Forge discards the name and the extra option and
promotes the members anyway, so `json:"wrapper,embed"` produces an object with
no `wrapper` member in it and nothing says why.

- [ ] **Step 1: Add the fixtures**

```go
// Inner is a struct with members to promote, so that what is wrong with the
// two fixtures below is the tag rather than the type.
type Inner struct {
	A int `json:"a"`
}

// NamedEmbed gives a name to a field whose members are promoted, which is a
// name nothing will ever be written under: promotion is what embed means and a
// promoted member carries its own name.
type NamedEmbed struct {
	Inner `json:"wrapper,embed"`
}

// DecoratedEmbed asks for a promoted field to be omitted when it is zero.
// There is no member to omit — the members are the enclosing struct's — so the
// option describes something that cannot happen.
type DecoratedEmbed struct {
	Inner `json:",embed,omitzero"`
}
```

- [ ] **Step 2: Write the failing test rows**

```go
		"NamedEmbed":     {"FRG2008", "embed", "on its own"},
		"DecoratedEmbed": {"FRG2008", "embed", "on its own"},
```

- [ ] **Step 3: Run them to make sure they fail**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestWhatACodecRefusesToWrite/(NamedEmbed|DecoratedEmbed)' -v`
Expected: FAIL for both with `a codec was written for ...`

- [ ] **Step 4: Implement the check**

```go
// impureEmbed reports an embed written with a name or with company.
//
// embed says the field's members are the enclosing struct's members. A name
// would be the name of an object that is never written, and an option deciding
// whether to write that object decides about the same nothing. The standard
// library refuses both rather than picking one to ignore, and so does this.
func (p *planner) impureEmbed(field plugin.Field) (plugin.Diagnostic, bool) {
	tag, ok := field.Tag(jsonKey)
	if !ok || !tag.Has(optionEmbed) {
		return plugin.Diagnostic{}, false
	}

	switch {
	case tag.Name != "":
		return plugin.New(codeTagOption, field.Pos,
			"%s is embedded under the name %q, which nothing is written under",
			field.Name, tag.Name).
			WithHint("%s", impureEmbedHint), true
	case len(tag.Options) > 1:
		return plugin.New(codeTagOption, field.Pos,
			"%s is embedded and tagged %q as well", field.Name, tag.Raw).
			WithHint("%s", impureEmbedHint), true
	}

	return plugin.Diagnostic{}, false
}

const impureEmbedHint = "embed promotes a field's members into the enclosing object, " +
	"so it is written on its own: write json:\",embed\""
```

Call it in `unsupported` after `repeatedOption`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestWhatACodecRefusesToWrite/(NamedEmbed|DecoratedEmbed)' -v`
Expected: PASS for both

- [ ] **Step 6: Run the whole layer**

Run: `go test ./internal/layers/jsoncodec/`
Expected: ok

- [ ] **Step 7: Commit**

```bash
git add internal/layers/jsoncodec/plan.go \
        internal/layers/jsoncodec/refused_test.go \
        internal/layers/jsoncodec/testdata/codec/refused/refused.go
git commit -m "fix: refuse an embed option written with a name or with company"
```

---

### Task 4: Refuse an unexported field carrying a json tag

**Files:**
- Modify: `internal/layers/jsoncodec/plan.go` — register `codeTaggedUnexported`
  at `:21-25`; add `(*planner).taggedUnexported`
- Modify: `internal/layers/jsoncodec/plan.go:602` — call it in the same place
  `unsupported` is called, but for **every** field rather than only exported
  ones
- Modify: `internal/layers/jsoncodec/testdata/codec/refused/refused.go` — add
  `TaggedUnexported`
- Test: `internal/layers/jsoncodec/refused_test.go` — one table row

**Interfaces:**
- Consumes: `plugin.Field.Exported bool`; `plugin.Tag.Ignored bool`;
  `plugin.Tag.Raw string`.
- Produces: `codeTaggedUnexported = plugin.Register(2031, "an unexported field carries a json tag")`
  and `func (p *planner) taggedUnexported(field plugin.Field) (plugin.Diagnostic, bool)`.

`encoding/json/v2` refuses this outright: "unexported Go struct field %s cannot
have non-ignored `json:%q` tag" (`v2/fields.go:427-433`). `json:"-"` on an
unexported field stays legal, because that tag agrees with what happens anyway.
Forge drops the field silently at `name.go:29-31`, so an author who tags an
unexported field gets a codec that ignores the tag and a document missing a
member, with nothing said.

**Read this before implementing:** `wireName` returns `("", false)` for an
unexported field *before* looking at the tag (`name.go:29-31`), and the caller
at `plan.go:602` may skip unexported fields entirely. Find where unexported
fields are dropped and raise this diagnostic there, not in `wireName` — that
function answers what a field is called, and a refusal is a different question.

- [ ] **Step 1: Add the fixture**

```go
// TaggedUnexported tags a field generated code cannot read. The tag asks for a
// member the codec will never write, and an author who wrote it is describing
// a wire format they will not get.
type TaggedUnexported struct {
	Exported   int `json:"exported"`
	unexported int `json:"unexported"`
}
```

Note: `unexported` will draw an unused-field warning from some linters. The
fixture package is generated-code input rather than library code; check whether
`testdata` is excluded from lint before adding a nolint directive, and prefer
exclusion to a directive.

- [ ] **Step 2: Write the failing test row**

```go
		"TaggedUnexported": {"FRG2031", "unexported", "json:\"-\""},
```

- [ ] **Step 3: Run it to make sure it fails**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestWhatACodecRefusesToWrite/TaggedUnexported' -v`
Expected: FAIL with `a codec was written for TaggedUnexported`

- [ ] **Step 4: Register the code**

At `internal/layers/jsoncodec/plan.go:21-25`, add to the existing `var` block:

```go
	codeTaggedUnexported = plugin.Register(2031, "an unexported field carries a json tag")
```

- [ ] **Step 5: Implement the check**

```go
// taggedUnexported reports a tag on a field generated code cannot read.
//
// The field is left out either way, so the tag describes a member that will
// never be written. Left alone it reads as an instruction that was followed,
// which is worse than an error: the author's own source says the member is
// there. json:"-" is the one tag that stays legal, because it asks for exactly
// what happens.
func (p *planner) taggedUnexported(field plugin.Field) (plugin.Diagnostic, bool) {
	if field.Exported {
		return plugin.Diagnostic{}, false
	}

	tag, ok := field.Tag(jsonKey)
	if !ok || tag.Ignored {
		return plugin.Diagnostic{}, false
	}

	return plugin.New(codeTaggedUnexported, field.Pos,
		"%s is unexported and carries %s, which asks for a member nothing writes",
		field.Name, tag.String()).
		WithHint("%s", taggedUnexportedHint), true
}

const taggedUnexportedHint = "generated code cannot read an unexported field from outside " +
	"its package; export the field, or write json:\"-\" to say it is left out on purpose"
```

- [ ] **Step 6: Call it for every field**

At `plan.go:602`, `unsupported` is consulted for a field being turned into a
member. An unexported field may not reach that point. Add the call where the
struct's fields are walked — `(*planner).member`, which begins immediately after
`flatten` at roughly `:600` — so that it runs before the field is dropped:

```go
	if wrong, found := p.taggedUnexported(field); found {
		p.diags.Add(wrong)
		return nil
	}
```

Verify by reading `member` in full first. If it already returns early for an
unexported field, this call goes above that return.

- [ ] **Step 7: Run the test to verify it passes**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestWhatACodecRefusesToWrite/TaggedUnexported' -v`
Expected: PASS

- [ ] **Step 8: Prove `json:"-"` is still legal**

Add to `refused_test.go` a case in whichever test asserts a codec **is**
written — `TestTheBoundaryIsWhatMakesTheDifference` at `:104` is the existing
home for "this one is fine" — with a fixture:

```go
// IgnoredUnexported says out loud that an unexported field is left out, which
// is the one tag on one that is not a mistake.
type IgnoredUnexported struct {
	Exported   int `json:"exported"`
	unexported int `json:"-"`
}
```

Run: `go test ./internal/layers/jsoncodec/ -run TestTheBoundaryIsWhatMakesTheDifference -v`
Expected: PASS

- [ ] **Step 9: Run the whole layer**

Run: `go test ./internal/layers/jsoncodec/`
Expected: ok

- [ ] **Step 10: Commit**

```bash
git add internal/layers/jsoncodec/plan.go \
        internal/layers/jsoncodec/refused_test.go \
        internal/layers/jsoncodec/testdata/codec/refused/refused.go
git commit -m "fix: refuse a json tag on a field generated code cannot read"
```

---

### Task 5: Refuse a struct with no exported fields

**Files:**
- Modify: `internal/layers/jsoncodec/plan.go` — register `codeNoMembers`; add
  the check where a struct's members are settled
- Modify: `internal/layers/jsoncodec/testdata/codec/refused/refused.go` — add
  `NoMembers`
- Test: `internal/layers/jsoncodec/refused_test.go` — one table row

**Interfaces:**
- Consumes: `(*planner).flatten` (`plan.go:580`), which returns `[]member`;
  `plugin.Struct.Fields`.
- Produces: `codeNoMembers = plugin.Register(2032, "a struct has no members to write")`.

`encoding/json/v2` refuses a struct with no exported fields
(`v2/fields.go:78, 271-282`), suppressed by any json tag on any field or by the
struct having no fields at all. Forge has no such check, so such a type almost
certainly yields `{}` — which round-trips through itself perfectly and is
therefore invisible to every test that only round-trips.

**Confirm the current behaviour before writing the check.** Build a throwaway
fixture with `type Hidden struct{ a int }` and generate for it; record what comes
out. If it already refuses for another reason, this task narrows to a test
asserting that, and the code registration is not needed.

- [ ] **Step 1: Add the fixture**

```go
// NoMembers has fields, and none of them can be written. A codec for it would
// be a function that writes {} — which round-trips through itself and so is
// invisible to any test that only reads back what it wrote.
type NoMembers struct {
	hidden  int
	private string
}
```

- [ ] **Step 2: Write the failing test row**

```go
		"NoMembers": {"FRG2032", "no members", "export"},
```

- [ ] **Step 3: Run it to make sure it fails**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestWhatACodecRefusesToWrite/NoMembers' -v`
Expected: FAIL with `a codec was written for NoMembers`

- [ ] **Step 4: Register the code**

```go
	codeNoMembers = plugin.Register(2032, "a struct has no members to write")
```

- [ ] **Step 5: Implement the check**

Where the subject's members are settled — after `flatten` returns for the
subject itself, not for each reached struct — add:

```go
// A struct with nothing to write is refused rather than written as an empty
// object. The standard library refuses it too, and for the reason that matters
// here: {} read back into the same type gives the same value, so a codec that
// wrote it would satisfy every round trip while carrying none of the value.
//
// A field carrying a json tag is a field the author meant to be written, so a
// struct with one of those is a different complaint and is made elsewhere.
if len(members) == 0 && len(held.Fields) > 0 && !anyTagged(held) {
	p.diags.Add(plugin.New(codeNoMembers, where.pos,
		"%s has no members to write", out.spelled.Text).
		WithHint("%s", noMembersHint))
	return nil
}

const noMembersHint = "export a field, or give one a json tag, " +
	"so that there is something for the document to carry"
```

and the helper:

```go
// anyTagged reports whether any of a struct's fields carries a json tag, which
// is what the standard library takes as the author having meant something by a
// struct that otherwise has nothing to write.
func anyTagged(held *plugin.Struct) bool {
	for _, field := range held.Fields {
		if _, ok := field.Tag(jsonKey); ok {
			return true
		}
	}
	return false
}
```

Adjust the variable names to match the surrounding function; `where`, `out` and
`held` are the names used at `plan.go:442-455`.

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestWhatACodecRefusesToWrite/NoMembers' -v`
Expected: PASS

- [ ] **Step 7: Run the whole layer**

Run: `go test ./internal/layers/jsoncodec/`
Expected: ok

- [ ] **Step 8: Commit**

```bash
git add internal/layers/jsoncodec/plan.go \
        internal/layers/jsoncodec/refused_test.go \
        internal/layers/jsoncodec/testdata/codec/refused/refused.go
git commit -m "fix: refuse a struct whose codec would write nothing but braces"
```

---

### Task 6: Take the disjunction of omitzero and omitempty

**Files:**
- Modify: `internal/layers/jsoncodec/encode.go:176-186` — `(*writer).omitted`
- Test: `internal/layers/jsoncodec/codec_test.go` — extend the agreement
  fixture, or add a case to the emitted-text test if agreement cannot reach it

**Interfaces:**
- Consumes: `(*writer).nonZero(held string, of *form) (string, bool)` and
  `(*writer).nonEmpty(held string, of *form) when` (`encode.go` immediately
  below `omitted`); the `when` type with fields `cond string` and `can bool`.
- Produces: no new names — `omitted` keeps its signature.

This is the one defect in Stage 0 that is not a missing diagnostic. `omitted` is
a `switch` whose first arm wins:

```go
	switch {
	case one.omitZero:
		cond, can := w.nonZero(held, &one.of)
		return when{cond: cond, can: can}
	case one.omitEmpty:
		return w.nonEmpty(held, &one.of)
	default:
		return always
	}
```

A member tagged `json:"s,omitzero,omitempty"` therefore gets the zero test only.
The standard library applies `omitzero` before marshalling
(`v2/arshal_default.go:1154`) and `omitempty` after (`:1174`, `:1235`), so the
member is omitted when **either** says so. For a nil slice the two agree; for an
empty non-nil slice they do not, and forge writes `"s":[]` where the standard
library omits the member.

- [ ] **Step 1: Write the failing test**

Add to the agreement fixture at
`internal/layers/jsoncodec/testdata/agreement.go.txt` a subject carrying both
options on a slice, and a value whose slice is empty but not nil:

```go
// BothOmissions carries both options on one member. They agree about a nil
// slice and disagree about an empty one, which is the case that tells whether
// the two are being read as alternatives or as a disjunction.
type BothOmissions struct {
	S []string `json:"s,omitzero,omitempty"`
}
```

with the value `BothOmissions{S: []string{}}` added to whatever list the
agreement harness walks. Read the top of `agreement.go.txt` to find the shape it
expects — the harness marshals each value through both the generated codec and
the reflective twin and requires the bytes to match.

- [ ] **Step 2: Run it to make sure it fails**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestCodec' -v`
Expected: FAIL, with the generated codec producing `{"s":[]}` and the reflective
twin producing `{}`

- [ ] **Step 3: Implement the disjunction**

```go
// omitted returns the condition under which a member is written at all.
//
// The two options are not alternatives and the standard library does not treat
// them as such: omitzero is asked before the value is written and omitempty
// after, so a member carrying both is left out when either says to leave it
// out. They agree about a nil slice and disagree about an empty one, and taking
// the first arm of a switch would write "s":[] where the standard library
// writes no member at all.
func (w *writer) omitted(one member, held string) when {
	zero, zeroCan := when{}, true
	if one.omitZero {
		cond, can := w.nonZero(held, &one.of)
		zero, zeroCan = when{cond: cond, can: can}, can
	}

	empty := when{}
	if one.omitEmpty {
		empty = w.nonEmpty(held, &one.of)
	}

	switch {
	case one.omitZero && one.omitEmpty:
		return both(zero, empty)
	case one.omitZero:
		return when{cond: zero.cond, can: zeroCan}
	case one.omitEmpty:
		return empty
	default:
		return always
	}
}

// both returns the condition that a member survives two tests, which is what a
// member carrying both options has to do to be written.
//
// A condition that could not be written at all makes the pair unwritable: a
// member whose emptiness this codec cannot test is one whose omission it cannot
// promise, and promising it anyway is what the refusal at FRG2010 exists to
// prevent.
func both(zero, empty when) when {
	if !zero.can || !empty.can {
		return when{can: false}
	}
	return when{cond: "(" + zero.cond + ") && (" + empty.cond + ")", can: true}
}
```

Read the `when` type and `always` before writing this; the field names above are
from `encode.go:176-186` and the `can` semantics must match `nonEmpty`'s.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestCodec' -v`
Expected: PASS

- [ ] **Step 5: Regenerate the goldens and the example**

Run:
```bash
go test ./internal/generate -update
go test ./internal/layers/guarded -update
go test ./internal/racetest -update
make example
```
Expected: any diff is confined to members carrying both options.

- [ ] **Step 6: Run the whole suite**

Run: `go test ./...`
Expected: ok

- [ ] **Step 7: Commit**

```bash
git add internal/layers/jsoncodec/ examples/ internal/generate/testdata/ \
        internal/layers/guarded/testdata/ internal/racetest/testdata/
git commit -m "fix: leave a member out when either omission asks for it"
```

---

### Task 7: Refuse time.Duration with no format

**Files:**
- Modify: `internal/layers/jsoncodec/plan.go` — register `codeNoRepresentation`;
  add the check in the type classification near `:377-381`
- Modify: `internal/layers/jsoncodec/testdata/codec/refused/refused.go` — add
  `Timed`
- Test: `internal/layers/jsoncodec/refused_test.go` — one table row

**Interfaces:**
- Consumes: `(*planner).refuse` at `:545` for the message shape; the type
  classification switch that reaches `*types.Basic` at `:377-381`; `go/types`
  for type identity.
- Produces: `codeNoRepresentation = plugin.Register(2033, "a type has no default JSON representation")`.

Verified: `json.Marshal` on a struct with a bare `time.Duration` field errors
with `json: cannot marshal from Go time.Duration within "/D": no default
representation` (`v2/arshal_time.go:55-62`). It refuses to choose between
nanoseconds and `"1h30m"`. Forge writes a bare nanosecond integer, because
`time.Duration` has no marshal or text methods, so `owned()` declines and the
walk reaches `*types.Basic` int64 and takes `writtenInt` (`plan.go:377-380`,
`:515-531`). That silently matches v1's `FormatDurationAsNano` and disagrees with
v2.

`time.Duration` must be recognised by **type identity**, exactly as the standard
library recognises it (`v2/arshal_time.go:42, 134`), not by underlying kind —
every `int64` must not become a duration.

Zero fixtures in the repository use `time.Duration`, verified by
`grep -rn 'time\.Duration' internal/layers/jsoncodec examples driver`, so this
breaks nothing that exists.

- [ ] **Step 1: Add the fixture**

```go
// Timed holds a duration with no format asked for. The standard library
// refuses to choose between a count of nanoseconds and a string like "1h30m",
// and a codec that chose quietly would put one of them on a wire the other end
// reads the other way.
type Timed struct {
	For time.Duration `json:"for"`
}
```

with `"time"` added to the fixture file's imports.

- [ ] **Step 2: Write the failing test row**

```go
		"Timed": {"FRG2033", "time.Duration", "format:"},
```

- [ ] **Step 3: Run it to make sure it fails**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestWhatACodecRefusesToWrite/Timed' -v`
Expected: FAIL with `a codec was written for Timed` — and, before the fix, the
generated codec writes `{"for":5400000000000}`

- [ ] **Step 4: Register the code**

```go
	codeNoRepresentation = plugin.Register(2033, "a type has no default JSON representation")
```

- [ ] **Step 5: Implement the check**

In the classification walk, before the `*types.Basic` arm reaches `writtenInt`:

```go
// A duration is refused rather than written as the integer underneath it.
// int64 nanoseconds is what v1 wrote and what this layer wrote by accident;
// encoding/json/v2 refuses to guess between that and "1h30m", and a wire
// format guessed on the author's behalf is one the far end reads differently.
//
// By identity and not by kind: every int64 is not a duration.
if isDuration(where.typ) {
	p.diags.Add(plugin.New(codeNoRepresentation, where.pos,
		"%s is a time.Duration, which has no one JSON form", where.name).
		WithHint("%s", durationHint))
	return
}
```

with:

```go
// isDuration reports whether a type is time.Duration itself.
func isDuration(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Name() == "Duration" &&
		obj.Pkg() != nil && obj.Pkg().Path() == "time"
}

const durationHint = "say which form the document carries: format:units for \"1h30m\", " +
	"format:sec, format:milli, format:micro or format:nano for a number"
```

Adjust `where.typ`, `where.pos` and `where.name` to the actual field names on
`blamed` — read `plan.go:275-300` for how `blamed` is built and what it carries.
If `blamed` does not carry the type, pass it separately.

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestWhatACodecRefusesToWrite/Timed' -v`
Expected: PASS

- [ ] **Step 7: Check that time.Time is untouched**

`time.Time` is already refused as a foreign struct (`FRG2007`, the `Foreign`
fixture). This must not change.

Run: `go test ./internal/layers/jsoncodec/ -run 'TestWhatACodecRefusesToWrite/Foreign' -v`
Expected: PASS, still reporting `FRG2007`

- [ ] **Step 8: Run the whole suite**

Run: `go test ./...`
Expected: ok

- [ ] **Step 9: Commit**

```bash
git add internal/layers/jsoncodec/plan.go \
        internal/layers/jsoncodec/refused_test.go \
        internal/layers/jsoncodec/testdata/codec/refused/refused.go
git commit -m "fix: refuse a duration rather than guess what its document says"
```

---

### Task 8: Pin embedding with a fixture

**Files:**
- Modify: `internal/layers/jsoncodec/testdata/codec/model/person.go` — add an
  embedding subject
- Test: `internal/layers/jsoncodec/codec_test.go` and
  `testdata/agreement.go.txt` — add it to the agreement set

**Interfaces:**
- Consumes: the agreement harness in `testdata/agreement.go.txt`, whose `agree`
  and `readAlike` functions marshal a value through the generated codec and the
  reflective twin and require the bytes to match.
- Produces: no new names.

`grep -rn ',embed' internal/layers/jsoncodec/testdata` returns nothing: the
option is implemented (`plan.go:640-642, 655-688`) and no test proves it. Tasks
3 and 4 both add refusals near it, and a refusal with no matching acceptance
test is one bug away from refusing everything.

- [ ] **Step 1: Write the failing test**

Add to the fixture package:

```go
// Address is embedded rather than held, so its members belong to whatever
// embeds it.
type Address struct {
	City    string `json:"city"`
	Country string `json:"country"`
}

// Located embeds a struct explicitly. The members come out where the embedded
// field is, which is the order the standard library writes and the reason this
// is checked by agreement rather than by reading the output.
type Located struct {
	ID int `json:"id"`
	Address `json:",embed"`
	Note string `json:"note"`
}

// LocatedByAnonymity embeds without the option, which Go's own anonymity
// implies and the standard library treats the same way.
type LocatedByAnonymity struct {
	ID int `json:"id"`
	Address
	Note string `json:"note"`
}
```

and both types, populated, to the agreement set.

- [ ] **Step 2: Run it and see what happens**

Run: `go test ./internal/layers/jsoncodec/ -run 'TestCodec' -v`
Expected: PASS if embedding is correct, or a byte difference naming exactly what
is wrong. Either outcome is information; a failure here is a bug this task
uncovered rather than a bug this task introduced, and it gets its own commit.

- [ ] **Step 3: Regenerate goldens if the fixture is in a golden package**

Run: `go test ./internal/generate -update && go test ./internal/layers/jsoncodec -update`
Expected: additions only.

- [ ] **Step 4: Run the whole suite**

Run: `go test ./...`
Expected: ok

- [ ] **Step 5: Commit**

```bash
git add internal/layers/jsoncodec/
git commit -m "test: hold embedded members to the order the standard library writes"
```

---

### Task 9: Close the stage

**Files:**
- Modify: `internal/layers/jsoncodec/doc.go` — the paragraph listing refused tag
  options
- Modify: `docs/` — wherever diagnostic codes are indexed, if such an index
  exists

**Interfaces:**
- Consumes: everything above.
- Produces: a shippable change.

- [ ] **Step 1: Find the code index**

Run: `grep -rln "FRG2007\|FRG2010" --include='*.md' --include='*.go' . | grep -v _test`
Expected: the files that list codes. `diag.Registered()` returns every code in
ascending order and something may render it; if there is a generated index, run
its generator.

- [ ] **Step 2: Update doc.go**

The paragraph at `doc.go:105-115` says a tag option this layer does not generate
for is refused, and names "a format, a loose name match, a number asked to be
written as a string". Add the six new refusals to that sentence's list. Do not
claim anything about speed or about the emitter — those sentences are Stage 1's
to rewrite.

- [ ] **Step 3: Run the full gate**

Run: `make check`
Expected: pass. Note that `./scripts/bench.sh` fails today on `Validate` and
`ValidateByHandWithThePattern` at 1 B/op against a budget of 0; that failure
predates this work and is not a regression.

- [ ] **Step 4: Run the race and fuzz gates**

Run: `make race && FUZZ_TIME=30s ./scripts/fuzz.sh`
Expected: pass.

- [ ] **Step 5: Commit and open the pull request**

```bash
git add -A
git commit -m "docs: say which json tag options this codec refuses"
gh pr create --title "Refuse every json tag the standard library refuses" \
  --body "Stage 0 of docs/superpowers/specs/2026-09-03-json-codec-design.md.

Eight defects in tag handling, seven of them silent:

- an option that is omitempty spelled another way was ignored
- an option written twice took its first occurrence
- case:ignore beside case:strict was read as one of them
- embed written with a name or another option discarded both
- an unexported field carrying a json tag was dropped without a word
- a struct with nothing to write got a codec that writes {}
- omitzero and omitempty together applied only the first
- time.Duration was written as bare nanoseconds, which v2 refuses to guess at

Each is a hard break for an author: a package that generates today refuses
tomorrow. internal/diag has no warning tier, which is why this lands on its own
rather than inside the emitter rewrite that follows."
```

---

## Self-Review

**Spec coverage.** Section 12's Stage 0 lists eight defects and one missing
fixture. Tasks 1, 2, 3, 4, 5, 6, 7 cover the eight; Task 8 covers the fixture.
Task 0 covers the repository fault that blocks every commit step. Task 9 closes
the stage. No Stage 0 requirement is unassigned.

**Placeholder scan.** Every step carries the code or the exact command.
Three tasks — 4, 5 and 7 — contain an instruction to read a specific function
before writing, because the variable names on `blamed` and the early-return
structure of `member` could not be confirmed without reading them in full, and
guessing a name would be worse than saying which line to look at. Those are
reading instructions with the line number attached, not deferred decisions.

**Type consistency.** `normalizedOption` is defined in Task 1 and consumed in
Task 2. `knownOptions` likewise. `codeTaggedUnexported` (2031),
`codeNoMembers` (2032) and `codeNoRepresentation` (2033) are registered once
each, in Tasks 4, 5 and 7, and the numbers do not collide with 2001–2030.
`(*writer).omitted` keeps its signature in Task 6; `both` and `when` agree on
`cond string` and `can bool`.

**One risk worth naming.** Tasks 1, 2 and 3 all add checks to
`(*planner).unsupported`, and their order matters: `misspelledOption`,
`repeatedOption`, `impureEmbed`, then the existing `format`, `case` and `string`
checks. A contradiction between `case:ignore` and `case:strict` reported as an
unsupported `case` is technically a refusal and would pass a laxer test; the
table rows assert on the message so that the order is held.
