package validate

import (
	"strings"
	"testing"

	"github.com/okian/forge/plugin"
)

// Source that does not parse is reported against the subject it was assembled
// for, rather than written out and left for the compiler.
//
// The branch is unreachable through the layer's own front door, which is the
// reason to test it here: everything the writer emits is built from a template
// this repository compiles, so a run that reached the parser with invalid Go
// would mean the writer had broken, not the author. Left untested it is a
// message nobody has ever read, and the thing it has to do — name the subject,
// so the reader knows which of a run's many declarations went wrong — is
// exactly what an unread message gets wrong.
func TestSourceThatDoesNotParseIsReportedAgainstItsSubject(t *testing.T) {
	_, _, _, err := parsed("func (", "Person")

	if err == nil {
		t.Fatal("parsing source that is not Go: want an error, got none")
	}
	if !strings.Contains(err.Error(), "Person") {
		t.Errorf("the error does not name the subject it was assembled for: %v", err)
	}
	if !strings.Contains(err.Error(), "not valid Go") {
		t.Errorf("the error does not say what was wrong: %v", err)
	}
}

// A field whose type the loader never resolved is asked nothing.
//
// Every one of these reads field.Type.Type, which is nil only where the package
// failed to type-check — and a package that failed to type-check still reaches
// the planner, because refusing to plan is how the diagnostics that explain the
// failure get written. So each of them is a question asked about a type that is
// not there, and the answer has to be the one that adds no check: inventing a
// check against a type nobody resolved would emit code naming a type the
// package does not have.
func TestAFieldWithNoResolvedTypeIsAskedNothing(t *testing.T) {
	var p planner
	field := plugin.Field{Name: "Unresolved"}

	if nested, indirect := p.nested(field); nested || indirect {
		t.Errorf("nested = (%v, %v), want (false, false)", nested, indirect)
	}
	if got := p.throughFor(field); got != "" {
		t.Errorf("throughFor = %q, want none", got)
	}
	if p.dropped(field) {
		t.Error("dropped = true, want false")
	}
	if got := spelt(nil, field); len(got) != 0 {
		t.Errorf("spelt = %v, want no imports", got)
	}
}

// A struct that is not one is not remembered.
//
// The planner walks what a subject reaches, and what it reaches is built from
// the same load that may have failed. Reserving a plan for a struct with no
// type of its own would put an entry in the order that every later stage then
// has to special-case, so the walk drops it here instead — once, rather than at
// each of the places that reads what was remembered.
func TestAStructThatIsNotOneIsNotRemembered(t *testing.T) {
	cases := map[string]*plugin.Struct{
		"nothing":               nil,
		"a struct with no type": {},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			var p planner
			p.remember(held)

			if len(p.order) != 0 {
				t.Errorf("remembering %s left %v in the order, want nothing", name, p.order)
			}
		})
	}
}
