package resolve_test

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"slices"
	"testing"

	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/resolve"
)

// fixtureMarkers is the marker package the arity fixture is written against,
// which is not the one forge ships.
const fixtureMarkers = "stacksfixture/markers"

// candidates loads the fixture module and discovers its declarations, which is
// the input resolution takes.
func candidates(t *testing.T) []discover.Candidate {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("testdata", "stacks"))
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}

	session, err := load.Load(load.Config{Dir: dir})
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if !session.Diagnostics.Empty() {
		t.Fatalf("fixture does not load clean:\n%s", session.Diagnostics.Render())
	}

	found, diags := discover.Declarations(session)
	if !diags.Empty() {
		t.Fatalf("fixture does not discover clean:\n%s", diags.Render())
	}
	return found
}

// resolved resolves the fixture against the markers forge ships and fails if
// anything was reported.
func resolved(t *testing.T) []resolve.Declaration {
	t.Helper()

	found, diags := resolve.Declarations(candidates(t), forges)
	if !diags.Empty() {
		t.Fatalf("fixture does not resolve clean:\n%s", diags.Render())
	}
	return found
}

// names returns the declared names, in the order they were resolved.
func names(decls []resolve.Declaration) []string {
	out := make([]string, len(decls))
	for i, decl := range decls {
		out[i] = decl.Candidate.Name
	}
	return out
}

// find returns the resolved declaration with the given name.
func find(t *testing.T, decls []resolve.Declaration, name string) resolve.Declaration {
	t.Helper()

	for _, decl := range decls {
		if decl.Candidate.Name == name {
			return decl
		}
	}
	t.Fatalf("declaration %s was not resolved; got %v", name, names(decls))
	return resolve.Declaration{}
}

// Stacks one to four layers deep, with the subject at the bottom of each.
func TestStacksResolveToTheDeclarationTheyName(t *testing.T) {
	decls := resolved(t)

	cases := map[string]string{
		"Encoded":  "Encoded Json[Person]",
		"People":   "People Collection[Person]",
		"Recent":   "Recent Collection[Ring[Person]]",
		"Streams":  "Streams Collection[Ring[Json[Person]]]",
		"Persons":  "Persons Guarded[Collection[Ring[Json[Person]]]]",
		"Sessions": "Sessions Collection[Session]",
	}

	for name, want := range cases {
		if got := find(t, decls, name).String(); got != want {
			t.Errorf("%s resolved to %q, want %q", name, got, want)
		}
	}
}

// A marker written through a dot import and one written through a package name
// are the same type by the time resolution sees them, so the two spellings must
// resolve identically.
func TestBothImportSpellingsResolveTheSame(t *testing.T) {
	decls := resolved(t)

	qualified := find(t, decls, "People")
	dotted := find(t, decls, "Guests")

	if !slices.Equal(qualified.Stack, dotted.Stack) {
		t.Errorf("Guests resolved to %v, want People's %v", dotted.Stack, qualified.Stack)
	}
	if qualified.Subject != dotted.Subject {
		t.Errorf("Guests is specialised to %s, want People's %s", dotted.Subject, qualified.Subject)
	}
}

// An alias in the middle of a stack names an instantiation, and resolution has
// to see through it or lose the layer it names.
func TestAnAliasedLayerIsFollowedThrough(t *testing.T) {
	decls := resolved(t)

	if got, want := find(t, decls, "Buffered").String(), "Buffered Collection[Ring[Person]]"; got != want {
		t.Errorf("Buffered resolved to %q, want %q", got, want)
	}
}

// An alias at the bottom of a stack has to be resolved too, and this one is not
// about the rendering: the stage that builds the subject model type-asserts
// what it is handed, so an alias reaching it fails as a missing subject rather
// than as an alias.
func TestAnAliasedSubjectIsFollowedThrough(t *testing.T) {
	subject := find(t, resolved(t), "Aliased").Subject

	named, ok := subject.(*types.Named)
	if !ok {
		t.Fatalf("Aliased is specialised to a %T, want a *types.Named", subject)
	}
	if got, want := named.Obj().Name(), "Person"; got != want {
		t.Errorf("Aliased is specialised to %s, want %s", got, want)
	}
}

