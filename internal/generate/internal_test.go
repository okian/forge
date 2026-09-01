package generate

import (
	"errors"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// declaredAt is where the declarations these tests are about were written.
var reportedAt = token.Position{Filename: "model/spec.go", Line: 8, Column: 6}

// A layer that reported a diagnostic is reported as it wrote it: it had the
// declaration and knew what was wrong, and rewording it would lose that.
func TestALayerThatSaidWhatWasWrong(t *testing.T) {
	code := diag.Register(3999, "a layer said so itself")
	said := diag.New(code, reportedAt, "the field Age is not a colour")

	got := refusal(said, &model.Model{Name: "Persons"},
		model.LayerRef{Origin: model.TypeRef{Name: "Collection"}})

	if got.Code != code || got.Message != said.Message {
		t.Errorf("what the layer said became %v", got)
	}
}

// A layer that returned an ordinary error had something go wrong it has no
// vocabulary for, and what an author can do about that is say so — so it is
// given a code, a position, and the name of the layer that produced it.
func TestALayerThatMerelyFailed(t *testing.T) {
	held := &model.Model{Name: "Persons", Pos: reportedAt}

	got := refusal(errors.New("the template is on fire"), held,
		model.LayerRef{Origin: model.TypeRef{Name: "Collection"}})

	if got.Code.String() != "FRG4008" {
		t.Errorf("code is %s", got.Code)
	}
	for _, want := range []string{"Collection", "Persons", "on fire"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("the message %q does not name %s", got.Message, want)
		}
	}
	if got.Pos != reportedAt {
		t.Errorf("it points at %s", got.Pos)
	}
	if got.Hint == "" {
		t.Error("nothing says what to do about a layer that failed")
	}
}

// A helper nothing provides is a fault in forge, reported against a declaration
// rather than nowhere: a package has no position of its own, and a report with
// none points at the working directory.
func TestAHelperNothingProvides(t *testing.T) {
	requests := []Request{{Model: &model.Model{Name: "Persons", Pos: reportedAt}}}
	required := []model.TypeRef{{Pkg: "example.com/model", Name: "Nonesuch"}}

	_, diags := helpers("example.com/model", required, requests)

	reported := diags.Render()
	if !strings.Contains(reported, "FRG4007") || !strings.Contains(reported, "Nonesuch") {
		t.Errorf("the report does not name what nothing provides:\n%s", reported)
	}
	if !strings.Contains(reported, reportedAt.Filename) {
		t.Errorf("it points nowhere:\n%s", reported)
	}

	// And a run whose declarations were all refused has no position to point
	// at, which is a report about the working directory rather than a crash.
	if got := at(nil); got.IsValid() {
		t.Errorf("a run with no declarations is at %s", got)
	}
	if got := at([]Request{{}}); got.IsValid() {
		t.Errorf("a run whose declaration has no model is at %s", got)
	}
}

// Asking for a helper by a name nothing answers to comes back as an error
// rather than an empty unit, because an empty unit says the helper was emitted.
func TestAskingForAHelperThatIsNotThere(t *testing.T) {
	if _, err := provided(model.TypeRef{Name: "Nonesuch"}, "example.com/model"); err == nil {
		t.Error("a helper nobody provides was provided")
	}
}

// The closure counts as well as the subject: a layer that walks it generates
// from what it finds, so a nested struct that changed is a declaration whose
// output changed even though its own source did not move.
func TestWhatTheClosureAddsToAFingerprint(t *testing.T) {
	subject := func(nested string) *model.Struct {
		pkg := types.NewPackage("example.com/model", "model")

		held := &model.Struct{
			Named: types.NewNamed(
				types.NewTypeName(token.NoPos, pkg, "Person", nil),
				types.NewStruct(nil, nil), nil),
			Fields: []model.Field{{Name: "Address", Exported: true, Type: model.Classified{Type: types.Typ[types.String]}}},
		}

		held.Closure = []*model.Struct{{
			Named: types.NewNamed(
				types.NewTypeName(token.NoPos, pkg, "Address", nil),
				types.NewStruct(nil, nil), nil),
			Fields: []model.Field{{Name: nested, Exported: true, Type: model.Classified{Type: types.Typ[types.String]}}},
		}}

		return held
	}

	held := func(nested string) string {
		var sum emit.Digest
		Fingerprint(&sum, Request{Model: &model.Model{Name: "Persons", Subject: subject(nested)}}, "model", Config{})

		return sum.String()
	}

	if held("City") == held("Town") {
		t.Error("a field of a reachable struct changed and the fingerprint did not")
	}

	// A declaration with no model at all still records what the run was, since
	// that is what it has.
	var sum emit.Digest
	Fingerprint(&sum, Request{}, "model", Config{Forge: "v1"})

	if sum.Len() == 0 {
		t.Error("a request with no declaration recorded nothing at all")
	}
}

