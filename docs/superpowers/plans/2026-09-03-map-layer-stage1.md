# Map Layer (Stage 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `type UserPerson Map[User, Person]` under `//forge:map` generates a
compiled, ledgered constructor `PersonFromUser(src *User) Person`, with members
matched by name, settled by compiled-but-never-called hints, or refused.

**Architecture:** A new marker `Map[S, T any]` (forge's first two-parameter
marker) resolves into a one-layer stack of a new kind, `KindBridge`, whose
source type travels beside the subject through the pipeline. The layer walks S
with go/types, matches T's fields on a four-rung ladder, folds in hints — real
functions in the spec file whose bodies the loader now keeps and type-checks —
and emits the constructor as assembled text reparsed under one fresh FileSet.

**Tech Stack:** Go 1.27, go/types + go/ast + golang.org/x/tools/go/ast/astutil,
forge's internal pipeline (discover → resolve → model → compose → generate).

**Spec:** `docs/superpowers/specs/2026-09-03-map-layer-design.md`

## Global Constraints

- Branch: `feat/map-layer` (main is hook-protected; never commit to it directly).
- Every commit must pass the pre-commit hooks: gofumpt, typos (allowlist in
  `typos.toml`), gitleaks. Commit subjects are lowercase `type: sentence`
  (e.g. `feat: resolve the two-argument marker`), no ticket references.
- `make check` must pass at the end of every task that says so: gofmt, vet
  (incl. `-tags forgespec`), golangci-lint (cyclop max 15, funlen max 40
  statements/65 lines ignoring comments, no unused symbols), coverage ≥ 90%
  global, size gate.
- Comment voice: full sentences explaining why, written for the file they land
  in; read neighbouring files first. No TODOs.
- Diagnostic codes are allocated, never reused: this plan uses 2034–2038
  (subject-shaped) and 3025–3031 (directive/hint-shaped). `diag.Register`
  panics on duplicates, so a collision is loud.
- Generated identifiers must go through the locals allocation
  (`plugin.Locals`/`plugin.Mentioned`) — see `internal/layers/jsoncodec/locals.go:20-55`
  for the canonical wrapper. A subject named `src` must still compile.
- The layer must stay on the published `plugin` surface (its
  `internal/layers/surface_test.go` row is `""`), like enum.

---

### Task 1: The `Map[S, T]` marker

**Files:**
- Modify: `forge.go` (append after `Csv`, ~line 231)
- Modify: `forge_test.go:184-192` (the one-type-parameter fatal), `forge_test.go:35-69` (the marker table), `forge_test.go:197` (instantiation)

**Interfaces:**
- Produces: `forge.Map[S, T any]`, a zero-sized two-parameter element-style
  phantom struct. Later tasks refer to it as `model.TypeRef{Pkg: model.MarkerPkg, Name: "Map"}`.

- [ ] **Step 1: Write the failing test.** In `forge_test.go`, extend the
  `markers` map (line ~35, shape values `shapeElement`/`shapeContainer` — read
  the file for the exact enum first) with a new shape or a special case:

```go
// In the markers table (forge_test.go:35-69), add:
	"Map": shapeBridge,
```

and add the constant beside `shapeElement`/`shapeContainer`:

```go
	// shapeBridge is a marker over two types: a source it reads and a target
	// it writes. Zero-sized like an element, and the one marker whose stack
	// stays linear by taking the layer below as its second argument.
	shapeBridge
```

Then in the parameter-count check (forge_test.go:184-192), replace the flat
fatal with a per-shape expectation:

```go
	want := 1
	if markers[name] == shapeBridge {
		want = 2
	}
	params := named.TypeParams()
	if params.Len() != want {
		t.Fatalf("%s takes %d type parameters, want exactly %d", name, params.Len(), want)
	}
	for i := range params.Len() {
		if got := params.At(i).Constraint().String(); got != "any" {
			t.Errorf("%s constrains type parameter %d to %s, want any", name, i, got)
		}
	}
```

and where `instantiate(t, named, person)` is called (forge_test.go:197),
instantiate bridges with two arguments (add a second helper type or reuse
`person` twice: `instantiate(t, named, person, person)` — change `instantiate`
to variadic `args ...types.Type`). The element-shape assertions (zero-sized;
two instantiations do not share an underlying type) must also run for
`shapeBridge` — follow the existing element branch at forge_test.go:209-224.

- [ ] **Step 2: Run to verify it fails.**

Run: `go test . -run 'TestMarker' -count=1`
Expected: FAIL — `Map` is in the table but `forge.go` declares no such type
(lookup failure), or the parameter-count fatal if the lookup is lazy.

- [ ] **Step 3: Declare the marker.** Append to `forge.go`:

```go
// Map generates a constructor that builds the second type from the first:
// members matched by name where that is unambiguous and assignable, settled by
// a //forge:map hint where it is not, and refused where they are neither. The
// source may be a struct or an interface; the target gains nothing — the
// constructor is a package function named from both, PersonFromUser for
// Map[User, Person].
//
// Kind: bridge. Stage: v1. Directive: //forge:map.
type Map[S, T any] struct {
	_ [0]S
	_ [0]T
}
```

- [ ] **Step 4: Run the marker tests.**

Run: `go test . -count=1`
Expected: `TestMarkerSetMatchesTheCatalog` and `TestEveryMarkerIsClaimedByALayer`
now FAIL (no layer claims Map yet). That is correct and expected — note the
failure text, and if these tests cannot tolerate an unclaimed marker until Task
7 registers the layer, reorder locally: keep this task's commit for after Task
7's registration, or (preferred) check whether the claimed-by-a-layer test
consults `layers.Builtins()`; if so, Task 7's stub registration (Layer with
`StageReady`, `Generate` returning an error) can be pulled forward into this
task as a minimal `internal/layers/mapping/mapping.go` so the package always
has a green tree. Decide by running; a red intermediate commit is not allowed.

- [ ] **Step 5: Commit** (possibly folded with the minimal layer stub):

```bash
git add forge.go forge_test.go
git commit -m "feat: declare the two-parameter Map marker"
```

---

### Task 2: `KindBridge`

**Files:**
- Modify: `internal/model/kind.go:15-62` (the enum and `kindNames`)
- Modify: `plugin/layer.go:28-33` (re-export), `plugin/surface_test.go:49` allowlist
- Modify: `internal/compose/rules.go` (a bridge-stands-alone rule; the
  `opaque()` sentence at rules.go:307-313)
