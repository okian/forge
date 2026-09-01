package discover_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
)

// loadFixture loads the fixture module the tests share.
func loadFixture(t *testing.T) *load.Session {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("testdata", "decls"))
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
	return session
}

// names returns the candidates' declared names.
func names(candidates []discover.Candidate) []string {
	out := make([]string, len(candidates))
	for i, c := range candidates {
		out[i] = c.Name
	}
	return out
}

// find returns the candidate with the given name.
func find(t *testing.T, candidates []discover.Candidate, name string) discover.Candidate {
	t.Helper()

	for _, c := range candidates {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("candidate %s not found; got %v", name, names(candidates))
	return discover.Candidate{}
}

// The fixture holds an inline declaration, a spec declaration, an alias, a
// declaration that is not an instantiation, and instantiations written outside
// any declaration. Only the first two kinds are generation requests.
func TestDeclarationsPicksOutTheRightShapes(t *testing.T) {
	candidates, _ := discover.Declarations(loadFixture(t))

	got := names(candidates)
	// Ordered by package import path first, then by file and position within it.
	//
	// Sessions is in another generator's output, which forge reads: only what
	// forge itself wrote is passed over, and the file declaring Seq is the one
	// here that qualifies.
	want := []string{
		"Items",
		"Numbers", "Trailing", "Sessions", "People", "Persons", "Recent", "Undirected",
		"First", "Second", "Detached", "Spaced", "Blocked",
	}

	if !slices.Equal(got, want) {
		t.Fatalf("Declarations() = %v, want %v", got, want)
	}

	// Each exclusion is worth naming, because each is a different reason.
	for _, excluded := range []string{"Aliased", "Plain", "Loose", "Person", "Box", "Item", "Lookup", "registry"} {
		if slices.Contains(got, excluded) {
			t.Errorf("%s is a candidate; it should not be", excluded)
		}
	}
}

// An instantiation of a generic type the author declared is a candidate here
// and stops being one at resolution, which is the only stage that can follow it
// to its origin and see that no layer claims it.
func TestUnrelatedInstantiationsSurviveThisStage(t *testing.T) {
	candidates, _ := discover.Declarations(loadFixture(t))

	numbers := find(t, candidates, "Numbers")
	if got, want := numbers.String(), "Numbers Box[int]"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if len(numbers.Directives) != 0 {
		t.Errorf("Numbers carries directives: %v", numbers.Directives)
	}
}

// Which file a declaration lives in decides whether forge adds methods to the
// author's type or owns the declaration outright, and nothing downstream can
// work it out on its own.
func TestFormFollowsTheFile(t *testing.T) {
	candidates, _ := discover.Declarations(loadFixture(t))

	cases := map[string]model.Form{
		"Items":      model.FormInline,
		"People":     model.FormInline,
		"Numbers":    model.FormInline,
		"Persons":    model.FormSpec,
		"Recent":     model.FormSpec,
		"Undirected": model.FormSpec,
	}

	for name, want := range cases {
		if got := find(t, candidates, name).Form; got != want {
			t.Errorf("%s has form %s, want %s", name, got, want)
		}
	}
}

func TestDirectivesAreCollectedInOrder(t *testing.T) {
	candidates, _ := discover.Declarations(loadFixture(t))

	persons := find(t, candidates, "Persons")
	if len(persons.Directives) != 3 {
		t.Fatalf("Persons carries %d directives, want 3: %v", len(persons.Directives), persons.Directives)
	}

	want := []struct {
		layer string
		args  string
	}{
		{"collection", "sort=Age,LastName index=Name"},
		{"ring", "cap=1024 overflow=overwrite"},
		{"json", "omitzero=true"},
	}

	for i, w := range want {
		got := persons.Directives[i]
		if got.Layer != w.layer {
			t.Errorf("Directives[%d].Layer = %q, want %q", i, got.Layer, w.layer)
		}
		if got.Args != w.args {
			t.Errorf("Directives[%d].Args = %q, want %q", i, got.Args, w.args)
		}
	}

	// Prose in the doc comment is not a directive, and neither is a directive
	// meant for another tool.
	people := find(t, candidates, "People")
	if len(people.Directives) != 1 || people.Directives[0].Layer != "collection" {
		t.Errorf("People carries %v, want one collection directive", people.Directives)
	}
}

// A declaration inside a parenthesised type group gets its directives from its
// own comment, because the group's comment sits above several declarations and
// could not say which one it means.
func TestDirectivesInsideATypeGroup(t *testing.T) {
	candidates, _ := discover.Declarations(loadFixture(t))

	recent := find(t, candidates, "Recent")
	if len(recent.Directives) != 1 {
		t.Fatalf("Recent carries %d directives, want 1: %v", len(recent.Directives), recent.Directives)
	}
	if got, want := recent.Directives[0].Args, "cap=16"; got != want {
		t.Errorf("Recent's directive args = %q, want %q", got, want)
	}

	if undirected := find(t, candidates, "Undirected"); len(undirected.Directives) != 0 {
		t.Errorf("Undirected carries directives it did not declare: %v", undirected.Directives)
	}
}

// Every diagnostic about a declaration points at the declared name, so the
// position has to be the name's and not the keyword's.
func TestPositionIsTheDeclaredName(t *testing.T) {
	candidates, _ := discover.Declarations(loadFixture(t))

	persons := find(t, candidates, "Persons")
	if filepath.Base(persons.Pos.Filename) != "spec.go" {
		t.Errorf("Persons is at %s, want spec.go", persons.Pos.Filename)
	}
	if got, want := persons.Pos.Column, len("type ")+1; got != want {
		t.Errorf("Persons is at column %d, want %d — the name, not the keyword", got, want)
	}

	// The directives are consecutive lines sitting immediately above the
	// declaration, which is what a diagnostic pointing at one of them relies on.
	for i, directive := range persons.Directives {
		want := persons.Pos.Line - len(persons.Directives) + i
		if directive.Pos.Line != want {
			t.Errorf("Directives[%d] is at line %d, want %d", i, directive.Pos.Line, want)
		}
		if directive.Pos.Column != 1 {
			t.Errorf("Directives[%d] is at column %d, want 1", i, directive.Pos.Column)
		}
	}

	// A diagnostic about one option points at the option, not at the line, so
	// the offset of the arguments within the comment has to be right.
	collection := persons.Directives[0]
	if got := collection.Text[collection.ArgsOffset:]; got != collection.Args {
		t.Errorf("ArgsOffset lands on %q, want %q", got, collection.Args)
	}
	if got, want := collection.ArgsPos().Column, collection.Pos.Column+collection.ArgsOffset; got != want {
		t.Errorf("ArgsPos() column = %d, want %d", got, want)
	}
}

// Walking the candidates has to give the same answer every run, or everything
// derived from walking them inherits the variation.
func TestDeclarationsAreOrdered(t *testing.T) {
	session := loadFixture(t)

	initial, _ := discover.Declarations(session)
	first := names(initial)
	for range 3 {
		again, _ := discover.Declarations(session)
		if got := names(again); !slices.Equal(got, first) {
			t.Fatalf("Declarations() returned %v then %v", first, got)
		}
	}

	// Within a package the order is by file and then by position, which is the
	// order an author reads them in.
	candidates, _ := discover.Declarations(session)
	for i := 1; i < len(candidates); i++ {
		before, after := candidates[i-1], candidates[i]
		if before.Pos.Filename != after.Pos.Filename {
			continue
		}
		if before.Pos.Offset >= after.Pos.Offset {
			t.Errorf("%s and %s are out of order within %s", before.Name, after.Name, after.Pos.Filename)
		}
	}
}

func TestDeclarationsOnNothing(t *testing.T) {
	got, diags := discover.Declarations(nil)
	if got != nil {
		t.Errorf("Declarations(nil) = %v, want nil", got)
	}
	if !diags.Empty() {
		t.Errorf("Declarations(nil) reported %s", diags.Render())
	}
}

// A directive that lands on nothing is the failure this stage exists to catch.
// The declaration is still generated, with its options quietly defaulted, so
// the author gets a wrong result rather than a missing one — which is the kind
// nobody thinks to look for.
func TestReportsDirectivesThatLandOnNothing(t *testing.T) {
	_, diags := discover.Declarations(loadFixture(t))

	if diags.Empty() {
		t.Fatal("no stray directives reported")
	}

	rendered := diags.Render()

	// Every shape an author can produce, each for a different reason.
	cases := map[string]string{
		"above a declaration that is not an instantiation": "FRG3001",
		"above an alias":                                 "FRG3001",
		"above a group of several declarations":          "FRG3001",
		"separated from its declaration by a blank line": "FRG3001",
		"written with a space after the marker":          "FRG3002",
		"written as a block comment":                     "FRG3002",
	}
	for name, code := range cases {
		if !strings.Contains(rendered, code) {
			t.Errorf("nothing reported for a directive %s (want %s):\n%s", name, code, rendered)
		}
	}

	var notAttached, malformed int
	for _, d := range diags.All() {
		switch d.Code.String() {
		case "FRG3001":
			notAttached++
		case "FRG3002":
			malformed++
		}
		if d.Hint == "" {
			t.Errorf("%s carries no hint", d.Code)
		}
		if d.Pos.Line == 0 {
			t.Errorf("%s has no position", d.Code)
		}
	}

	if notAttached != 4 {
		t.Errorf("reported %d unattached directives, want 4:\n%s", notAttached, rendered)
	}
	if malformed != 2 {
		t.Errorf("reported %d malformed directives, want 2:\n%s", malformed, rendered)
	}
}

// A directive written after the declaration on the same line is attached to it
// by the parser, and losing it would leave the declaration misconfigured with
// nothing said.
func TestTrailingDirectiveIsAttached(t *testing.T) {
	candidates, diags := discover.Declarations(loadFixture(t))

	trailing := find(t, candidates, "Trailing")
	if len(trailing.Directives) != 1 {
		t.Fatalf("Trailing carries %d directives, want 1: %v", len(trailing.Directives), trailing.Directives)
	}
	if got, want := trailing.Directives[0].Args, "cap=4"; got != want {
		t.Errorf("Trailing's directive args = %q, want %q", got, want)
	}

	// Having been claimed, it is not also reported as landing on nothing.
	if strings.Contains(diags.Render(), "cap=4") {
		t.Errorf("an attached trailing directive was also reported as stray:\n%s", diags.Render())
	}
}

// Forge's own output is not input. Generated code holds declarations that look
// exactly like requests — the shared sequence view is a defined type over an
// instantiation, which is the shape a candidate is recognised by — so a run
// that read it back would find a declaration nobody wrote, in a file the author
// does not edit, that the run before it created.
//
// The fixture carries one such file, so this fails if the rule goes away rather
// than merely passing where nothing tries it.
func TestForgeDoesNotReadItsOwnOutput(t *testing.T) {
	found, _ := discover.Declarations(loadFixture(t))

	for _, one := range found {
		if strings.HasPrefix(filepath.Base(one.Pos.Filename), "zz_forge_") {
			t.Errorf("%s was found in %s, which forge wrote", one.Name, one.Pos.Filename)
		}
	}

	if slices.Contains(names(found), "Seq") {
		t.Errorf("the shared view was read back as a declaration; found %v", names(found))
	}
}

// Only what forge wrote, and not everything that says it was generated.
//
// Another generator's output may legitimately hold a declaration written
// against these markers — generating a schema and then generating from it is an
// ordinary arrangement — so refusing every file carrying the convention's line
// would break that, silently, for anybody who had built it. The narrowness is
// the deliberate half of the rule, and it is the half a reader reaching for
// go/build's own predicate would remove.
func TestSomebodyElsesGeneratedCodeIsStillRead(t *testing.T) {
	found, _ := discover.Declarations(loadFixture(t))

	if !slices.Contains(names(found), "Sessions") {
		t.Errorf("a declaration in another generator's output was passed over; found %v", names(found))
	}
}

// A directive written above a field is not reported as landing on nothing.
//
// It lands on the field, which this stage does not read: what reads it is the
// stage that walks the subject, where a field is a field rather than a line of
// syntax. Claiming it here is on that stage's behalf, and the claim has to be
// made — unclaimed, every correctly written field option is reported as
// applying to nothing, and the only advice forge could give is to delete the
// one thing that would have worked.
func TestADirectiveAboveAFieldLandsOnIt(t *testing.T) {
	_, diags := discover.Declarations(loadFixture(t))

	rendered := diags.Render()
	if strings.Contains(rendered, "fallback=stdlib") {
		t.Errorf("a directive above a field was reported as landing on nothing:\n%s", rendered)
	}
}