// Where a subject stands with respect to the package being written decides
// whether a layer attaches a method to it or emits a function beside it, so it
// is what the output depends on and therefore what the fingerprint has to hold.
func TestWhereTheSubjectStands(t *testing.T) {
	held := func(change func(*model.Struct)) string {
		of := &model.Struct{
			Named: types.NewNamed(
				types.NewTypeName(token.NoPos, types.NewPackage("example.com/model", "model"), "Person", nil),
				types.NewStruct(nil, nil), nil),
		}
		change(of)

		var sum emit.Digest
		Fingerprint(&sum, Request{Model: &model.Model{Name: "Persons", Subject: of}}, "model", Config{})

		return sum.String()
	}

	local := held(func(*model.Struct) {})
	external := held(func(of *model.Struct) { of.External = true })
	instantiated := held(func(of *model.Struct) { of.Instantiated = true })

	if local == external || local == instantiated || external == instantiated {
		t.Error("a subject in one place and the same subject in another fingerprint alike")
	}

	// And what it already carries, which decides what a layer generates rather
	// than delegates to and what it may not declare a second time.
	iface := held(func(of *model.Struct) {
		of.Implements = []model.TypeRef{{Pkg: "encoding/json", Name: "Marshaler"}}
	})
	method := held(func(of *model.Struct) { of.Methods = []string{"Len"} })

	if local == iface || local == method {
		t.Error("what a subject already carries left the fingerprint where it was")
	}
}

// Rearranging a struct's fields writes a different file, because generated
// output follows the declaration order — and it is not exotic, since every tool
// that reports struct padding asks for it.
func TestRearrangingFields(t *testing.T) {
	held := func(first, second string) string {
		of := &model.Struct{
			Named: types.NewNamed(
				types.NewTypeName(token.NoPos, types.NewPackage("example.com/model", "model"), "Person", nil),
				types.NewStruct(nil, nil), nil),
			Fields: []model.Field{
				{Name: first, Exported: true, Type: model.Classified{Type: types.Typ[types.Int]}},
				{Name: second, Exported: true, Type: model.Classified{Type: types.Typ[types.Int]}},
			},
		}

		var sum emit.Digest
		Fingerprint(&sum, Request{Model: &model.Model{Name: "Persons", Subject: of}}, "model", Config{})

		return sum.String()
	}

	if held("ID", "Age") == held("Age", "ID") {
		t.Error("two fields swapped left the fingerprint where it was")
	}
}

// A layer that answers with a panic is a layer that misbehaved, not a run that
// crashed — and this is the path that writes files, so a stack trace would
// arrive after part of somebody's package had already been rewritten.
func TestALayerThatPanicsWhileGenerating(t *testing.T) {
	unit, err := generated(exploding{}, nil, shape.Shape{})

	if err == nil {
		t.Fatal("a layer that panicked was reported as having generated")
	}
	if len(unit.Decls) != 0 {
		t.Errorf("it returned %d declarations as well", len(unit.Decls))
	}
	if !strings.Contains(err.Error(), "no idea") {
		t.Errorf("the error %q does not say what the layer said", err)
	}
}

// exploding is a layer from outside that generates by failing.
type exploding struct{}

func (exploding) Binds() []model.Import           { return nil }
func (exploding) Origin() model.TypeRef           { return model.TypeRef{Name: "Broken"} }
func (exploding) Kind() model.Kind                { return model.KindRefining }
func (exploding) OptionSchema() []layer.OptionDef { return nil }
func (exploding) Accepts(shape.Shape) error       { return nil }

func (exploding) Shape(_ *layer.Context, below shape.Shape) shape.Shape { return below }

func (exploding) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	panic("this layer has no idea")
}
