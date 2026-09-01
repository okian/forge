package clone

import (
	"strings"
	"testing"
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

// A shape that is not there takes no statement to copy.
//
// needs is asked about a field's element, and an element is absent for the
// shapes that have none — a pointer's target that failed to resolve, or a form
// the walk never filled in. Answering false is what keeps the copy an
// assignment: the alternative is a loop written over something with no element
// type to name, which is a file that does not compile rather than a copy that
// is merely wrong.
func TestAShapeThatIsNotThereTakesNoStatement(t *testing.T) {
	if needs(nil) {
		t.Error("needs(nil) = true, want false")
	}
}
