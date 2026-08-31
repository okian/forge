package layer_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
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

// The catalog's one field-scoped option is the codec's reflective boundary.
// It is per field by nature — turning reflection on for every field at once is
// the opposite of marking a boundary — and nothing else in the catalog is.
func TestWhichOptionsAreAboutFields(t *testing.T) {
	var about []string

	for _, l := range layer.Builtins().All() {
		for _, def := range l.OptionSchema() {
			if def.Scope == layer.ScopeField {
				about = append(about, l.Origin().Name+"."+def.Key)
			}
		}
	}

	if want := []string{"Json.fallback"}; strings.Join(about, ", ") != strings.Join(want, ", ") {
		t.Errorf("the options about fields are %v, want %v", about, want)
	}
}

// An option a layer cannot generate without, and that belongs on a field, would
// be demanded in the one place it is refused. Nothing in the catalog does that,
// and a layer that did could not be configured at all.
func TestNoOptionIsBothRequiredAndAboutAField(t *testing.T) {
	for _, l := range layer.Builtins().All() {
		for _, def := range l.OptionSchema() {
			if def.Required && def.Scope == layer.ScopeField {
				t.Errorf("%s.%s is required and belongs on a field, so it can be neither written nor left out",
					l.Origin().Name, def.Key)
			}
		}
	}
}

// A schema is copied on the way out, so a caller reading one cannot change what
// forge accepts for the rest of the process — including the scope.
func TestAScopeSurvivesBeingRead(t *testing.T) {
	json, ok := layer.Builtins().Lookup(model.TypeRef{Pkg: model.MarkerPkg, Name: "Json"})
	if !ok {
		t.Fatal("the catalog has no Json layer")
	}

	first := json.OptionSchema()
	for i := range first {
		first[i].Scope = layer.ScopeDeclaration
	}

	for _, def := range json.OptionSchema() {
		if def.Key == "fallback" && def.Scope != layer.ScopeField {
			t.Error("a caller rewrote what the catalog accepts")
		}
	}
}
