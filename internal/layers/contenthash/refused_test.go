package contenthash_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/layers/contenthash"
	"github.com/okian/forge/plugin"
)

// A subject no method can be attached to is refused, and the refusal says which
// subject it was.
//
// A hash is declared on the subject, so a struct with no name in the package
// being generated into has nothing to declare it on. Worth a test of its own
// rather than trusting the two layers beside this one: each writes the message
// itself, and a layer that returned the zero unit and no error here would emit
// nothing at all and say nothing about why.
func TestAHashIsRefusedForASubjectItCannotName(t *testing.T) {
	_, err := contenthash.New().Generate(&plugin.Context{
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
