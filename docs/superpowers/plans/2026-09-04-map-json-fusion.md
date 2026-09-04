# Map[S, Json[T]] Fusion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `type UserWire Map[User, Json[Person]]` generates, beside the stage-1
constructor and Person's own codec, package functions that write Person's JSON
document straight from a User — byte-identical to construct-then-encode, with
no Person allocated.

**Architecture:** The codec's append emitter already writes each member from a
read expression (today always `v.<path>`); the mapping's settle table says
what each member holds before any T exists. The fusion parameterizes the
emitter's top-level reads and hands it the mapping's bindings through one new
internal entry point, `jsoncodec.Fused`. Composition is relaxed to admit
exactly the stack `[Map, Json]`.

**Tech Stack:** Go 1.27, go/types, the existing text-assembly emitters, the
jsonwire runtime, the temp-module compile-and-run harness.

**Spec:** `docs/superpowers/specs/2026-09-03-map-json-fusion-design.md`

## Global Constraints

- The one admitted company for a bridge is exactly `[Map, Json]`; everything
  else stays FRG1009. No new diagnostic codes.
- Fused names: `Append<T>JSONFrom<S>(dst []byte, src *<S>) ([]byte, error)`
  and `Write<T>JSONFrom<S>(w io.Writer, src *<S>) (int64, error)`; interface
  sources take `src <S>` bare.
- Byte equality gate: fused output equals `TFromS(src)` then `AppendJSON(nil)`
  — bytes and error verdict both.
- Zero-alloc gate: the fused append allocates nothing beyond buffer growth,
  held by a `scripts/budget.txt` row (`0` allocs with a warmed buffer).
- Repo gates: `make check` (gofumpt, vet incl. `-tags forgespec`, golangci,
  cover ≥ 90%, size), `make fresh` clean. `make example` is regenerated and
  committed in Task 6 (the example gains a fused declaration on purpose).
- Every identifier a generated body binds goes through the emitting package's
  locals allocation (`naming`/`w.n`), never a bare constant.

---

### Task 1: Composition admits exactly [Map, Json]