// Whatever was written innermost is reported as written. A pointer, a basic
// type and a type parameter are all illegal subjects, and none of them is
// resolution's to reject: the stage that does has to be able to name what it
// found.
func TestTheSubjectIsWhateverWasWrittenInnermost(t *testing.T) {
	decls := resolved(t)

	cases := map[string]string{
		"Pointers": "Pointers Collection[*Person]",
		"Degrees":  "Degrees Collection[int]",
		"Wrapper":  "Wrapper Collection[T]",
		"Pairs":    "Pairs Collection[Pairing[string, int]]",
	}

	for name, want := range cases {
		if got := find(t, decls, name).String(); got != want {
			t.Errorf("%s resolved to %q, want %q", name, got, want)
		}
	}
}

// A declaration over a generic type of the author's own is an ordinary Go
// declaration. Dropping it is right; saying anything about it is not.
func TestDeclarationsNamingNoMarkerAreDroppedInSilence(t *testing.T) {
	decls, diags := resolve.Declarations(candidates(t), forges)

	if !diags.Empty() {
		t.Fatalf("resolution reported something:\n%s", diags.Render())
	}

	got := names(decls)
	for _, dropped := range []string{"Counts", "Wrapped", "Pipes", "Names", "Opaques"} {
		if slices.Contains(got, dropped) {
			t.Errorf("%s was resolved; no marker claims its outermost type", dropped)
		}
	}
}

// Resolution preserves discovery's order, which is already deterministic, so
// nothing downstream has to sort again to keep output stable.
func TestOrderFollowsTheCandidates(t *testing.T) {
	got := names(resolved(t))
	want := []string{
		// Package model, file by file: dot.go, then person.go, then spec.go.
		"Guests",
		"People", "Recent", "Sessions", "Pairs", "Pointers", "Degrees", "Wrapper", "Buffered", "Aliased",
		"Streams", "Persons", "Encoded",
	}

	if !slices.Equal(got, want) {
		t.Fatalf("Declarations() = %v, want %v", got, want)
	}
}

// Resolution records which marker a stack entry names and nothing else. The
// kind belongs to the registry, and an entry nobody wrote belongs to the stage
// that inserts the default storage.
func TestStackEntriesCarryOriginsOnly(t *testing.T) {
	stack := find(t, resolved(t), "Recent").Stack

	want := []model.LayerRef{
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Collection"}},
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Ring"}},
	}

	if !slices.Equal(stack, want) {
		t.Fatalf("Recent's stack is %v, want %v", stack, want)
	}
	for _, ref := range stack {
		if ref.Kind != model.KindInvalid {
			t.Errorf("%s carries kind %s; the registry has not been consulted", ref.Origin.Name, ref.Kind)
		}
		if ref.Implicit {
			t.Errorf("%s is marked inferred; it was written", ref.Origin.Name)
		}
	}
}

