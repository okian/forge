package guarded_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers/guarded"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// A call with no declaration in it is refused rather than generated from.
//
// Not a diagnostic, and the wording says so: a diagnostic points at a
// declaration, and what is missing here is the declaration. Reaching this is
// forge calling itself wrongly rather than anybody writing anything.
func TestGeneratingWithNoDeclaration(t *testing.T) {
	cases := map[string]*layer.Context{
		"no context": nil,
		"no model":   {},
		"no subject": {Model: &model.Model{Name: "Persons"}},
	}

	for name, ctx := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := guarded.New().Generate(ctx, walking("Person")); err == nil {
				t.Error("a call with no declaration in it generated something")
			}
		})
	}
}

// A subject the package being generated into cannot name is refused.
//
// A surface spells its element bare and the view forwards those spellings as
// they are written, which is right for a subject the file can name that way and
// wrong for one it cannot. Refusing is better than writing out a signature
// naming a type this package has no name for.
//
// Both of the ways a subject can be out of reach, because they are different
// questions with different answers and only one of them is the one to ask. A
// subject in another module is one forge cannot generate into; a subject in
// another package of the same module is one it can, and neither of them is one
// this file can spell bare — so a check that asked about the module would let
// the second through and emit a scope forwarding a name nothing here declares.
func TestALockOverASubjectThePackageCannotName(t *testing.T) {
	cases := map[string]*model.Struct{
		"another module":  outside(declaredIn("example.org/other", "other")),
		"another package": declaredIn("example.com/other", "other"),
	}

	for name, subject := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := asked("Persons")
			ctx.Model.Subject = subject

			_, err := guarded.New().Generate(ctx, walking("Person"))
			if err == nil {
				t.Fatal("a subject the package cannot name was generated for")
			}
			if !strings.Contains(err.Error(), "Persons") {
				t.Errorf("the refusal does not name the declaration: %v", err)
			}
		})
	}
}

// outside marks a subject as belonging to a module other than the one being
// generated into.
func outside(held *model.Struct) *model.Struct {
	held.External = true
	return held
}

// A stack whose walk or count is not the walk or count is refused, by name and
// with the signature it offered.
//
// Composition has already refused a stack that cannot be walked, so what
// reaches here is a stack whose methods do not match the contract they were
// admitted under — a layer beneath disagreeing rather than an author writing
// anything, and worth saying before it becomes a file that does not compile.
//
// What each case looks for is the half of the message the offered signature is
// not echoed into. A needle that appears in both halves — "iter.Seq[" against a
// walk answering with the wrong thing — is one the message satisfies by quoting
// the mistake back, and would go on passing if the layer stopped saying what it
// is written over.
func TestALockOverAContractItCannotUse(t *testing.T) {
	cases := map[string]struct {
		below shape.Shape
		says  string
	}{
		"nothing to walk": {
			below: without(walking("Person"), "All"),
			says:  "offers no All",
		},
		"a walk answering with something else": {
			below: signed(walking("Person"), "All", "() []Person"),
			says:  "written over All() iter.Seq[Person]",
		},
		"a walk that takes something": {
			below: signed(walking("Person"), "All", "(from int) iter.Seq[Person]"),
			says:  "written over All() iter.Seq[Person]",
		},
		// The case a check on the shape of the result rather than on the result
		// cannot see. A snapshot is written as a slice of the subject and
		// filled by collecting this walk, so a walk over anything else is two
		// halves of one method disagreeing — and it would arrive as a file that
		// does not compile.
		"a walk over something else": {
			below: signed(walking("Person"), "All", "() iter.Seq[string]"),
			says:  "written over All() iter.Seq[Person]",
		},
		"nothing to count": {
			below: without(walking("Person"), "Len"),
			says:  "offers no Len",
		},
		"a count answering with something else": {
			below: signed(walking("Person"), "Len", "() int64"),
			says:  "written over Len() int",
		},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := guarded.New().Generate(asked("Persons"), held.below)
			if err == nil {
				t.Fatal("a stack that does not offer what a lock writes over generated something")
			}
			if !strings.Contains(err.Error(), held.says) {
				t.Errorf("the refusal does not say what was wrong with it: %v", err)
			}
		})
	}
}

// A method a scope would forward that names the type it is a scope over is
// refused, because forwarding it is a way back out of the scope.
func TestAScopeThatCouldReachBackOut(t *testing.T) {
	below := walking("Person")
	below.Surface = append(below.Surface, shape.Method{
		Name: "Clone", Signature: "() Persons", Owner: marker("Clone"),
		Doc: "returns a copy of the container",
	})

	_, err := guarded.New().Generate(asked("Persons"), below)
	if err == nil {
		t.Fatal("a scope forwarding a way back to the value it was opened on was written")
	}
	if !strings.Contains(err.Error(), "Clone") {
		t.Errorf("the refusal does not name the method: %v", err)
	}
}

// without returns the shape with one method taken off its surface.
func without(below shape.Shape, name string) shape.Shape {
	return below.Without(name)
}

// signed returns the shape with one method's signature replaced, which is how a
// layer beneath is made to disagree with the contract it was admitted under.
func signed(below shape.Shape, name, signature string) shape.Shape {
	held, has := below.Method(name)
	if !has {
		return below
	}

	held.Signature = signature
	return below.WithMethods(held)
}
