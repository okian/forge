package clone_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/model"
)

// The copy opens by assigning, which is already the whole of it for a subject
// holding nothing but values.
//
// It is worth pinning because the alternative reads as more thorough and is
// worse: a copy that assigned every field one at a time would say the same
// thing in twenty lines, and would say nothing at all about which of them
// needed saying.
func TestASubjectOfValuesIsCopiedByAssignment(t *testing.T) {
	held := source(t, written(t, "Flat"))

	if !strings.Contains(held, "out := v\n\treturn out") {
		t.Errorf("a subject of values is not copied by assignment alone:\n%s", held)
	}
}

// Each of the three things an assignment would have shared is built again.
func TestWhatAnAssignmentWouldHaveShared(t *testing.T) {
	held := source(t, written(t, "Referring"))

	for _, want := range []string{
		"out.Tags = slices.Clone(v.Tags)",
		"out.Lookup = maps.Clone(v.Lookup)",
		"if v.Count != nil {\n\t\theld := (*v.Count)\n\t\tout.Count = &held\n\t}",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the copy does not hold %q:\n%s", want, held)
		}
	}
}

// A reference whose elements are themselves references is copied all the way
// down rather than one level.
//
// One level is the mistake this layer exists to write out. A slice of slices
// cloned once shares every inner slice, which looks like a deep copy until
// somebody writes to one.
func TestACopyGoesAllTheWayDown(t *testing.T) {
	held := source(t, written(t, "Deep"))

	for _, want := range []string{
		"out.Rows[i] = slices.Clone(one)",
		"out.Nested[key] = slices.Clone(one)",
		"out.Fixed[i] = slices.Clone(one)",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the copy does not hold %q:\n%s", want, held)
		}
	}

	// A pointer to a pointer is followed twice, and each level asked about
	// separately, because either of them may be nil.
	if !strings.Contains(held, "if v.Deeper != nil {") || !strings.Contains(held, "if (*v.Deeper) != nil {") {
		t.Errorf("a pointer to a pointer is not followed twice:\n%s", held)
	}
}

// A struct another struct holds is copied by its own method, however it is
// held.
//
// Which is what makes the copy one method per type rather than one enormous
// function: the method is written once and reached from wherever the type is.
func TestAStructIsCopiedByItsOwn(t *testing.T) {
	held := source(t, written(t, "Holding"))

	for _, want := range []string{
		"out.Home = v.Home.Clone()",
		"held := (*v.Work).Clone()",
		"out.Past[i] = one.Clone()",
		"out.ByName[key] = one.Clone()",
		"out.Windows[i] = one.Clone()",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the copy does not hold %q:\n%s", want, held)
		}
	}
}

// A dereference is parenthesised, because it binds looser than the selector
// after it.
//
// Without the parentheses *p.Clone() is a dereference of what p.Clone()
// answered with, which is a different thing and usually not a thing at all.
func TestADereferenceBindsBeforeTheCall(t *testing.T) {
	held := source(t, written(t, "Holding"))

	if strings.Contains(held, "*v.Work.Clone()") {
		t.Errorf("a dereference is written where the selector would take it first:\n%s", held)
	}
}

// A type that reaches itself produces a method that calls itself, which is a
// finite amount of code however deep the values go.
func TestATypeThatReachesItselfTerminates(t *testing.T) {
	held := source(t, written(t, "Node"))

	if !strings.Contains(held, "held := (*v.Next).Clone()") {
		t.Errorf("a self-referential field is not copied by the type's own method:\n%s", held)
	}
	if strings.Count(held, "func (v Node) Clone() Node {") != 1 {
		t.Errorf("the method was written more than once:\n%s", held)
	}
}

// A type whose author wrote the copy is called rather than written a second
// time.
func TestAHandWrittenCopyStaysAuthoritative(t *testing.T) {
	held := source(t, written(t, "Owning"))

	if !strings.Contains(held, "out.Held = v.Held.Clone()") {
		t.Errorf("a type that copies itself is not called:\n%s", held)
	}
	if strings.Contains(held, "func (v Counter) Clone()") {
		t.Errorf("a copy the author wrote was written a second time:\n%s", held)
	}
}

