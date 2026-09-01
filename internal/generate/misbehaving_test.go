package generate_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/okian/forge/internal/compose"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// What a layer can get wrong that nobody else can see.
//
// These four are about the layers taken together and about what one of them
// handed over, which is the first thing a stage holding a whole declaration can
// look at — and the last thing anybody looks at before a file is written. A
// layer is somebody else's code once the plugin surface is public, so each of
// them is a fault reported by name rather than a file that does not build.
func TestWhatAMisbehavingLayerIsToldAboutItself(t *testing.T) {
	cases := map[string]struct {
		layer layer.Layer
		code  string
		says  string
	}{
		"a declaration that is not there": {
			layer: gives{}, code: "FRG4014", says: "declarations that are not there",
		},
		"an import with no path": {
			layer: unpathed{}, code: "FRG4015", says: "with no path",
		},
		"two packages under one name": {
			layer: clashing{}, code: "FRG4016", says: "binds two packages",
		},
		"two layers writing one method": {
			layer: doubling{}, code: "FRG4012", says: "write",
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := generate.Package(local, "model",
				[]generate.Request{misbehaving(want.layer)}, misbehavingConfig(t, want.layer))

			if diags.Empty() {
				t.Fatal("nothing was reported")
			}

			found := reported(t, diags, want.code)
			if !strings.Contains(found.Message, want.says) {
				t.Errorf("the complaint does not mention %q:\n%s", want.says, found.Message)
			}
			if found.Hint == "" {
				t.Error("the complaint says nothing to do about it")
			}
			if found.Pos.Line != declaredAt.Line {
				t.Errorf("it points at %s rather than at the declaration", found.Pos)
			}
		})
	}
}

// misbehaving builds a declaration over the one layer being tested.
func misbehaving(one layer.Layer) generate.Request {
	held := request("Persons")
	held.Model.Form = model.FormInline
	held.Model.Stack = []model.LayerRef{{Origin: one.Origin(), Kind: one.Kind()}}

	return held
}

// misbehavingConfig returns a configuration whose catalog is the one layer,
// with the ordinary default storage so that composition has one to fill in.
func misbehavingConfig(t *testing.T, one layer.Layer) generate.Config {
	t.Helper()

	registry := layer.New()
	registry.MustRegister(one)
	for _, held := range layers.Builtins().All() {
		if held.Origin() != one.Origin() {
			registry.MustRegister(held)
		}
	}

	cfg := config()
	cfg.Catalog = compose.Catalog{Registry: registry, DefaultStorage: layers.DefaultStorage()}

	return cfg
}

// misbehaved is what the four layers below have in common: a storage layer that
// composes over anything and differs only in what it hands over.
type misbehaved struct{ named string }

func (m misbehaved) Origin() model.TypeRef {
	return model.TypeRef{Pkg: model.MarkerPkg, Name: m.named}
}
func (misbehaved) Kind() model.Kind                { return model.KindStorage }
func (misbehaved) Stage() layer.Stage              { return layer.StageReady }
func (misbehaved) Doc() string                     { return "a layer written to get one thing wrong" }
func (misbehaved) Transparent() bool               { return true }
func (misbehaved) Accepts(shape.Shape) error       { return nil }
func (misbehaved) OptionSchema() []layer.OptionDef { return nil }

func (misbehaved) Shape(_ *layer.Context, below shape.Shape) shape.Shape {
	below.Caps = below.Caps.With(shape.Sized, shape.Streamable)
	return below
}

// gives hands over a declaration that is not there.
type gives struct{ misbehaved }

func (gives) Origin() model.TypeRef { return misbehaved{named: "Gives"}.Origin() }

func (gives) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	return layer.Unit{Decls: []ast.Decl{nil}}, nil
}

// unpathed asks for an import bound to a name with no path behind it.
type unpathed struct{ misbehaved }

func (unpathed) Origin() model.TypeRef { return misbehaved{named: "Unpathed"}.Origin() }

func (unpathed) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	decls, comments, fset := built(`type Persons []Person`)

	return layer.Unit{
		Decls: decls, Comments: comments, Fset: fset,
		Imports: []emit.Import{{Name: "nowhere"}},
	}, nil
}

// clashing asks for two paths under one name, which is the one import mistake
// no single layer can see.
type clashing struct{ misbehaved }

func (clashing) Origin() model.TypeRef { return misbehaved{named: "Clashing"}.Origin() }

func (clashing) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	decls, comments, fset := built(`type Persons []Person`)

	return layer.Unit{
		Decls: decls, Comments: comments, Fset: fset,
		Imports: []emit.Import{
			{Path: "example.com/one/cmp", Name: "cmp"},
			{Path: "cmp", Name: "cmp"},
		},
	}, nil
}

// doubling writes one method twice, which is what two layers each wanting a
// name look like by the time the declarations reach one place.
type doubling struct{ misbehaved }

func (doubling) Origin() model.TypeRef { return misbehaved{named: "Doubling"}.Origin() }

func (doubling) Generate(*layer.Context, shape.Shape) (layer.Unit, error) {
	decls, comments, fset := built(
		"type Persons []Person\n\n" +
			"func (p Persons) Len() int { return len(p) }\n\n" +
			"func (p Persons) Len() int { return 0 }\n")

	return layer.Unit{Decls: decls, Comments: comments, Fset: fset}, nil
}

// built parses a layer's source the way a real one does.
func built(source string) ([]ast.Decl, []*ast.CommentGroup, *token.FileSet) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "layer.go", "package model\n\n"+source, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	return file.Decls, file.Comments, fset
}
