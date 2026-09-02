package clone_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/layers/clone"
	"github.com/okian/forge/plugin"
)

// A subject no method can be attached to is refused, and the refusal says which
// subject it was.
//
// The distinction the layer draws is between a copy that needs a function
// beside it and a copy that cannot be written at all, and only the second is an
// error. A struct reached through an instantiation, or one belonging to a
// module this run does not own, has no name in the package being generated
// into — so there is nothing for the copy to be declared on and nothing to call
// it by. Refusing names the declaration, because a message that said only that
// something was unreachable would leave the reader to work out which of the
// types their declaration reaches was meant.
func TestACopyIsRefusedForASubjectItCannotName(t *testing.T) {
	_, err := clone.New().Generate(&plugin.Context{
		Model: &plugin.Model{Name: "Elsewhere", Subject: &plugin.Struct{}},
	}, plugin.Shape{})

	if err == nil {
		t.Fatal("generating for a subject with no name of its own: want an error, got none")
	}
	if !strings.Contains(err.Error(), "Elsewhere") {
		t.Errorf("the refusal does not name the declaration: %v", err)
	}
	if !strings.Contains(err.Error(), "cannot be named") {
		t.Errorf("the refusal does not say what was wrong: %v", err)
	}
}