**Files:**
- Modify: `internal/compose/rules.go` (the `bridges` rule, ~line 250)
- Modify: `internal/model/kind.go` (KindBridge's comment: "composes with
  nothing else" → "composes with the JSON codec and nothing else")
- Modify: `internal/layers/mapping/doc.go` and `forge.go`'s `Map` doc footer
  if they say "composes with nothing" (grep for the phrase; update wording)
- Test: `internal/compose/rules_test.go`

**Interfaces:**
- Produces: `compose.Compose` accepts a declaration whose stack is exactly
  `[Map, Json]` (by origin: `{model.MarkerPkg, "Map"}` outermost,
  `{model.MarkerPkg, "Json"}` innermost) with no diagnostics; every other
  arrangement containing a bridge still reports FRG1009.

- [ ] **Step 1: Write the failing tests.** Append to
  `internal/compose/rules_test.go` (the real registry now claims both markers,
  so no stub is needed for the passing case; the failing cases reuse the
  `spanning` stub already in the file):

```go
// The one company a bridge admits: the JSON codec directly beneath it, which
// is the fusion the bridge exists to feed. Anything else is still refused.
func TestABridgeAdmitsTheCodecAlone(t *testing.T) {
	decl := written(model.FormSpec, "Map", "Json")

	held, diags := compose.Compose(decl, catalog())
	if !diags.Empty() {
		t.Fatalf("the fused stack was refused:\n%s", diags.Render())
	}
	if got := named(held); len(got) != 2 || got[0] != "Map" || got[1] != "Json" {
		t.Errorf("composed stack = %v, want [Map Json]", got)
	}
}

// The admission is exact: the codec under a foreign bridge, another element
// under Map, or Json written over Map are all still a bridge with company.
func TestWhatABridgeStillRefuses(t *testing.T) {
	registry := layers.Builtins()
	registry.MustRegister(spanning{})

	for name, stack := range map[string][]string{
		"another element beneath": {"Map", "Validate"},
		"the codec over a bridge": {"Json", "Map"},
		"a storage beneath":       {"Map", "Ring"},
		"the codec under a foreign bridge": {"Spans", "Json"},
		"three deep": {"Map", "Json", "Ring"},
	} {
		t.Run(name, func(t *testing.T) {
			_, diags := compose.Compose(written(model.FormSpec, stack...),
				compose.Catalog{Registry: registry, DefaultStorage: layers.DefaultStorage()})
			if !strings.Contains(diags.Render(), "FRG1009") {
				t.Errorf("%v was not refused:\n%s", stack, diags.Render())
			}
		})
	}
}
```

Note: `{"Map", "Json", "Ring"}` — check whether an earlier rule (elements)
also fires; the assertion only requires FRG1009 to be among the output.
`{"Json", "Map"}` puts the bridge innermost — the subject beneath a bridge is
fine, the company above it is not.

- [ ] **Step 2: Run to verify failure.**

Run: `go test ./internal/compose/ -run 'Bridge' -count=1`
Expected: `TestABridgeAdmitsTheCodecAlone` FAILS with FRG1009 in the render;
the refusal table may partially pass (most rows already refuse).

- [ ] **Step 3: Implement.** In `internal/compose/rules.go`, replace the
  `bridges` function body:

```go
// bridges holds a bridge to the one company it admits: the JSON codec
// directly beneath it, which is the fusion the bridge exists to feed.
//
// A bridge reads one type and writes about another; there is no stream for a
// storage to hold or a refiner to query, so any other company describes
// machinery with nothing to attach to. The admission is by origin rather than
// by kind, because the fusion is written for the codec: another element layer
// beneath a bridge would compose into nothing the bridge knows how to feed.
func bridges(stack []model.LayerRef, _ []layer.Layer, decl Declaration, layout model.Layout, diags *diag.Set) {
	if len(stack) < 2 {
		return
	}

	json := model.TypeRef{Pkg: model.MarkerPkg, Name: "Json"}
	for i, ref := range stack {
		if ref.Kind != model.KindBridge {
			continue
		}
		if i == 0 && len(stack) == 2 && stack[1].Origin.Origin() == json &&
			ref.Origin.Origin() == (model.TypeRef{Pkg: model.MarkerPkg, Name: "Map"}) {
			continue
		}

		at(diags, codeBridgeAlone, decl, layout, i,
			"declare the bridge on its own, or over the codec: type X Map[Source, Target] or Map[Source, Json[Target]]",
			"%s is a bridge and composes with Json beneath it or nothing", ref.Origin.Name)
	}
}
```

(Verify `TypeRef.Origin()` exists — the registry reduces instantiations with
it; if origins in a composed stack are already reduced, compare directly.)
Update `codeBridgeAlone`'s registered summary if the wording no longer fits —
`diag.Register(1009, "a bridge stands alone over its two types")` may stay;
the docs row in `docs/diagnostics.md` stays as-is unless the summary changes.
Update KindBridge's doc comment in `internal/model/kind.go` and the "composes
with nothing" phrasing in `internal/layers/mapping/mapping.go` (`Kind`'s
comment), `internal/layers/mapping/doc.go`, and `forge.go`'s `Map` footer to
say the codec is the one exception.

- [ ] **Step 4: Run** `go test ./internal/compose/ ./internal/model/ . -count=1` → PASS.
- [ ] **Step 5: Commit** `git commit -m "feat: let a bridge stand over the codec it feeds"`.

---

### Task 2: The codec's reads become a parameter (no output change)

**Files:**
- Modify: `internal/layers/jsoncodec/encode.go` (appendBody ~line 81,
  writeMember ~line 145, and the `w.n("v")+"."+one.path` sites; grep
  `+"."+one.path` and `guard.path` first)
- Test: the existing suite is the test — this task changes no output byte

**Interfaces:**
- Produces: the `writer` gains a read resolver so a later task can swap the
  top-level member reads. Exact shape: a field on `writer`,
  `reads func(path string) string`, nil meaning the default
  `w.n("v") + "." + path`. A helper method:

```go
// read spells where a member's value comes from. The codec reads the value
// being encoded; the fused writer reads the mapping's bindings instead, and
// this is the seam between the two.
func (w *writer) read(path string) string {
	if w.reads != nil {
		return w.reads(path)
	}
	return w.n("v") + "." + path
}
```