// A layer takes exactly one type argument, and a marker package forge does not
// ship is the only place a second one can come from.
func TestMarkerWithTwoTypeArgumentsIsReported(t *testing.T) {
	decls, diags := resolve.Declarations(candidates(t), fixture)

	// The declarations beside it still resolve, so the report is about the one
	// declaration and not about the package.
	if got, want := names(decls), []string{"Names", "Opaques"}; !slices.Equal(got, want) {
		t.Fatalf("Declarations() = %v, want %v", got, want)
	}
	if got, want := decls[0].String(), "Names Collection[string]"; got != want {
		t.Errorf("Names resolved to %q, want %q", got, want)
	}

	all := diags.All()
	if len(all) != 2 {
		t.Fatalf("resolution reported %d diagnostics, want 2:\n%s", len(all), diags.Render())
	}

	// Written at the top of a stack and written under one, because the caret
	// has to count the layers above the marker it underlines.
	cases := []struct {
		decl  string
		line  int
		stack string
		caret string
	}{
		{decl: "Pipes", line: 8, stack: "Pipeline[string, int]", caret: "^^^^^^^^"},
		{decl: "Nested", line: 20, stack: "Collection[Pipeline[string, int]]", caret: "           ^^^^^^^^"},
	}

	for i, want := range cases {
		got := all[i]

		if code := "FRG1007"; got.Code.String() != code {
			t.Errorf("%s: code is %s, want %s", want.decl, got.Code, code)
		}
		if message := "layer Pipeline is written with 2 type arguments"; got.Message != message {
			t.Errorf("%s: message is %q, want %q", want.decl, got.Message, message)
		}
		// The offending marker is spelled with its type arguments, so the count
		// the message reports is visible in the line the caret points at.
		if got.Stack != want.stack {
			t.Errorf("%s: stack line is %q, want %q", want.decl, got.Stack, want.stack)
		}
		if got.Caret != want.caret {
			t.Errorf("%s: caret is %q, want %q", want.decl, got.Caret, want.caret)
		}
		if got.Hint == "" {
			t.Errorf("%s: the diagnostic carries no hint", want.decl)
		}
		// The position is the declaration's own, in the file the author edits.
		if file := "arity.go"; filepath.Base(got.Pos.Filename) != file {
			t.Errorf("%s: position is in %s, want %s", want.decl, got.Pos.Filename, file)
		}
		if got.Pos.Line != want.line || got.Pos.Column != 6 {
			t.Errorf("%s: position is %d:%d, want %d:6", want.decl, got.Pos.Line, got.Pos.Column, want.line)
		}
	}
}

// A type from the marker package that is not generic was never applied to
// anything, so it is where a stack ends and not a layer written wrong.
func TestANonGenericMarkerTypeIsASubject(t *testing.T) {
	decls, _ := resolve.Declarations(candidates(t), fixture)

	if got, want := find(t, decls, "Opaques").String(), "Opaques Collection[Opaque]"; got != want {
		t.Errorf("Opaques resolved to %q, want %q", got, want)
	}
}

// Resolution runs over whatever discovery hands it, and a candidate with
// nothing to follow is dropped rather than panicked over.
func TestCandidatesWithNothingToFollowAreDropped(t *testing.T) {
	unresolvable := find(t, resolved(t), "People").Candidate
	// A right-hand side the type-checker never saw, which is what a package too
	// broken to type-check leaves behind.
	unresolvable.Spec = &ast.TypeSpec{Name: ast.NewIdent("People"), Type: ast.NewIdent("Missing")}

	decls, diags := resolve.Declarations([]discover.Candidate{{Name: "Empty"}, unresolvable}, forges)

	if len(decls) != 0 {
		t.Errorf("Declarations() = %v, want none", names(decls))
	}
	if !diags.Empty() {
		t.Errorf("resolution reported something:\n%s", diags.Render())
	}
}

// The zero value renders rather than panicking, because a stringer that only
// works on well-formed input is no use in the debugger where it is reached for.
func TestTheZeroDeclarationRenders(t *testing.T) {
	if got, want := (resolve.Declaration{}).String(), "?"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// forges claims the markers forge ships, which is what its own registry
// answers.
//
// A predicate rather than a registry, because that is what resolution takes:
// what it asks is which of the types in a nested instantiation a layer claims,
// and a test saying "the ones from this package" is saying it exactly.
func forges(ref model.TypeRef) bool { return ref.Pkg == model.MarkerPkg }

// fixture claims the markers the fixture module declares, which is how a rule
// no forge marker can break is reached: every one of them takes a single type
// argument, so a marker that takes two has to come from somewhere else.
func fixture(ref model.TypeRef) bool { return ref.Pkg == fixtureMarkers }
