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

// What every subject generates compiles, which is the claim no assertion about
// a substring makes.
func TestWhatIsWrittenCompiles(t *testing.T) {
	for _, name := range []string{"Flat", "Referring", "Deep", "Holding", "Node", "Owning", "Marked"} {
		t.Run(name, func(t *testing.T) {
			sources := []goldentest.Source{
				{Name: "model.go", Content: fixtureSource(t)},
				{Name: "zz_forge.go", Content: []byte(source(t, written(t, name))), Generated: true},
			}

			if err := goldentest.Compiles(goldentest.Package{Path: "model", Files: sources}); err != nil {
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