// A field asked to be carried across is left to the assignment, and the rest of
// the subject is still copied.
func TestWhatSharingLeavesAlone(t *testing.T) {
	held := source(t, written(t, "Marked"))

	if !strings.Contains(held, "out.Copied = slices.Clone(v.Copied)") {
		t.Errorf("sharing one field stopped the others being copied:\n%s", held)
	}
	if strings.Contains(held, "v.Shared") {
		t.Errorf("a field asked to be shared was copied anyway:\n%s", held)
	}
}

// A declaration that asked for sharing shares every reference, and the copy is
// then the assignment alone.
func TestSharingOnTheDeclaration(t *testing.T) {
	held := source(t, sharing(t, "Referring"))

	if !strings.Contains(held, "out := v\n\treturn out") {
		t.Errorf("a declaration that asked to share still copied:\n%s", held)
	}
}

// A struct the subject reaches in a package of its own is copied by a function
// rather than by a method, and everything holding one calls that function.
//
// Go puts a method only where its type is, so a copy written as a method there
// is not a copy that is missing something — it is a file that does not compile.
func TestAStructInAnotherPackageIsCopiedByAFunction(t *testing.T) {
	held := source(t, written(t, "Elsewhere"))

	if !strings.Contains(held, "func cloneOtherPlace(v other.Place) other.Place {") {
		t.Errorf("the copy for a struct of another package is not a function:\n%s", held)
	}
	if strings.Contains(held, "func (v other.Place)") {
		t.Errorf("a method was declared on another package's type:\n%s", held)
	}

	for _, want := range []string{
		"out.Home = cloneOtherPlace(v.Home)",
		"held := cloneOtherPlace((*v.Work))",
		"out.Past[i] = cloneOtherPlace(one)",
		"out.ByName[key] = cloneOtherPlace(one)",
		"out.Windows[i] = cloneOtherPlace(one)",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the copy does not hold %q:\n%s", want, held)
		}
	}
}

// The name of that function carries the package the struct came from, because a
// subject may reach two structs of one name and two functions of one name is a
// package that does not build.
func TestTheFunctionIsNamedByThePackageItIsFor(t *testing.T) {
	held := source(t, written(t, "Elsewhere"))

	if strings.Contains(held, "func clonePlace(") {
		t.Errorf("the function leaves out the package the struct came from:\n%s", held)
	}
}

// An unexported field of a struct in another package is left to the assignment,
// because generated code here cannot name it — and the copy says so.
//
// Saying so is the whole of what can be done about it. The copy for such a type
// is shallower than a copy usually is, and a comment claiming it shares nothing
// would be a sentence in somebody's file that is not true.
func TestWhatAnotherPackageKeepsToItself(t *testing.T) {
	held := source(t, written(t, "Elsewhere"))

	if strings.Contains(held, "unread") {
		t.Errorf("a copy names a field it cannot reach:\n%s", held)
	}
	for _, want := range []string{
		"except the\n// fields this package cannot name",
		"What it cannot reach is the unexported fields",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the copy does not hold %q:\n%s", want, held)
		}
	}

	// The holder says it too, because a copy that calls a shallow one is as
	// shallow as the one it calls — and its comment is a claim about the whole
	// value rather than about the fields it happens to own.
	if !strings.Contains(held, "func (v Elsewhere) Clone() Elsewhere {") {
		t.Fatalf("the holder has no copy:\n%s", held)
	}

	// And a copy that reaches everything makes the whole claim.
	full := source(t, written(t, "Holding"))
	if strings.Contains(full, "cannot name") || strings.Contains(full, "What it cannot reach") {
		t.Errorf("a copy that reaches every field says it did not:\n%s", full)
	}
}

