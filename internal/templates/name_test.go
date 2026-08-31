package templates_test

import (
	"strings"
	"testing"
)

// constructed is a template with a constructor in it, which is the case the
// caller-supplied names exist for: a package of one type calls it New, and the
// declaration it becomes is one type among an author's own.
const constructed = "package tmpl\n\n" +
	"type Collection[T any] []T\n\n" +
	"func New[T any](elems ...T) *Collection[T] {\n" +
	"\tout := Collection[T](elems)\n" +
	"\treturn &out\n" +
	"}\n\n" +
	"func (c Collection[T]) Rebuilt() *Collection[T] { return New(c...) }\n"

// A name the caller answered for takes that answer, everywhere the template
// used it — including from another body, which is where a rename that only
// rewrote declarations would leave a call to a function that is no longer
// there.
func TestANameTheCallerAnsweredFor(t *testing.T) {
	r := ordinary
	r.Names = map[string]string{"New": "NewPersons"}

	text := specialised(t, constructed, r)

	for _, want := range []string{"func NewPersons(elems ...Person) *Persons", "return NewPersons(c...)"} {
		if !strings.Contains(text, want) {
			t.Errorf("the output does not read %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "personsNew") {
		t.Errorf("the prefix was applied to a name that was answered for:\n%s", text)
	}
}

// A template whose every other name is answered for needs no prefix, which is
// what keeps a layer from inventing one it never uses.
func TestAnAnsweredNameNeedsNoPrefix(t *testing.T) {
	r := ordinary
	r.Prefix = ""
	r.Names = map[string]string{"New": "NewPersons"}

	if text := specialised(t, constructed, r); !strings.Contains(text, "func NewPersons(") {
		t.Errorf("a template with nothing left to prefix was refused a rewrite:\n%s", text)
	}
}

// A rename of something the template does not declare renames nothing, and what
// it renames nothing of is the name somebody meant to change.
func TestARenameOfSomethingThatIsNotThere(t *testing.T) {
	r := ordinary
	r.Names = map[string]string{"Nonesuch": "NewPersons"}

	reported := refused(t, constructed, r)
	if !strings.Contains(reported, "FRG4911") {
		t.Errorf("a rename of a name nobody declared was carried out:\n%s", reported)
	}
	if !strings.Contains(reported, "Nonesuch") {
		t.Errorf("the failure does not name what could not be renamed:\n%s", reported)
	}
}

// A name to rename to has to be a name: the printer writes an identifier
// verbatim, so anything else is a file that does not parse blamed on an author
// who wrote nothing wrong.
func TestARenameToSomethingThatIsNotAName(t *testing.T) {
	r := ordinary
	r.Names = map[string]string{"New": "not an ident"}

	if reported := refused(t, constructed, r); !strings.Contains(reported, "FRG4911") {
		t.Errorf("a rename to something that is not a name was carried out:\n%s", reported)
	}
}

// An answered name collides like any other, since the prefix is not what makes
// two names one — being the same name is.
func TestAnAnsweredNameThatCollides(t *testing.T) {
	r := ordinary
	r.Names = map[string]string{"New": "personsCounted"}

	reported := refused(t, "package tmpl\n\n"+
		"type Collection[T any] []T\n\n"+
		"type counted struct{ n int }\n\n"+
		"func New[T any]() *Collection[T] { return nil }\n", r)

	if !strings.Contains(reported, "become the same name") {
		t.Errorf("two names that become one were accepted:\n%s", reported)
	}
}

// The subject is the one name here the file being written does not declare: it
// is spelled as that file has to spell it, which for a type from another
// package is qualified and for an instantiation carries its arguments.
func TestTheSubjectAsTheFileHasToSpellIt(t *testing.T) {
	const source = "package tmpl\n\n" +
		"type Collection[T any] []T\n\n" +
		"func (c Collection[T]) First() T { return c[0] }\n"

	for _, subject := range []string{"domain.Person", "Pair[string, int]", "*Person", "[]byte"} {
		t.Run(subject, func(t *testing.T) {
			r := ordinary
			r.Subject = subject

			text := specialised(t, source, r)
			if want := "type Persons []" + subject; !strings.Contains(text, want) {
				t.Errorf("the output does not read %q:\n%s", want, text)
			}
			if want := "First() " + subject; !strings.Contains(text, want) {
				t.Errorf("the output does not read %q:\n%s", want, text)
			}
		})
	}
}

// A subject spelled like the container is the author's own type called Slice
// under a template that declares one, and it is rewritten rather than refused.
// The rename answers for each node once, so the container's mentions become the
// declared name before anything could turn them back, and the subject's arrive
// as the type parameter and are never looked up again.
func TestASubjectSpelledLikeTheContainer(t *testing.T) {
	r := ordinary
	r.Subject = "Collection"

	text := specialised(t, "package tmpl\n\n"+
		"type Collection[T any] []T\n\n"+
		"func (c Collection[T]) First() T { return c[0] }\n", r)

	for _, want := range []string{"type Persons []Collection", "func (c Persons) First() Collection"} {
		if !strings.Contains(text, want) {
			t.Errorf("the output does not read %q:\n%s", want, text)
		}
	}
}

// A name the caller answered for that the template does not decide is not a
// rename that lost — saying "it does not declare that" about the container
// would be false, and would send whoever wrote it looking for a declaration
// that is right there.
func TestARenameOfSomethingTheRewriteAlreadyDecides(t *testing.T) {
	const source = "package tmpl\n\n" +
		"type Collection[T any] []T\n\n" +
		"var _ = 0\n\n" +
		"func init() {}\n"

	cases := map[string]map[string]string{
		"the container":         {"Collection": "Elsewhere"},
		"the type parameter":    {"T": "Elsewhere"},
		"a name nobody can use": {"_": "Elsewhere"},
		"the initialiser":       {"init": "Elsewhere"},

		// An empty answer is as malformed as one that is not a name, and it is
		// the shape a caller reaches by building the map from something that
		// came up empty — so it must not be the one that quietly does nothing.
		"the container, emptily": {"Collection": ""},
	}

	for name, names := range cases {
		t.Run(name, func(t *testing.T) {
			r := ordinary
			r.Names = names

			reported := refused(t, source, r)
			if !strings.Contains(reported, "FRG4911") {
				t.Errorf("the rename was carried out:\n%s", reported)
			}
			if strings.Contains(reported, "does not declare") {
				t.Errorf("the failure says the template declares nothing of the sort:\n%s", reported)
			}
		})
	}
}

// A subject that is only the start of the text it was given would be written
// whole into an identifier's name, since a printer writes one verbatim.
func TestASubjectThatIsNotAllOfWhatItWasGiven(t *testing.T) {
	r := ordinary
	r.Subject = "Person // and the rest of a parameter list"

	if reported := refused(t, "package tmpl\n\ntype Collection[T any] []T\n", r); !strings.Contains(reported, "FRG4911") {
		t.Errorf("a subject with a comment after it was written into the output:\n%s", reported)
	}
}

// A prefixed helper that spells the subject's own name redeclares it in the
// author's package, which is the same failure as two of the template's names
// becoming one and reaches it by a different road.
func TestAHelperThatBecomesTheSubject(t *testing.T) {
	r := ordinary
	r.Subject = "personsEach"

	reported := refused(t, "package tmpl\n\n"+
		"type Collection[T any] []T\n\n"+
		"func each[T any](c Collection[T]) int { return len(c) }\n", r)

	if !strings.Contains(reported, "becomes the name of the subject") {
		t.Errorf("a helper that redeclares the subject was accepted:\n%s", reported)
	}
}