- [ ] **Step 1: Introduce `read` and route every top-level member read
  through it.** In `appendBody`: `plans[i] = w.omitted(one, w.read(one.path))`.
  In `writeMember`: the two `w.appendValue(v+"."+one.path, ...)` calls become
  `w.appendValue(w.read(one.path), ...)`, and the guard line
  `w.line("if %s.%s != nil {", v, guard.path)` becomes
  `w.line("if %s != nil {", w.read(guard.path))`. Leave every *nested* site
  (the `held+"."+...` forms at lines ~483, ~491, ~648, ~656, ~941) alone —
  they read locals the body itself bound, not the encoded value. Read the
  whole of encode.go's top-level entry (`appendBody` callers) to confirm no
  other `v.`-rooted read exists at the top level; `w.n("v")` used for the
  receiver declaration itself stays.

- [ ] **Step 2: Prove nothing changed.**

Run: `go test ./internal/layers/jsoncodec/ ./internal/generate/ -count=1 && make fresh`
Expected: PASS, and `make fresh` reports every declaration up to date (the
refactor emitted identical bytes). If any golden or freshness check moves,
the refactor changed behavior — fix it, do not regenerate.

- [ ] **Step 3: Commit** `git commit -m "refactor: let the codec say where a member's value comes from"`.

---

### Task 3: jsoncodec.Fused

**Files:**
- Create: `internal/layers/jsoncodec/fused.go`
- Test: `internal/layers/jsoncodec/fused_test.go` (package `jsoncodec_test`,
  reusing `loadFixture`/`named` from `fixture_test.go` and the model fixture's
  `Person`)

**Interfaces:**
- Consumes: Task 2's `writer.reads`; the codec's own `planner`/form machinery
  (read `json.go`'s `Generate` to see how a subject's plan and forms are
  built — reuse the same calls, do not re-derive).
- Produces:

```go
// Fused returns the two package functions that write the subject's document
// from a source, reading each top-level member from the expression the
// mapping settled it to instead of from a held value.
//
// reads is called once with the source parameter's allocated name and returns
// one expression per top-level field of the subject, keyed by Go field name —
// every field, ignored ones included (spelled as the zero they hold). The
// nested codecs and the wire runtime are not emitted here: the Json layer of
// the same declaration provides them into the same package.
func Fused(ctx *plugin.Context, reads func(src string) map[string]string) (plugin.Unit, error)
```

Function names inside the unit: `Append<T>JSONFrom<S>` / `Write<T>JSONFrom<S>`
via `plugin.Upper` on `ctx.Model.Subject.Named.Obj().Name()` and the source's
`types.Named` name, with the literal string `JSON`. Parameter: `src *<S>` for
a struct source, `src <S>` for an interface — derive from
`ctx.Model.Source.Underlying()` exactly as mapping's `body()` does.

- [ ] **Step 1: Write the failing test:**

```go
// The fused writers read the mapping's expressions where the codec reads the
// held value, and everything else — names, order, escaping — is the codec's.
func TestFusedWritesFromTheGivenReads(t *testing.T) {
	loaded := loadFixture(t)
	builder := subject.New(subject.Config{Fset: loaded.Fset, Owned: loaded.Owned(), Docs: loaded.FieldDocs()})

	pkg, _ := loaded.Package(modelPkg)
	person := named(t, loaded, "Person")
	built, problems := builder.Build(person, subject.Site{})
	if !problems.Empty() {
		t.Fatalf("modelling Person: %s", problems.Render())
	}

	// A source type stands in through go/types; only its name and kind reach
	// the signature.
	srcObj := types.NewTypeName(token.NoPos, pkg.Types, "Feed", nil)
	source := types.NewNamed(srcObj, types.NewStruct(nil, nil), nil)

	unit, err := jsoncodec.Fused(&plugin.Context{
		Model: &plugin.Model{
			Name: "Wire", Form: plugin.FormSpec, Subject: built, Source: source,
			Pkg: pkg, Pos: token.Position{Filename: "spec.go"},
		},
	}, func(src string) map[string]string {
		out := make(map[string]string)
		for _, field := range built.Fields {
			out[field.Name] = src + ".X" + field.Name + "()"
		}
		return out
	})
	if err != nil {
		t.Fatalf("fusing: %v", err)
	}

	text := rendered(t, unit) // helper: emit.File{Package:"model", Sections:{...}}.Render()
	for _, want := range []string{
		"func AppendPersonJSONFromFeed(dst []byte, src *Feed) ([]byte, error)",
		"func WritePersonJSONFromFeed(w io.Writer, src *Feed) (int64, error)",
		"src.XName()", // the read the callback supplied, in the body
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the fused unit does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "v.Name") {
		t.Errorf("a held-value read survived into the fused body:\n%s", text)
	}
}
```

