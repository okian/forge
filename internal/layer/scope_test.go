package layer_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/layer"
)

// A scope is spelled in a diagnostic, so it has a name — and one nobody defined
// says its number rather than nothing, because a report that names no scope is
// a report about nothing.
func TestHowAScopeIsSpelled(t *testing.T) {
	spelled := map[layer.Scope]string{
		layer.ScopeDeclaration: "declaration",
		layer.ScopeField:       "field",
	}

	for scope, want := range spelled {
		if got := scope.String(); got != want {
			t.Errorf("%d is spelled %q, want %q", scope, got, want)
		}
	}

	if got := layer.Scope(99).String(); !strings.Contains(got, "99") {
		t.Errorf("a scope nobody defined is spelled %q", got)
	}
}

// The zero value is the declaration, because nearly every option is about one
// and an option that has not thought about scope should not be about a field by
// accident.
func TestTheScopeAnOptionHasByDefault(t *testing.T) {
	var def layer.OptionDef

	if def.Scope != layer.ScopeDeclaration {
		t.Errorf("an option that says nothing is about a %s", def.Scope)
	}
}
