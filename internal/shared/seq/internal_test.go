package seq

import (
	"go/token"
	"strings"
	"testing"
)

// A view that cannot be read is a fault in forge, reported against the
// declaration that required it — an author can do nothing about it, and the
// declaration is what they were working on.
//
// Reachable only from inside the package: the view this ships is Go that the
// ordinary build compiles, which is what the arrangement is for and what makes
// the failure unreachable any other way.
func TestAViewThatCannotBeRead(t *testing.T) {
	at := token.Position{Filename: "model/spec.go", Line: 8, Column: 6}

	was := bodies
	defer func() { bodies = was }()

	bodies = []byte("package tmpl\n\nfunc (\n")

	unit, err := Unit(at)
	if err == nil {
		t.Fatal("a view that is not Go was read without complaint")
	}
	if len(unit.Decls) != 0 {
		t.Errorf("it returned %d declarations as well", len(unit.Decls))
	}
	if !strings.Contains(err.Error(), at.Filename) {
		t.Errorf("the error %q does not point at the declaration that asked", err)
	}
}