(Adjust the fixture subject if `Person` in the codec fixture has members whose
reads complicate the substring assertions — any struct subject from the
fixture works; read `fixture_test.go` for what `modelPkg` declares. Write the
small `rendered` helper in the test file.)

- [ ] **Step 2: Run to verify failure** (`go test ./internal/layers/jsoncodec/ -run Fused -count=1` — undefined: jsoncodec.Fused).

- [ ] **Step 3: Implement `fused.go`.** Shape:

```go
func Fused(ctx *plugin.Context, reads func(src string) map[string]string) (plugin.Unit, error) {
	// Guards exactly as Generate's: nil ctx/Model/Subject/Source is forge
	// calling itself wrongly.

	// 1. Plan the subject exactly as Generate does (same planner entry, same
	//    refusal propagation) — the fused writer refuses whatever the codec
	//    refuses, in the codec's own words.

	// 2. Build the writer with the same locals seeding Generate uses, plus
	//    the source spelling; allocate src := w.n("src").

	// 3. table := reads(src). Set w.reads = func(path string) string {
	//        top, rest, cut := strings.Cut(path, ".")
	//        held := "(" + table[top] + ")"   // parenthesized: a hint expression selects safely
	//        if !cut { return held }
	//        return held + "." + rest
	//    }
	//    A missing top is forge's own bug: return an error naming the member.

	// 4. Emit Append<T>JSONFrom<S>: the doc comment names the equality it
	//    keeps ("byte-identical to <T>From<S> followed by AppendJSON");
	//    signature as produced above; body is the subject form's appendBody —
	//    the same call Generate makes for the subject's own appender, under
	//    w.reads.

	// 5. Emit Write<T>JSONFrom<S>: mirror marshaller()'s scratch pattern —
	//    take the scratch buffer, call the append function, write to w,
	//    return int64(n) and the first error; release the scratch the way
	//    jsonFinish/the pool expects (read tmpl.go for the exact helpers —
	//    do NOT invent new runtime calls; if the runtime lacks a "finish to
	//    writer" helper, assemble from jsonTakeScratch + the pool's put).

	// 6. parsed(...) with the codec's own parsed helper; imports: the source
	//    spelling's imports + "io" + whatever Reaching keeps.
}
```

The exact planner entry, form type, and appendBody invocation must be read
from `json.go`'s `Generate` and copied — the fused body is the subject's own
appender body under different reads and a different signature, not a new
emitter. Where `Generate` emits methods on the subject, `Fused` emits two
free functions and nothing else.