// An instantiation of a generic cannot carry a method either, and that is a
// different reason from being somewhere else.
//
// The type is right here and its unexported fields are this package's to read,
// so the copy is a full one — it is only the method that has nowhere to go. A
// comment giving the other reason would be plainly wrong to anybody looking at
// the type two lines away.
func TestAnInstantiationIsCopiedByAFunctionToo(t *testing.T) {
	held := source(t, written(t, "Instantiated"))

	if !strings.Contains(held, "func clonePairStringInt(v Pair[string, int]) Pair[string, int] {") {
		t.Errorf("the copy for an instantiation is not a function:\n%s", held)
	}
	if !strings.Contains(held, "the type is an instantiation of a") {
		t.Errorf("the copy gives the wrong reason for being a function:\n%s", held)
	}
	if strings.Contains(held, "declared in another package") {
		t.Errorf("the copy says the type is elsewhere, and it is here:\n%s", held)
	}

	// Its unexported field is this package's to read, so it is copied like any
	// other — which is what the two reasons differ about.
	if !strings.Contains(held, "out.notes = slices.Clone(v.notes)") {
		t.Errorf("an unexported field of a local type was left to the assignment:\n%s", held)
	}
}

// What every subject generates compiles, which is the claim no assertion about
// a substring makes.
func TestWhatIsWrittenCompiles(t *testing.T) {
	for _, name := range []string{"Flat", "Referring", "Deep", "Holding", "Node", "Owning", "Marked", "Elsewhere", "Instantiated"} {
		t.Run(name, func(t *testing.T) {
			sources := []goldentest.Source{
				{Name: "model.go", Content: fixtureSource(t)},
				{Name: "zz_forge.go", Content: []byte(source(t, written(t, name))), Generated: true},
			}

			held := goldentest.Package{
				Path:     "clonefixture/model",
				Files:    sources,
				Requires: []goldentest.Package{besideFixture(t)},
			}
			if err := goldentest.Compiles(held); err != nil {
				t.Errorf("the copy for %s does not compile: %v", name, err)
			}
		})
	}
}

// A field nothing can copy is refused rather than shared behind the author's
// back.
//
// A copy that quietly shared would be worse than no copy at all: it would look
// like the thing it is not, and the program that relied on it would be wrong in
// a way no test of the copy could find.
func TestWhatCannotBeCopied(t *testing.T) {
	cases := map[string]struct {
		code  string
		says  string
		hints string
	}{
		"Opaque":      {"FRG2015", "Anything", "aliasing=share"},
		"Mistaken":    {"FRG2016", "does not answer with", "rename the method"},
		"Misoptioned": {"FRG3018", "whatever is not an option", "aliasing=share"},
		"Misvalued":   {"FRG3018", "aliasing takes share", "the declaration is where"},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := generating(t, name, model.Options{})
			if err == nil {
				t.Fatalf("a copy was written for %s", name)
			}

			reported, ok := diag.From(err)
			if !ok {
				t.Fatalf("%v is not a diagnostic", err)
			}

			if got := reported.Code.String(); got != want.code {
				t.Errorf("%s is reported as %s, want %s: %s", name, got, want.code, reported.Message)
			}
			if !strings.Contains(reported.Message, want.says) {
				t.Errorf("the complaint about %s does not mention %q:\n%s", name, want.says, reported.Message)
			}
			if !strings.Contains(reported.Hint, want.hints) {
				t.Errorf("the hint for %s does not say %q:\n%s", name, want.hints, reported.Hint)
			}
		})
	}
}

// The three things nothing can copy are all refused, rather than one of them
// standing in for the others.
func TestEveryOpaqueFieldIsReported(t *testing.T) {
	_, err := generating(t, "Opaque", model.Options{})
	if err == nil {
		t.Fatal("a copy was written for a subject of things nothing can copy")
	}

	for _, want := range []string{"Anything", "Updates", "Do"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s is not reported:\n%v", want, err)
		}
	}
}

// And saying what was meant is what makes them writable.
func TestSayingWhatWasMeantMakesThemWritable(t *testing.T) {
	if _, err := generating(t, "Marked", model.Options{}); err != nil {
		t.Errorf("a subject that said what it meant was refused anyway: %v", err)
	}
}