- Test: `internal/model/` kind test (find the existing one via `grep -rn 'kindNames\|Kind.String' internal/model/*_test.go`), `internal/compose/rules_test.go` (or the package's existing rule test file — find with `grep -rln 'FRG1002' internal/compose`)

**Interfaces:**
- Produces: `model.KindBridge` / `plugin.KindBridge`, `String() == "bridge"`;
  compose rule: a `KindBridge` entry must be the only layer of its stack
  (diagnostic `FRG1009`, message `a bridge stands alone over its two types`).
- Consumes: nothing from earlier tasks.

- [ ] **Step 1: Write failing tests.** In the model package's kind test, add
  `KindBridge` to whatever table asserts `String()`/`Valid()`. In the compose
  rules test, add a case: a stack `[{Origin: Ring}, {Origin: Map-as-bridge}]`
  (build `model.LayerRef` values by hand as the neighbouring tests do) must
  report `FRG1009`; a stack of exactly one bridge entry must pass the rule.

- [ ] **Step 2: Run to verify failure.**

Run: `go test ./internal/model/ ./internal/compose/ -count=1`
Expected: FAIL — `KindBridge` undefined.

- [ ] **Step 3: Implement.** In `internal/model/kind.go`, append to the enum
  (after `KindTransport`, never reordering existing values):

```go
	// KindBridge spans two types rather than sitting over one: it reads a
	// source and writes about a target, and composes with nothing above it.
	// It neither holds elements nor refines a stream, so no storage is
	// defaulted beneath it.
	KindBridge
```

and extend `kindNames` with `KindBridge: "bridge",`. In `plugin/layer.go` add
`KindBridge = model.KindBridge` to the const block and add `"KindBridge"` to
the `plugin/surface_test.go` allowlist. In `internal/compose/rules.go`, add the
rule beside its siblings (read `elements()` at rules.go:147-165 for the house
shape) and register it wherever `validate()` lists rules:

```go
// codeBridgeAlone reports a bridge composed with anything else. A bridge reads
// one type and writes about another; there is no stream for a storage to hold
// or a refiner to query, so a stack around one describes machinery with
// nothing to attach to.
var codeBridgeAlone = diag.Register(1009, "a bridge stands alone over its two types")

func bridges(decl Declaration, diags *diag.Set) {
	held := -1
	for i, ref := range decl.Stack {
		if ref.Kind == model.KindBridge {
			held = i
		}
	}
	if held < 0 || len(decl.Stack) == 1 {
		return
	}
	diags.Add(diag.New(codeBridgeAlone, decl.Pos(),
		"%s is a bridge and composes with nothing else in a stack",
		decl.Stack[held].Origin.Name).
		WithHint("declare the bridge on its own: type X Map[Source, Target]"))
}
```

(Adapt the exact `Declaration`/`Pos()` spelling to what `rules.go`'s
neighbouring rules use — read two of them first; the diagnostics style is
theirs.) Also add a `KindBridge` arm to `opaque()` (rules.go:307-313) so an
inline declaration is refused with its own sentence:

```go
	if ref.Kind == model.KindBridge {
		return "is a form of its own rather than a container, so a declaration " +
			"naming it has a phantom struct as its underlying type"
	}
```

Note stage 2 will relax `bridges` to allow exactly `[Map, Json]`; keep the
function's shape amenable (it already reports only when `len(stack) > 1`).

- [ ] **Step 4: Run the tests.**

Run: `go test ./internal/model/ ./internal/compose/ ./plugin/ -count=1`
Expected: PASS. Also confirm `defaulted()` needs no change: read
`internal/compose/compose.go:367-405` — a bridge sets `elements=false` via the
`ref.Kind != model.KindElement` line and neither `refining` nor `storage`, so
`(!refining && !elements)` returns the stack unchanged. Add one test in the
compose package proving a single-bridge stack gets **no** implicit Slice.

- [ ] **Step 5: Commit.**

```bash
git add internal/model/kind.go plugin/layer.go plugin/surface_test.go internal/compose/
git commit -m "feat: give a two-type bridge a kind of its own"
```

---

### Task 3: Resolving `Map[S, T]`

**Files:**
- Modify: `internal/resolve/resolve.go:109-150` (the walk), `resolve.go:25-48`
  (`Declaration`), `resolve.go:13-16` (arity hint text)
- Test: `internal/resolve/resolve_test.go`, fixture under
  `internal/resolve/testdata/stacks/` (read the existing fixtures first —
  `resolve_test.go:16` names a fixture marker package `stacksfixture/markers`)

**Interfaces:**
- Consumes: `forge.Map` exists (Task 1). The resolver recognises Map by origin
  name against the claims set, exactly as `marker()` does.
- Produces: `resolve.Declaration.Source types.Type` — nil for every
  non-bridge declaration; for `Map[User, Person]` it is `User`'s type and
  `Declaration.Subject` is `Person`'s. The stack holds one
  `model.LayerRef{Origin: {MarkerPkg, "Map"}}`.

- [ ] **Step 1: Write the failing test.** Add a fixture file to the resolve
  testdata module (beside the arity fixture; note the fixture uses its own
  marker package, so add a two-parameter `Map[S, T any] struct{ _ [0]S; _ [0]T }`
  to `stacksfixture/markers` — check how the fixture's markers are claimed in
  `resolve_test.go` and claim `Map` the same way). Then:

```go
// A bridge names two types: the source it reads and the target it writes.
// The first is carried beside the stack rather than pushed onto it, because
// a source is not a layer.
func TestABridgeCarriesItsSource(t *testing.T) {
	declarations := resolved(t, "bridge") // follow the file's existing loader helper

	one := single(t, declarations) // or however sibling tests pick the one declaration
	if got, want := len(one.Stack), 1; got != want {
		t.Fatalf("the stack holds %d layers, want %d", got, want)
	}
	if got := one.Stack[0].Origin.Name; got != "Map" {
		t.Errorf("the layer is %s, want Map", got)
	}
	if one.Source == nil {
		t.Fatal("the source was not carried")
	}
	if got := model.TypeString(one.Source); !strings.HasSuffix(got, ".User") {
		t.Errorf("the source is %s, want User", got)
	}
	if got := model.TypeString(one.Subject); !strings.HasSuffix(got, ".Person") {
		t.Errorf("the subject is %s, want Person", got)
	}
}
```

with fixture `testdata/stacks/bridge/bridge.go`:

```go
package bridge

import "stacksfixture/markers"

type User struct{ Name string }

type Person struct{ Name string }

type UserPerson markers.Map[User, Person]
```

(Match the existing fixtures' package layout and build tags exactly — read
`testdata/stacks/arity/arity.go` first.)

- [ ] **Step 2: Run to verify failure.**

Run: `go test ./internal/resolve/ -run TestABridgeCarriesItsSource -count=1`
Expected: FAIL — today `args.Len() > 1` reports FRG1007 and drops the
declaration, so `resolved` returns nothing (or `Source` is undefined at
compile time first).

- [ ] **Step 3: Implement.** Add the field to `Declaration` (resolve.go:25):

```go
	// Source is the type a bridge reads, carried beside the stack because a
	// source is not a layer: Map[User, Person] pushes Map and descends into
	// Person, and User rides here. Nil for every declaration that is not a
	// bridge.
	Source types.Type
```

In `resolve()` (resolve.go:109), replace the arity branch with a shape switch.
The resolver has no registry — it knows origins, not kinds — so it recognises
the bridge by origin, the same way `marker()` recognises markers by claims:

```go
	var source types.Type
	for {
		named, ok := current.(*types.Named)
		if !ok || !marker(named, claims) {
			break
		}

		args := named.TypeArgs()
		if args.Len() == 0 {
			break
		}

		// A bridge takes two: the source it reads, and the layer or subject
		// below. Every other marker takes exactly one — the layer below it.
		if bridge(named) {
			if args.Len() != 2 || source != nil {
				diags.Add(arity(candidate, stack, named))
				return Declaration{}, false
			}
			source = types.Unalias(args.At(0))
			stack = append(stack, model.LayerRef{Origin: origin(named)})
			current = types.Unalias(args.At(1))
			continue
		}

		if args.Len() > 1 {
			diags.Add(arity(candidate, stack, named))
			return Declaration{}, false
		}

		stack = append(stack, model.LayerRef{Origin: origin(named)})
		current = types.Unalias(args.At(0))
	}
```

with:

```go
// bridge reports whether a marker is the one that takes two type arguments.
//
// By name against the marker package, because resolution happens before the
// registry assigns kinds: what is known here is where a type comes from and
// what it is called, and Map is the one name with a second argument.
func bridge(named *types.Named) bool {
	held := origin(named)
	return held.Pkg == model.MarkerPkg && held.Name == "Map"
}
```

(Verify `model.MarkerPkg` is the constant the claims use — grep `MarkerPkg` in
internal/model; if origins in the fixture tests use the fixture package path,
key `bridge()` off the claims set the way `marker()` does instead: pass
`claims` in and compare the origin's registered kind... the registry is not
available here, so name-matching against the claimed origin set is the honest
version — mirror however `marker()` narrows.) The `source != nil` guard
refuses a bridge nested under a bridge. Thread `source` into the return:
`Declaration{Candidate: candidate, Stack: stack, Subject: current, Source: source}`.

Also update `arityHint` (resolve.go:16) to stop overclaiming:

```go
const arityHint = "every layer takes exactly one type argument, the layer below it — " +
	"except Map, which takes its source and its target; capacities, keys and " +
	"sort fields are written as //forge: options"
```

- [ ] **Step 4: Run the resolve suite.**

Run: `go test ./internal/resolve/ -count=1`
Expected: PASS, including the pre-existing FRG1007 arity tests (they use a
fixture `Pipeline[string, int]`, not Map, so they still refuse).

- [ ] **Step 5: Commit.**

```bash
git add internal/resolve/
git commit -m "feat: resolve the bridge marker's two arguments"
```

---

### Task 4: Carrying the source to the layer

**Files:**
- Modify: `internal/model/model.go` (add `Source` beside `Subject`, ~line 33)
- Modify: `internal/cli/write.go:200-214` (`built`), `internal/cli/explain.go:257-271` (`specialised`)
- Modify: `internal/generate/generate.go` fingerprint (`FingerprintPackage`,
  find `sum.AddString("form", ...)` at ~generate.go:1307 and add the source)
- Test: extend an existing pipeline-level test — `internal/cli/` has
  `forge_test.go`/acceptance-style tests; simplest is a unit test beside
  `internal/generate`'s `shadow_test.go` helpers building a Request by hand
  (defer full end-to-end to Task 10's fixture run)

**Interfaces:**
- Consumes: `resolve.Declaration.Source` (Task 3).
- Produces: `model.Model.Source types.Type` (nil when not a bridge), reachable
  in a layer as `ctx.Model.Source`. The fingerprint changes when S changes.

- [ ] **Step 1: Add the field** to `model.Model` (read model.go:20-50 first;
  place it after `Subject`):

```go
	// Source is the type a bridge reads, raw rather than modelled: what a
	// bridge needs from it — exported fields, zero-parameter methods — is a
	// question go/types answers directly, and a *Struct model of it would
	// carry a fields list that is wrong for an interface. Nil for every
	// declaration that is not a bridge.
	Source types.Type
```

- [ ] **Step 2: Thread it.** In `internal/cli/write.go` `built()` add
  `Source: decl.Source,` to the `model.Model` literal; make the identical
  change in `internal/cli/explain.go` `specialised()`. In
  `internal/generate/generate.go`, where the fingerprint adds the form, add:

```go
	if held.Source != nil {
		sum.AddString("source", model.TypeIdentity(held.Source))
	}
```

(Verify `model.TypeIdentity` exists — jsoncodec's `key()` calls
`plugin.TypeIdentity`; use the model-package original.)

- [ ] **Step 3: Build and test.**

Run: `go build ./... && go test ./internal/cli/ ./internal/generate/ ./internal/model/ -count=1`
Expected: PASS (nothing consumes Source yet; this step guards against a
missed literal-field compile error and fingerprint drift).

- [ ] **Step 4: Commit.**

```bash
git add internal/model/model.go internal/cli/ internal/generate/
git commit -m "feat: carry a bridge's source beside its subject"
```

---

### Task 5: The loader keeps directive-carrying bodies

**Files:**
- Modify: `internal/load/parse.go:38-66`
- Test: `internal/load/` — find the existing parse/strip tests with
  `grep -rn 'stripBodies\|needsBody' internal/load/*_test.go`

**Interfaces:**
- Produces: for a package-level function whose doc comment contains a line
  beginning `//forge:`, `pkg.Syntax` keeps the real `*ast.FuncDecl.Body` and
  `pkg.TypesInfo` holds types for every expression in it. Everything else is
  stripped exactly as before.

- [ ] **Step 1: Write the failing tests** (in the load package's existing
  test-fixture style — read how current strip tests build sources first):

```go
// A function carrying a forge directive keeps its body, because a stage reads
// it: a hint's statements are the input, and a stripped hint is no input at
// all. Everything else is stripped exactly as before — bodies are bulk the
// pipeline never reads.
func TestADirectiveCarryingFunctionKeepsItsBody(t *testing.T) {
	// Load a fixture holding:
	//   //forge:map hint
	//   func personFromUser(src *User, dst *Person) { dst.Name = src.Name }
	//   func helper() int { return 1 }
	// Assert: the hint's FuncDecl.Body != nil and has 1 statement; helper's
	// Body == nil; and TypesInfo.Types has an entry for the hint's RHS.
}

// A hint that does not type-check is a load diagnostic, not a generator
// mystery: keeping the body puts it in front of the compiler forge already
// runs.
func TestABrokenHintFailsTheLoad(t *testing.T) {
	// Same fixture with dst.Name = src.Missing — assert the session's
	// diagnostics are non-empty and mention Missing.
}
```

- [ ] **Step 2: Run to verify failure.**

Run: `go test ./internal/load/ -run 'Hint|KeepsItsBody' -count=1`
Expected: FAIL — body is nil today.

- [ ] **Step 3: Implement** in `internal/load/parse.go`. Extend `needsBody`
  (it has no fset, so the check is textual over the doc group — the parse mode
  keeps comments):

```go
// directed reports whether a function's doc carries a forge directive, which
// is what marks a body a stage will read: a hint's statements are input, and
// stripping them would hand the generator a function with nothing in it. The
// check is textual because this runs inside the parser, before positions mean
// anything — the directive grammar proper is applied where the hint is read.
func directed(fn *ast.FuncDecl) bool {
	if fn.Doc == nil {
		return false
	}
	for _, one := range fn.Doc.List {
		if strings.HasPrefix(one.Text, model.DirectivePrefix) {
			return false || true // see note below
		}
	}
	return false
}
```

**Note:** write it properly as `return true` inside the loop; and check
whether `internal/load` may import `internal/model` (grep load's imports). If
that import is unwanted, inline the prefix: `strings.HasPrefix(one.Text, "//forge:")`
with a comment naming `model.DirectivePrefix` as the source of truth. Then:

```go
func needsBody(fn *ast.FuncDecl) bool {
	if fn.Type.TypeParams.NumFields() > 0 {
		return true
	}
	if directed(fn) {
		return true
	}
	return fn.Recv == nil && fn.Name.Name == "init"
}
```

**Careful:** `needsBody` today routes to `panicBody(fn.Body)` (a body that
panics), not to keeping the body — read `stripBodies` again: generics and
init get `panicBody`. A hint must keep its REAL body. So the change is in
`stripBodies`, not `needsBody`:

```go
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if directed(fn) {
			// A stage reads this body; see [directed].
			continue
		}
		if needsBody(fn) {
			fn.Body = panicBody(fn.Body)
			continue
		}
		fn.Body = nil
	}
```

- [ ] **Step 4: Run the load suite and the world.**

Run: `go test ./internal/load/ -count=1 && go build ./... && go test ./internal/discover/ -count=1`
Expected: PASS. (Discover must still report FRG3001 for the unclaimed
function directive — that changes in Task 6, not here. If a load fixture in
another package now fails because a kept body changed diagnostics, read
`internal/load/errors.go:59` and `:242-259` — the unused-import forgiveness
becomes unnecessary for hint files but must not break for others.)

- [ ] **Step 5: Commit.**

```bash
git add internal/load/
git commit -m "feat: keep the body of a function a directive marks"
```

---

### Task 6: Discovering and claiming hints

**Files:**
- Modify: `internal/discover/discover.go` (a `Hint` type, a `claimFuncs`
  sibling to `claimFields` at discover.go:207-221, and `Declarations` returning
  hints — or a parallel `Hints(session)` sharing the claim map; read
  `Declarations` at :70 and keep one walk)
- Modify: `internal/model/model.go` (a `Hint` carrier on `Model`)
- Modify: `internal/cli/pipeline.go:194-278` (thread hints from discovery to
  modelling; match by parameter types; attach to the right request)
- Modify: `internal/cli/explain.go` (the twin of `built()` gets hints too)
- Test: `internal/discover/` tests beside the existing directive-claiming
  tests; a pipeline-level matching test in `internal/cli/` if a harness
  exists, else covered end-to-end in Task 10

**Interfaces:**
- Consumes: kept bodies (Task 5), `model.Model.Source` (Task 4).
- Produces:
  - `discover.Hint{Layer, Args string; Fn *ast.FuncDecl; Pkg *packages.Package; Form model.Form; Pos token.Position}` — every package-level
    function carrying any `//forge:` directive, claimed so FRG3001 stops firing
    for it.
  - `model.Hint{Fn *ast.FuncDecl; Pkg *packages.Package; Pos token.Position}` and
    `model.Model.Hints []Hint` — the hints whose `(src *S, dst *T)` parameter
    types are identical to the declaration's `(Source, Subject)`.
  - Diagnostics: `FRG3029` hint matches no map declaration; `FRG3030` hint
    outside the spec file; `FRG3025` hint signature is not `func(src *S, dst *T)`;
    `FRG3028` two hints for one declaration. Register 3025/3028 where the
    matcher lives (they need S and T to say anything useful); 3029/3030 at the
    matcher too (it sees all hints and all declarations).

- [ ] **Step 1: Failing discover test** (mirror the existing stray-directive
  tests — find them via `grep -rn '3001' internal/discover`):

```go
// A directive on a package-level function is claimed for the stage that reads
// hints, so a correctly written hint stops being reported as applying to
// nothing.
func TestAFunctionDirectiveIsClaimed(t *testing.T) {
	// Fixture: spec file with //forge:map hint above func f(src *A, dst *B).
	// Assert: Declarations reports no FRG3001, and the hint is returned with
	// Layer "map", Args "hint", Fn non-nil with a non-nil Body.
}
```

- [ ] **Step 2: Run to verify failure** (`go test ./internal/discover/ -run Claimed -count=1`).

- [ ] **Step 3: Implement discovery.** In `inFile`'s walk (or a sibling pass
  over `file.Decls`), collect:

```go
// Hint is a package-level function carrying a directive, kept whole for the
// stage that reads it. Discovery claims it and judges nothing: whether the
// directive means anything is the reading stage's to say, which is the same
// bargain claimFields strikes for field options.
type Hint struct {
	Layer string
	Args  string
	Fn    *ast.FuncDecl
	Pkg   *packages.Package
	Form  model.Form
	Pos   token.Position
}
```

For each file: for each `*ast.FuncDecl` with `Recv == nil`, read
`model.Directives(fset, fn.Doc)`; claim each directive's offset into the
existing `claimed` map; append one `Hint` per directive with `Form` from
`load.SpecFile(fset, file)`. Return them beside the candidates (change
`Declarations` to return `([]Candidate, []Hint, diag.Set)` and fix its two
callers — `internal/cli/pipeline.go:239` and any test callers).

- [ ] **Step 4: Implement matching in the pipeline.** In
  `internal/cli/pipeline.go`, after modelling (the `requests` loop at :256-278),
  match hints to requests:

```go
// matched pairs each map hint with the declaration it is for, by the types its
// parameters name: a hint func(src *User, dst *Person) belongs to the
// declaration whose source is User and whose subject is Person. What matches
// nothing is a diagnostic, because a hint nobody reads is a mapping silently
// running without it.
func matched(session *load.Session, hints []discover.Hint, requests []request, diags *diag.Set) {
	for _, hint := range hints {
		if hint.Layer != "map" {
			continue // another layer's directive on a function is FRG3001's to keep refusing — do NOT claim non-map function directives in discover; claim only Layer=="map". Adjust Task 6 Step 3 accordingly.
		}
		if hint.Args != "hint" {
			diags.Add(diag.New(codeHintUnknown, hint.Pos,
				"%q is not something the map layer takes on a function", hint.Args).
				WithHint("a function is marked //forge:map hint and nothing else"))
			continue
		}
		if hint.Form != model.FormSpec {
			diags.Add(diag.New(codeHintNotSpec, hint.Pos,
				"a map hint lives in the spec file, and %s is not one", hint.Pos.Filename).
				WithHint("move the function into a file guarded by //go:build forgespec; "+
					"it is compiled there and never linked"))
			continue
		}
		src, dst, ok := hintTypes(hint)
		if !ok {
			diags.Add(diag.New(codeHintShape, hint.Pos,
				"%s is not shaped like a hint", hint.Fn.Name.Name).
				WithHint("a hint is func(src *S, dst *T) for the S and T of one Map declaration"))
			continue
		}
		if !claimHint(requests, session, hint, src, dst) {
			diags.Add(diag.New(codeHintUnmatched, hint.Pos,
				"%s matches no Map declaration in its package", hint.Fn.Name.Name).
				WithHint("declare type X Map[S, T] with the S and T the hint's parameters name"))
		}
	}
}
```

with `hintTypes` reading the signature from
`hint.Pkg.TypesInfo.Defs[hint.Fn.Name].(*types.Func).Type().(*types.Signature)`
— two params, both `*types.Pointer` to named types, no results, no receiver —
and `claimHint` walking requests for `req.Model != nil && req.Model.Source != nil`
with `types.Identical(req.Model.Source, src) && types.Identical(req.Model.Subject.Type(), dst)`
in the same package, appending `model.Hint{Fn: hint.Fn, Pkg: hint.Pkg, Pos: hint.Pos}` to
`req.Model.Hints` and reporting `FRG3028` when a second lands. Codes:

```go
var (
	codeHintUnknown   = diag.Register(3025, "a function directive the map layer does not take")
	codeHintShape     = diag.Register(3026, "a map hint is not shaped like one")
	codeHintNotSpec   = diag.Register(3030, "a map hint lives outside the spec file")
	codeHintUnmatched = diag.Register(3029, "a map hint matches no declaration")
	codeHintTwice     = diag.Register(3028, "two hints for one mapping")
)
```

(**Important scoping decision, encode it in discover:** only `Layer == "map"`
function directives are claimed; any other function directive still falls
through to FRG3001. That keeps the claim exactly as wide as the reader.)

Add `Hints []Hint` + `type Hint struct { Fn *ast.FuncDecl; Pkg *packages.Package; Pos token.Position }`
to `internal/model/model.go` with a doc comment saying hints ride the model
because the layer that reads them is handed the model and nothing else.

- [ ] **Step 5: Run.**

Run: `go test ./internal/discover/ ./internal/cli/ ./internal/load/ -count=1 && go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add internal/discover/ internal/model/ internal/cli/
git commit -m "feat: discover map hints and hand each to its declaration"
```

---

### Task 7: The mapping layer skeleton and registration

**Files:**
- Create: `internal/layers/mapping/doc.go`, `internal/layers/mapping/mapping.go`
- Modify: `internal/layers/layers.go:29-44` (`written()`), `internal/layers/catalog_test.go:33-131` (the row),
  `internal/layers/layers_test.go:44-76` (the kind map), `internal/layers/surface_test.go:35-52` (`"mapping": ""`)
- Test: the catalog/surface tests above are the tests

**Interfaces:**
- Consumes: `forge.Map` (Task 1), `plugin.KindBridge` (Task 2).
- Produces: `mapping.New() mapping.Layer` registered in `Builtins()`;
  `Origin() == plugin.TypeRef{Pkg: plugin.MarkerPkg, Name: "Map"}`;
  `Kind() == plugin.KindBridge`; `OptionSchema()` with
  `{Key: "ignore", Value: plugin.ValueFields, Doc: "target fields left unset on purpose"}`;
  `Writes() == nil`; `Binds() == nil`; `Doc()` one line; `Generate` stubbed to
  return a clear error until Task 10 replaces it.

- [ ] **Step 1: Write the layer skeleton** (enum is the template — read
  `internal/layers/enum/enum.go` fully first):

```go
// Package mapping generates a constructor from one type's values to
// another's, driven by member names and settled by hints.
package mapping

import (
	"errors"
	"slices"

	"github.com/okian/forge/plugin"
)

// The marker this layer claims, and the name a directive writes it under.
const (
	container  = "Map"
	markerName = "map"
)

// Layer generates the bridge's constructor.
type Layer struct{}

// New returns the map layer.
func New() Layer { return Layer{} }

// Origin identifies the marker this layer claims.
func (Layer) Origin() plugin.TypeRef { return plugin.TypeRef{Pkg: plugin.MarkerPkg, Name: container} }

// Binds names what this layer's output imports: nothing. A constructor is
// assignments, and every type it spells arrives through the subject's own
// spelling.
func (Layer) Binds() []plugin.Import { return nil }

// Writes names the methods this layer puts on the subject: none. The
// constructor is a package function, so the target's method set is untouched
// and the target need not be local.
func (Layer) Writes() []string { return nil }

// Kind says where in a stack the layer may appear: nowhere but alone. A
// bridge reads one type and writes about another, and composes with nothing.
func (Layer) Kind() plugin.Kind { return plugin.KindBridge }

// Stage says how far along the layer is.
func (Layer) Stage() plugin.Stage { return plugin.StageReady }

// Doc returns the one-line summary the list command prints.
func (Layer) Doc() string {
	return "constructor from a source type's values, matched by name and settled by hints"
}

// OptionSchema declares every option the layer accepts.
func (Layer) OptionSchema() []plugin.OptionDef {
	return []plugin.OptionDef{{
		Key: "ignore", Value: plugin.ValueFields,
		Doc: "target fields left unset on purpose",
	}}
}

// Accepts reports whether the layer can sit on the shape beneath it. There is
// no shape beneath a bridge — the composition rule keeps it alone — so there
// is nothing to refuse here; what decides is the two types, refused where the
// answer is, in [planned].
func (Layer) Accepts(plugin.Shape) error { return nil }

// Shape returns what the layer exposes upward, which is nothing: nothing
// composes above a bridge.
func (Layer) Shape(_ *plugin.Context, below plugin.Shape) plugin.Shape { return below }

// Generate returns the constructor for the declaration.
func (Layer) Generate(ctx *plugin.Context, _ plugin.Shape) (plugin.Unit, error) {
	return plugin.Unit{}, errors.New("map: not generating yet") // replaced in the emission task
}

var _ = slices.Clone[[]int] // drop with the first real use of slices
```

(Drop the `slices` filler if unused; keep imports minimal and real. Check
`plugin.Stage`/`StageReady` spelling against enum.)

- [ ] **Step 2: Register and satisfy the rosters.** Add `mapping.New()` to
  `written()` in `internal/layers/layers.go`; add the catalog row:

```go
	"Map": {
		kind: model.KindBridge, stage: layer.StageReady,
		writes: nil,
	},
```

(match the `entry` struct's real fields — read two neighbouring rows); add the
kind to `layers_test.go`'s map; add `"mapping": ""` to `surface_test.go`'s
`against`.

- [ ] **Step 3: Run the rosters and the marker tests.**

Run: `go test ./internal/layers/ . -count=1`
Expected: PASS — including Task 1's `TestEveryMarkerIsClaimedByALayer`, now
that a layer claims Map. If `TestMarkerSetMatchesTheCatalog` reads the marker
doc footers, the `Kind: bridge.` footer written in Task 1 must match the
catalog row — fix whichever is wrong.

- [ ] **Step 4: Commit.**

```bash
git add internal/layers/ forge.go forge_test.go
git commit -m "feat: register the map layer over the bridge marker"
```

---

### Task 8: The match ladder

**Files:**
- Create: `internal/layers/mapping/plan.go`, `internal/layers/mapping/plan_test.go`
  (an internal test — package `mapping` — so the ladder is testable without the
  pipeline; jsoncodec has no internal tests any more, but enum's `plan.go` is
  driven through exported tests; either style is fine, prefer package-external
  `mapping_test` with a tiny fixture loader mirroring
  `internal/layers/jsoncodec/fixture_test.go:28-44`)
- Create: fixture module `internal/layers/mapping/testdata/mapping/{go.mod,model/model.go,refused/refused.go}`

**Interfaces:**
- Consumes: `ctx.Model.Source` (Task 4), `ctx.Model.Subject`, `ctx.Options.List("ignore")`.
- Produces (consumed by Tasks 9/10):

```go
// plan is everything the constructor is built from.
type plan struct {
	source  types.Type      // S as declared
	target  *plugin.Struct  // T's model
	// members, in T field declaration order: how each target field is settled.
	members []binding
	diags   plugin.Diagnostics // adapt: enum uses plugin diagnostics via plan.diags
}

// how a target field is settled.
type settled uint8

const (
	settledInvalid settled = iota
	settledField           // src.<From>
	settledMethod          // src.<From>()
	settledHint            // the hint's assignment, lifted
	settledIgnored         // stays zero, on purpose
)

type binding struct {
	field plugin.Field // the target field
	via   settled
	from  string       // source field or method name (settledField/Method)
	hint  ast.Expr     // the RHS for settledHint, already respelled? NO — raw; respelling is Task 9's
	folded bool        // matched on rung 3, recorded for the ledger
}
```

with the entry point `planned(ctx *plugin.Context) (*plan, error)`.

- [ ] **Step 1: Write the fixture module.** `testdata/mapping/go.mod`:

```
module mapfixture

go 1.27
```

`testdata/mapping/model/model.go` — every rung and both source kinds:

```go
// Package model holds the pairs a constructor is written for: one pair per
// question the ladder has to answer.
package model

// User is a struct source: fields and methods both.
type User struct {
	ID    int
	Email string
	Age   int
}

// FullName is a method candidate: no parameters, one result.
func (u User) FullName() string { return "u" }

// user_name exercises the fold: it reaches Name and nothing else does.
func (u User) User_Name() string { return "n" }

// Person is the plain target.
type Person struct {
	ID    int    // rung 1: field, exact
	Email string // rung 1
	Name  string // rung 3: folded to User_Name
	Age   int    // rung 1
}

// Reader is an interface source.
type Reader interface {
	ID() int
	Label() string
}

// Card is Reader's target.
type Card struct {
	ID    int
	Label string
}
```

`testdata/mapping/refused/refused.go` — one type pair per refusal (unmatched
member, ambiguous fold, non-assignable match, plus an `Ignored` pair for the
option tests). Write each with a doc comment naming the refusal it is for,
matching the voice of `internal/layers/jsoncodec/testdata/codec/refused/refused.go`.

- [ ] **Step 2: Write the failing ladder tests** (fixture loader copied from
  jsoncodec's `fixture_test.go`, pointed at `testdata/mapping`):

```go
func TestTheLadderMatchesByName(t *testing.T) {
	built := plannedFor(t, "User", "Person") // helper: load fixture, build T's model, call planned with a hand-built plugin.Context carrying Source
	want := map[string]settledKind{ // export a test hook or assert through the ledger text — decide when writing plan.go; asserting on the ledger string keeps plan's internals private
		"ID": "field", "Email": "field", "Age": "field", "Name": "method",
	}
	// ... assert each member's settlement and source name.
}

func TestAnInterfaceSourceMatchesByMethod(t *testing.T)  { /* Reader -> Card: both via methods */ }
func TestWhatTheLadderRefuses(t *testing.T)              { /* refused fixtures -> FRG2035/2036/2037 table, refused_test style with plugin.From(err) */ }
func TestIgnoreSettlesAMemberOnPurpose(t *testing.T)     { /* ignore=X removes the refusal; ignore of a settled member -> FRG3031 */ }
```

Use the refused-table shape from `internal/layers/jsoncodec/refused_test.go:25-90`
verbatim (code + message substring + hint substring, `plugin.From(err)`).

- [ ] **Step 3: Run to verify failure**, then **Step 4: implement `plan.go`.**
  The core, concretely:

```go
// Codes this layer refuses with. The shape ones are about the two types; the
// option one is about a directive.
var (
	codeNoMembers    = plugin.Register(2034, "a bridge's source has no members to read")
	codeUnsettled    = plugin.Register(2035, "a target member is settled no way")
	codeAmbiguous    = plugin.Register(2036, "two source members claim one target member")
	codeUnassignable = plugin.Register(2037, "a matched member's types do not assign")
	codeOutOfReach   = plugin.Register(2038, "a target's unexported fields are out of reach")
	codeIgnoreSaysNothing = plugin.Register(3031, "ignore names a member that is already settled")
)

// candidate is one thing S offers: a field, or a method taking nothing and
// returning one value.
type candidate struct {
	name   string
	typ    types.Type
	method bool
}

// candidates lists what a source offers, fields before methods so that the
// ladder's precedence is an ordering fact rather than a comparison.
func candidates(source types.Type) []candidate {
	var out []candidate

	if structure, ok := source.Underlying().(*types.Struct); ok {
		for field := range structure.Fields() {
			if field.Exported() {
				out = append(out, candidate{name: field.Name(), typ: field.Type()})
			}
		}
	}

	// Methods through the pointer's method set, so a getter with either
	// receiver counts: the constructor holds *S (or the interface itself),
	// and both reach everything.
	held := source
	if _, isInterface := source.Underlying().(*types.Interface); !isInterface {
		held = types.NewPointer(source)
	}
	set := types.NewMethodSet(held)
	for sel := range set.Methods() { // verify the iterator's real name: go/types MethodSet has Len/At — write the Len/At loop if Methods() does not exist
		fn, ok := sel.Obj().(*types.Func)
		if !ok || !fn.Exported() {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
			continue
		}
		out = append(out, candidate{name: fn.Name(), typ: sig.Results().At(0).Type(), method: true})
	}

	return out
}

// folded is the comparison a near-miss is recognised by: lowercased with
// underscores dropped, the same fold the tag diagnostics use.
func folded(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

// match settles one target field against the candidates, walking the ladder:
// exact field, exact method, the unique fold, nothing.
func match(field plugin.Field, all []candidate) (candidate, bool, bool, error) {
	var exactField, exactMethod []candidate
	var foldedHits []candidate

	for _, one := range all {
		switch {
		case one.name == field.Name && !one.method:
			exactField = append(exactField, one)
		case one.name == field.Name && one.method:
			exactMethod = append(exactMethod, one)
		case folded(one.name) == folded(field.Name):
			foldedHits = append(foldedHits, one)
		}
	}

	for _, rung := range [][]candidate{exactField, exactMethod} {
		switch len(rung) {
		case 0:
		case 1:
			return rung[0], false, true, nil
		default:
			return candidate{}, false, false, ambiguous(field, rung)
		}
	}
	switch len(foldedHits) {
	case 0:
		return candidate{}, false, false, nil
	case 1:
		return foldedHits[0], true, true, nil
	default:
		return candidate{}, false, false, ambiguous(field, foldedHits)
	}
}
```

(`Fields()`/`Methods()` iterators: verify against go1.27's go/types — if
`*types.Struct` has no `Fields()` range iterator, use the
`NumFields()`/`Field(i)` loop; same for `MethodSet.Len()/At(i)`. The plan's
executor MUST compile-check rather than trust the sketch.) Assignability is
`types.AssignableTo(candidate.typ, field.Type.Type)`; a name match that fails
it is `codeUnassignable` naming both types and hinting "write a hint that
converts, or ignore the member". Unexported target fields: if any exist and
`!target.Local(ctx.Model.Pkg.PkgPath)` → `codeOutOfReach`; if local, they join
the members list and must be settled like any other (matching may settle them
— assignability from the same package is legal — otherwise they demand hints
or ignores). Sources with no candidates at all → `codeNoMembers`. `ignore`
names come from `ctx.Options.List("ignore")`; each must name a target field
(already validated by ValueFields) that is NOT otherwise settled — an ignore
on a field the ladder settles is `codeIgnoreSaysNothing`.

- [ ] **Step 5: Run** `go test ./internal/layers/mapping/ -count=1` → PASS.
- [ ] **Step 6: Commit** `git commit -m "feat: settle a bridge's members on the ladder"`.

---

### Task 9: Hint grammar and lifting

**Files:**
- Create: `internal/layers/mapping/hint.go`, tests in the same package's test files
- Modify: `internal/layers/mapping/plan.go` (fold hint assignments into the members)

**Interfaces:**
- Consumes: `ctx.Model.Hints` (Task 6): `Fn *ast.FuncDecl` with a real body;
  `Pkg.TypesInfo` for expression types.
- Produces: for each hint assignment `dst.F = expr`, a `binding{via: settledHint, hint: expr}`
  on member F, with the hint's `src`/`dst` parameter names recorded so emission
  can respell them; `FRG3026` for any statement outside the grammar; `FRG3027`
  for a member assigned twice within the hint.

- [ ] **Step 1: Failing tests.** Extend the fixture's spec-file side: give the
  fixture module a `model/spec.go` under `//go:build forgespec` with a hint,
  and target pairs whose ladder alone would refuse (a renamed member, a
  conversion). Tests:

```go
func TestAHintSettlesWhatTheLadderCannot(t *testing.T)   { /* hinted member becomes settledHint */ }
func TestAHintOverridesAMatch(t *testing.T)              { /* assign an auto-matched member; assert override recorded for the ledger */ }
func TestWhatAHintMayNotSay(t *testing.T) {
	// table: a local declaration; an if; an assignment to a src field; an
	// assignment whose LHS is not dst.<Field>; a double assignment.
	// -> FRG3026 / FRG3026 / FRG3026 / FRG3026 / FRG3027, message substrings.
}
```

Note the fixture loader must run over the fixture MODULE with forge's `load`
(which sets the forgespec tag), so hint bodies are kept by Task 5's loader
change. Build `model.Hint` values for the context by finding the fixture's
hint FuncDecl in `pkg.Syntax` the same way the pipeline does.

- [ ] **Step 2: Implement `hint.go`:**

```go
// grammar holds a hint to the narrow shape the layer reads: plain assignments,
// one target member each. Narrow twice over — the left sides are how the
// layer knows what the hint settles, and stage 2 inlines each right side into
// a fused writer, which only a pure expression survives.
func grammar(fn *ast.FuncDecl, target *plugin.Struct, pos token.Position) (map[string]ast.Expr, error) {
	src, dst := paramNames(fn)
	out := make(map[string]ast.Expr)

	for _, stmt := range fn.Body.List {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return nil, plugin.New(codeHintGrammar, pos,
				"%s says more than a hint may: a hint is plain assignments, dst.Member = expression",
				fn.Name.Name).
				WithHint("locals, branches and multiple assignment belong in ordinary code beside the mapper")
		}

		member, ok := dstField(assign.Lhs[0], dst)
		if !ok {
			return nil, plugin.New(codeHintGrammar, pos,
				"%s assigns to something that is not a member of dst", fn.Name.Name).
				WithHint("the left side of every hint assignment is dst.<Member>")
		}
		if _, has := target.Field(member); !has {
			return nil, plugin.New(codeHintGrammar, pos,
				"%s assigns %s, which the target does not declare", fn.Name.Name, member).
				WithHint("%s has %s", "the target", strings.Join(target.FieldNames(), ", "))
		}
		if _, twice := out[member]; twice {
			return nil, plugin.New(codeHintTwiceMember, pos,
				"%s assigns %s twice", fn.Name.Name, member).
				WithHint("one assignment per member; the last word is nobody's in a mapping")
		}
		out[member] = assign.Rhs[0]
	}

	_ = src // src's name is recorded by the caller for respelling
	return out, nil
}

// dstField returns the member name of an assignment target shaped dst.<Member>.
func dstField(lhs ast.Expr, dst string) (string, bool) {
	sel, ok := lhs.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	base, ok := sel.X.(*ast.Ident)
	if !ok || base.Name != dst {
		return "", false
	}
	return sel.Sel.Name, true
}
```

Register locally: `codeHintGrammar = plugin.Register(3026, ...)`,
`codeHintTwiceMember = plugin.Register(3027, "one member assigned twice in a hint")`.
**Registration collision watch:** Task 6 registered 3025/3028/3029/3030 in the
cli/pipeline package; 3026/3027 belong here where the grammar lives. If Task 6
provisionally registered 3026 anywhere, remove it there — `diag.Register`
panics on duplicates, so the first `go test ./...` run tells you.

Respelling (used by Task 10, defined here):

```go
// respelled returns the hint expression with the author's parameter names
// rewritten to the identifiers the constructor binds, leaving selector members
// and map keys alone — dst.Name = src.Name renames both bases and neither
// Name. The tree is cloned by reprinting, so the author's syntax is never
// edited in place.
func respelled(expr ast.Expr, renamed map[string]string) string {
	rewritten := astutil.Apply(expr, func(c *astutil.Cursor) bool {
		if sel, ok := c.Parent().(*ast.SelectorExpr); ok && c.Node() == sel.Sel {
			return false
		}
		if pair, ok := c.Parent().(*ast.KeyValueExpr); ok && c.Node() == pair.Key {
			return false
		}
		ident, ok := c.Node().(*ast.Ident)
		if !ok {
			return true
		}
		if to, rename := renamed[ident.Name]; rename {
			c.Replace(&ast.Ident{Name: to, NamePos: ident.NamePos})
		}
		return true
	}, nil).(ast.Expr)

	return types.ExprString(rewritten)
}
```

(**Do not mutate the author's tree**: replace via `c.Replace` with fresh
idents as above rather than assigning `ident.Name`, because the same AST is
shared with the load session. Add a test asserting the original FuncDecl still
prints its own names after respelling.)

- [ ] **Step 3: Fold into the plan.** In `planned()`, after the ladder: run
  each hint through `grammar`; each returned member either fills an unsettled
  binding (`settledHint`) or overrides a matched one (record the override for
  the ledger); anything still unsettled and unignored → `codeUnsettled`
  listing every such member in one diagnostic ("Name, Age are settled no way")
  with hint "match them by name, assign them in a //forge:map hint, or list
  them in ignore=".

- [ ] **Step 4: Run** `go test ./internal/layers/mapping/ ./... -count=1` (the
  full run catches duplicate code registration). Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "feat: read a mapping's hints and hold them to the grammar"`.

---

### Task 10: Emission, and the compile-and-run gate

**Files:**
- Create: `internal/layers/mapping/write.go`, `internal/layers/mapping/locals.go`
  (copy the jsoncodec wrapper: `internal/layers/jsoncodec/locals.go:20-55`,
  package-adjusted)
- Modify: `internal/layers/mapping/mapping.go` (real `Generate`)
- Create: `internal/layers/mapping/mapping_test.go` compile-and-run driver +
  `internal/layers/mapping/testdata/reference.go.txt` (embedded reference test)

**Interfaces:**
- Consumes: `planned(ctx)` (Task 8/9), `respelled` (Task 9).
- Produces: `Generate` returning a `plugin.Unit` whose `Decls` hold, in order:
  the declared empty struct (spec form only — check `ctx.Model.Form`), and the
  constructor. Constructor name: `plugin.Upper(targetName) + "From" + plugin.Upper(sourceName)`.
  Signature `func PersonFromUser(src *User) Person` — for an interface source,
  `func CardFromReader(src Reader) Card`.

- [ ] **Step 1: Write the reference fixture test** (`testdata/reference.go.txt`,
  embedded like `internal/layers/jsoncodec/testdata/agreement.go.txt` via
  `fixture_test`-style `//go:embed`): a real `_test.go` body for the fixture
  module comparing generated constructors against hand-written expectations:

```go
package model

import "testing"

// byHand is the mapping a person would have written, which the generated one
// has to equal member for member.
func byHand(src *User) Person {
	return Person{ID: src.ID, Email: src.Email, Name: src.User_Name(), Age: src.Age}
}

func TestTheConstructorAgreesWithTheHandWrittenOne(t *testing.T) {
	src := User{ID: 7, Email: "a@b", Age: 40}
	if got, want := PersonFromUser(&src), byHand(&src); got != want {
		t.Errorf("the constructor built %+v, want %+v", got, want)
	}
}

// ... one Test per fixture pair, including the interface source and the
// hinted pair, each against its own byHand twin.
```

- [ ] **Step 2: Write the driver** (mirror
  `internal/layers/jsoncodec/codec_test.go:37-57` exactly: `t.TempDir()`,
  `copied`, `write` the generated file as `model/zz_map.go` and the reference
  as `model/reference_test.go`, `exec go test ./...` with `GOFLAGS=-mod=mod`,
  `testing.Short()` skip). Generation drives `mapping.New().Generate` with a
  hand-built `plugin.Context` per pair: `Model: &plugin.Model{Name, Form: plugin.FormSpec, Subject: builtT, Source: sTypesType, Hints: [...], Pkg, Pos}`
  plus `Options` carrying `ignore` where a fixture needs it (build
  `model.Options` the way `internal/generate/shadow_test.go:154-206` builds a
  directive).

- [ ] **Step 3: Implement `write.go`.** Text assembly, one fresh fset, in the
  house style (`internal/layers/validate/write.go` is the model). The whole
  emission:

```go
// written assembles the constructor and the declared type.
func written(ctx *plugin.Context, built *plan) (plugin.Unit, error) {
	names := naming(append(spelledTypes(ctx, built), ctx.Model.Name)...)
	src := names.name("src")
	held := names.name("held")

	w := &writer{} // the same 20-line writer every layer carries: line/blank/String
	sourceSpelled := plugin.Spell(built.source, ctx.Model.Pkg.PkgPath, ctx.Bound())
	targetSpelled := ctx.Model.SubjectSpelling(sourceSpelled.Bound(ctx.Bound()))
	name := constructorName(built, ctx)

	if ctx.Model.Form == plugin.FormSpec {
		w.line("// %s is the mapping's declaration; the constructor beside it is", ctx.Model.Name)
		w.line("// what it produces. It holds nothing.")
		w.line("type %s struct{}", ctx.Model.Name)
		w.blank()
	}

	w.line("// %s builds a %s from what a %s holds.", name, targetSpelled.Text, sourceSpelled.Text)
	w.line("//")
	w.line("// %s", ledger(built)) // "Matched by name: ID, Email, Age. Folded: Name (User_Name). From the hint: … . Ignored: …"
	w.line("func %s(%s %s) %s {", name, src, sourceParam(built, sourceSpelled.Text), targetSpelled.Text)
	w.line("%s := %s{", held, targetSpelled.Text)
	for _, member := range built.members {
		switch member.via {
		case settledField:
			w.line("%s: %s.%s,", member.field.Name, src, member.from)
		case settledMethod:
			w.line("%s: %s.%s(),", member.field.Name, src, member.from)
		}
	}
	w.line("}")
	for _, member := range built.members {
		if member.via == settledHint {
			w.line("%s.%s = %s", held, member.field.Name,
				respelled(member.hint, map[string]string{member.srcName: src, member.dstName: held}))
		}
	}
	w.line("return %s", held)
	w.line("}")

	decls, comments, fset, err := parsed(w.String(), ctx.Model.Name)
	if err != nil {
		return plugin.Unit{}, err
	}

	imports := append(sourceSpelled.Imports, targetSpelled.Imports...)
	return plugin.Unit{
		Decls: decls, Comments: comments, Fset: fset,
		Imports: plugin.Reaching(decls, toImports(imports)),
	}, nil
}
```

with `sourceParam` returning `"*"+spelled` for a struct source and the bare
spelling for an interface; `parsed` copied from enum (`enum.go:187-196`,
package-name adjusted); `constructorName(built, ctx)` =
`plugin.Upper(targetName) + "From" + plugin.Upper(sourceName)` where the names
are `Named.Obj().Name()` of each side; `spelledTypes` gathering both spellings
plus every member type spelling for the locals seed; the ledger a single
sentence assembled from the members, override notes included ("the hint
overrides the match on Name"). Hint bindings need `srcName`/`dstName` (the
author's parameter spellings) carried on the binding from Task 9 — add those
two fields there if not already present.

- [ ] **Step 4: Wire `Generate`:**

```go
func (Layer) Generate(ctx *plugin.Context, _ plugin.Shape) (plugin.Unit, error) {
	if ctx == nil || ctx.Model == nil || ctx.Model.Subject == nil || ctx.Model.Source == nil {
		return plugin.Unit{}, errors.New("map: asked to generate without a modelled declaration")
	}
	built, err := planned(ctx)
	if err != nil {
		return plugin.Unit{}, err
	}
	return written(ctx, built)
}
```

- [ ] **Step 5: Run everything.**

Run: `go test ./internal/layers/mapping/ -count=1` (includes the temp-module
run), then `go test ./... -count=1`.
Expected: PASS.

- [ ] **Step 6: Commit** `git commit -m "feat: write the bridge's constructor, ledger and all"`.

---

### Task 11: The shadow row and the whole gate

**Files:**
- Modify: `internal/generate/shadow_test.go:60-80` (the map row) and its
  `shadowing`/`shadowSource` helpers (they must set `Model.Source` and NOT
  declare the type inline)
- Modify: `docs/diagnostics.md` (rows for 1009, 2034–2038, 3025–3031)
- Test: the full gate

**Interfaces:**
- Consumes: everything above.

- [ ] **Step 1: The shadow row.** Add to `theBound`:

```go
	{layer: "map", stack: []string{"Map"}, names: []string{"src", "dst", "held"}},
```

and teach `shadowing()` to build a bridge request when the stack is `["Map"]`:
the same hand-built `model.Struct` serves as target; add a second
`types.NewNamed` for a source type with one `ID int` field... **verify how the
generated constructor's locals interact with the shadow subject name** — the
row's `names` must be exactly the identifiers `written()` binds (`src`,
`held`, and `dst` only if emission ever binds it — if only hints name dst and
hints are absent in the shadow case, keep `dst` in the row anyway: the locals
allocation must still move it, and the case then asserts the allocation, not
the emission). Run and adjust until each name-case generates and compiles.

- [ ] **Step 2: diagnostics.md rows** — one line per new code, matching the
  file's existing table format (read the 2031–2033 rows added recently).

- [ ] **Step 3: The whole gate.**

Run, in order, and fix anything that falls out:

```bash
go test ./... -count=1
make check          # fmt, vet (incl. forgespec), lint, cover >= 90, size
go test -race ./internal/layers/mapping/ ./internal/generate/ -count=1
make example        # must be a no-op diff: examples declare no Map yet
make fresh
git status --short  # clean
```

Coverage note: if the global gate dips below 90, the uncovered lines are
almost certainly refusal branches — extend the refused fixture table rather
than writing filler tests.

- [ ] **Step 4: Commit** `git commit -m "test: hold the map layer to the shadow and the gate"`.

- [ ] **Step 5: Update the plan's own checkboxes and stop.** Stage 2 (the
  `Map[S, Json[T]]` fusion) is a separate plan, written after this one ships:
  it will relax the `bridges` compose rule to admit exactly `[Map, Json]`,
  reuse the jsoncodec emitters with member paths rewritten from the plan's
  bindings, and gate on byte-equality with `From` + `AppendJSON`.

---

## Self-Review (performed while writing)

- **Spec coverage:** §1 marker+constructor → Tasks 1,3,4,10. §2 ladder,
  ignore, unexported-target rule → Task 8. §3 hints (spec-file, compiled,
  grammar, override, one-per-declaration) → Tasks 5,6,9. §4 refusal table →
  Tasks 8,9 (codes 2034–2038, 3026–3031; "S or T not a struct/interface" is
  2034 plus the subject builder's own 2001-series for T). §5 ledger + empty
  declared type + no-error contract → Task 10. §7 plumbing (two-parameter
  marker, hint discovery, naming shield, ledger-by-execution) → Tasks 2,3,5,6,
  locals in Task 10, compile-and-run in Task 10. §8 test regime → Tasks 8–11.
- **Known uncertainty, flagged in-task:** go/types iterator spellings
  (`Struct.Fields()`, `MethodSet` iteration) and rules.go registration shape —
  each task says to verify against the file before trusting the sketch.
- **Type consistency:** `binding` gains `srcName`/`dstName` in Task 9 and is
  consumed by Task 10's `respelled` call — declared in Task 8's struct? No:
  added in Task 9, used in Task 10, both say so.