- [ ] **Step 4: Run** `go test ./internal/layers/jsoncodec/ -count=1` → PASS
  (including the codec's own suite: the refactor is additive).
- [ ] **Step 5: Commit** `git commit -m "feat: teach the codec to write a document from somebody else's reads"`.

---

### Task 4: The mapping layer fuses

**Files:**
- Modify: `internal/layers/mapping/mapping.go` (Generate), new
  `internal/layers/mapping/fuse.go`
- Modify: `internal/layers/surface_test.go` (the `against` map:
  `"mapping": ""` → `"mapping": "the fused writer the codec builds from a mapping's bindings"`)
- Test: `internal/layers/mapping/plan_test.go` additions

**Interfaces:**
- Consumes: `jsoncodec.Fused` (Task 3), the stage-1 `plan`/`binding` table,
  `respelled` (hint.go).
- Produces: for a declaration whose stack contains
  `{model.MarkerPkg, "Json"}`, `mapping.Layer.Generate` returns the stage-1
  unit (declared type + constructor) with the fused unit's decls, comments and
  imports appended. Fset note: two parsed units carry two fsets —
  **do not merge them into one Unit naively.** Read how `emit.Section` keeps
  Decls+Comments+Fset together and how a layer returns more than one section
  (jsoncodec's `Provides`); if `plugin.Unit` cannot carry two fsets, return
  the fused half through `Unit.Provides` keyed by the fused function name, the
  way the codec provides nested codecs — read `internal/layer/layer.go`'s
  Unit doc for the Provides contract first, and pick the mechanism that keeps
  the three together. This decision is the task's one real judgement call;
  everything downstream (the emitter) already handles both mechanisms.

- [ ] **Step 1: Failing test** (in `plan_test.go`; add a `json bool` field to
  `pair` and, in `contextFor`, append
  `model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Json"}}`
  to a `Stack` field on the built Model when set — stage-1 contexts carried no
  Stack, so add `Stack: stack` to the literal):

```go
// A bridge over the codec generates the fused writers beside the constructor.
func TestABridgeOverTheCodecFuses(t *testing.T) {
	loaded := loadFixture(t)
	unit, err := New().Generate(contextFor(t, loaded, pair{
		pkg: modelPkg, source: "User", target: "Person", json: true,
	}), plugin.Shape{})
	if err != nil {
		t.Fatalf("fusing was refused: %v", err)
	}

	text := renderedUnit(t, unit) // helper over emit.File, as in Task 3's test
	for _, want := range []string{
		"func PersonFromUser(src *User) Person",
		"func AppendPersonJSONFromUser(dst []byte, src *User) ([]byte, error)",
		"func WritePersonJSONFromUser(w io.Writer, src *User) (int64, error)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the fused unit does not contain %q", want)
		}
	}
}

// Without the codec beneath it, nothing changes: stage 1's output, no writers.
func TestABridgeAloneStillOnlyConstructs(t *testing.T) {
	loaded := loadFixture(t)
	unit, err := New().Generate(contextFor(t, loaded, pair{
		pkg: modelPkg, source: "User", target: "Person",
	}), plugin.Shape{})
	if err != nil {
		t.Fatalf("the plain pair was refused: %v", err)
	}
	if text := renderedUnit(t, unit); strings.Contains(text, "AppendPersonJSONFromUser") {
		t.Error("a bridge with no codec beneath it grew writers")
	}
}
```

- [ ] **Step 2: Run to verify failure.**
- [ ] **Step 3: Implement `fuse.go`:**

```go
// fusedInStack reports whether the declaration writes the codec beneath the
// bridge, which is what asks for the fused writers.
func fusedInStack(ctx *plugin.Context) bool {
	for _, ref := range ctx.Model.Stack {
		if ref.Origin.Pkg == plugin.MarkerPkg && ref.Origin.Name == "Json" {
			return true
		}
	}
	return false
}

// fusedReads builds the codec's reads from the settle table: what each member
// of the target holds, spelled against the source parameter the codec binds.
func fusedReads(built *plan, target string) func(src string) map[string]string {
	return func(src string) map[string]string {
		out := make(map[string]string, len(built.members))
		for _, member := range built.members {
			switch member.via {
			case settledField:
				out[member.field.Name] = src + "." + member.from
			case settledMethod:
				out[member.field.Name] = src + "." + member.from + "()"
			case settledHint:
				out[member.field.Name] = respelled(member.hint,
					map[string]string{built.srcParam: src, built.dstParam: zeroOf(target)})
			case settledIgnored, settledInvalid:
				out[member.field.Name] = zeroOf(target) + "." + member.field.Name
			}
		}
		return out
	}
}

// zeroOf spells the zero value a member that stays unset holds.
func zeroOf(target string) string { return target + "{}" }
```

**Watch the hint case:** a hint whose expression mentions `dst` reads the
value under construction — under fusion there is no such value. Stage-1
grammar allows `dst` on the right-hand side syntactically. Decide it here:
refuse fusion for a hint whose expression mentions the dst parameter (walk
the expr for an ident == `built.dstParam` before building reads; refuse with
`codeHintGrammar` and the message "a fused mapping's hint cannot read dst:
there is no target value while the document is written"). Add the refused
fixture (`spec.go`: a hint whose RHS reads `dst.ID`) and a test row asserting
FRG3032 when — and only when — the pair is fused (`json: true`); the same
hint UNfused plans clean.

In `mapping.go`'s `Generate`, after `written(ctx, built)`:

```go
	if !fusedInStack(ctx) {
		return unit, nil
	}
	fused, err := jsoncodec.Fused(ctx, fusedReads(built, targetSpelled))
	if err != nil {
		return plugin.Unit{}, err
	}
	// Merge per the mechanism chosen above (Provides or a second section) —
	// never append Decls across two fsets into one section.
```

(`targetSpelled` is the same spelling `written` computes — thread it out of
`written` or recompute with the same `SubjectSpelling` call.)

- [ ] **Step 4: Run** `go test ./internal/layers/mapping/ ./internal/layers/ -count=1` → PASS (surface test row updated).
- [ ] **Step 5: Commit** `git commit -m "feat: write the document straight from the source"`.

---

### Task 5: The byte-equality gate

**Files:**
- Modify: `internal/layers/mapping/mapping_test.go` (gate pairs gain
  `json: true` variants; `generated` merges the Json layer's unit too)
- Modify: `internal/layers/mapping/testdata/reference.go.txt`
- Test: this is the test

**Interfaces:**
- Consumes: everything above; `jsoncodec.New().Generate` for the codec's own
  unit (mirror `internal/layers/jsoncodec/codec_test.go`'s `generated` for
  the Provides/jsonwire handling — the temp module needs the wire runtime
  exactly as that test provides it).

- [ ] **Step 1: Extend the gate.** In `mapping_test.go`: add fused pairs —
  `{source: "User", target: "Person", json: true}`,
  `{source: "Account", target: "Rolodex", json: true}` (a tag pin),
  `{source: "User", target: "Renamed", hint: "renamedFromUser", json: true}`
  (a hint), `{source: "Terse", target: "Sparse", ignore: "Note", json: true}`
  (a zero member). For each `json: true` pair, `generated` also calls
  `jsoncodec.New().Generate` for the target (once per distinct target, keyed
  like the codec test's `seen` map) and appends the jsonwire unit once — copy
  the pattern from `codec_test.go:70-102` including `Provides` handling.
  Distinct declared names: fused pairs already get `<S>To<T>` from
  `contextFor`; a plain and a fused pair over one (S, T) would collide — keep
  either the plain or the fused variant of each pair in the gate, not both.

- [ ] **Step 2: Equality tests in the reference file.** Append to
  `reference.go.txt`, one per fused pair (bytes and error verdict):

```go
func TestFusedPersonEqualsConstructThenEncode(t *testing.T) {
	src := User{ID: 7, Email: "a@b", Age: 40}

	held := PersonFromUser(&src)
	want, wantErr := held.AppendJSON(nil)
	got, gotErr := AppendPersonJSONFromUser(nil, &src)

	if (gotErr == nil) != (wantErr == nil) {
		t.Fatalf("fused err = %v, construct-then-encode err = %v", gotErr, wantErr)
	}
	if string(got) != string(want) {
		t.Errorf("fused wrote %s, want %s", got, want)
	}

	var buf bytes.Buffer
	n, err := WritePersonJSONFromUser(&buf, &src)
	if err != nil || n != int64(len(want)) || buf.String() != string(want) {
		t.Errorf("Write wrote %q (%d, %v), want %q", buf.String(), n, err, want)
	}
}
```

(and the analogous three for Rolodex/Renamed/Sparse — write each out in
full; the Sparse one asserts the ignored member's zero appears exactly as
construct-then-encode writes it). Add `"bytes"` to the reference file's
imports.

- [ ] **Step 3: Run** `go test ./internal/layers/mapping/ -count=1` →
  the temp module compiles and every equality holds.
- [ ] **Step 4: Commit** `git commit -m "test: hold the fused writer to construct-then-encode, byte for byte"`.

---

### Task 6: The example, the budgets, the shadow, the whole gate

**Files:**
- Modify: `examples/people/person.go` (or a new `applicant.go`): an
  `Applicant` source struct mirroring Person's fields (same types; rename one
  and pin it with a `from` tag so the example shows the tag)
- Modify: the example's spec file (grep `forgespec` in examples/people):
  `type ApplicantWire Map[Applicant, Json[Person]]` with `//forge:map`
- Modify: `examples/people/codec_bench_test.go` (or a new
  `map_bench_test.go`): `BenchmarkFusedJSONEncode` (warmed buffer, zero
  allocs) and `BenchmarkConstructThenEncode` beside it
- Modify: `scripts/budget.txt`: rows for both benchmarks
  (`FusedJSONEncode 0 0`; measure the other and write what it costs)
- Modify: `internal/generate/shadow_test.go`: row
  `{layer: "map", stack: []string{"Map", "Json"}, names: [...]}` — start
  from the stage-1 map row's names plus the json row's names and prune to
  what the fused stack's generation actually binds; run and adjust
- Modify: `docs/diagnostics.md` only if FRG1009's summary changed in Task 1
- Test: the full gate

- [ ] **Step 1: The example.** Add `Applicant`, the spec declaration and
  regen: `make example`. Read the generated `forge.gen.go` diff — it should
  gain the constructor, the fused pair, and nothing unrelated. Write a small
  example test asserting `ApplicantWire`-adjacent behavior through the public
  functions (equality against construct-then-encode, one case) in
  `examples/people/map_test.go` — the example package's tests run in CI.

- [ ] **Step 2: The benchmarks.** Mirror `BenchmarkJSONEncode`'s warmed-buffer
  pattern (`codec_bench_test.go:51-70`) exactly:

```go
// The fused writer's whole point: the document, no Person built.
func BenchmarkFusedJSONEncode(b *testing.B) {
	src := applicantFixture()

	var buf []byte
	write := func() {
		var err error
		if buf, err = AppendPersonJSONFromApplicant(buf[:0], &src); err != nil {
			b.Fatal(err)
		}
	}
	write()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		write()
	}
}
```

and `BenchmarkConstructThenEncode` doing `PersonFromApplicant` +
`AppendJSON(buf[:0])` with the same warming. Run
`./scripts/bench.sh` (or the make target that wraps it — read the Makefile)
once to measure, then write both budget rows; the fused row must be `0 0`.

- [ ] **Step 3: The shadow row.** Add the `["Map", "Json"]` row; the
  `shadowing` helper already builds a source for `layer == "map"` — it keys
  the stack from `of.stack`, so no change beyond the row unless the json
  entry needs the fixture subject's `Name string` field renamed (it does not:
  the row's names are what to iterate on). Run
  `go test ./internal/generate/ -run Shadow -count=1` and prune names that
  fail to generate for unrelated reasons; every surviving name must compile.

- [ ] **Step 4: The whole gate.**

```bash
go test ./... -count=1
make check
go test -race ./internal/layers/mapping/ ./internal/layers/jsoncodec/ ./internal/generate/ -count=1
make fresh
git status --short   # only intended files
```

Coverage note: if the global floor complains, the uncovered lines are refusal
branches in `fused.go`/`fuse.go` — extend the refused fixtures, not filler.

- [ ] **Step 5: Commit** `git commit -m "feat: show the fusion in the example and hold it to its budgets"`.

- [ ] **Step 6: Tick this plan's boxes and stop.** Integration is the
  finishing-a-development-branch skill's business.

---

## Self-Review (performed while writing)

- **Spec coverage:** §1 declaration/composition → Task 1 (resolution already
  handles the shape — verified against stage-1's resolver walk, which
  descends `Json[Person]` as a claimed marker). §2 generated shape → Tasks 3
  (functions), 4 (beside the constructor), 5 (codec + runtime in the gate
  module), 6 (example). §3 fusion table → Task 4's `fusedReads` (all four
  settle kinds); the seam → Tasks 2+3; nested members → Task 2 leaves nested
  reads alone by design. §4 refusals → Task 1 (composition) and Task 4 (the
  dst-reading hint — a case the spec called impossible; the plan closes it
  with the existing FRG3032 rather than a new code, keeping §4's "no new
  codes"). §5 gates → Task 5 (byte equality), Task 6 (zero-alloc, shadow,
  whole gate). §6 out-of-scope respected: no decode, no containers.
- **Placeholder scan:** the two deliberate judgement calls are named as such
  with the decision procedure spelled out (Unit/fset merging in Task 4;
  runtime helper reuse in Task 3 step 5) — the executor reads the named files
  and picks the existing mechanism, never invents one.
- **Type consistency:** `Fused(ctx, reads func(src string) map[string]string)`
  is identical in Tasks 3 and 4; `pair.json` and `contextFor`'s `Stack`
  addition are introduced in Task 4 and reused in Task 5; the fused function
  names are spelled the same in Tasks 3, 5 and 6.
