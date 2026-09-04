package jsoncodec_test

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layers/jsoncodec"
	"github.com/okian/forge/internal/subject"
	"github.com/okian/forge/plugin"
)

// The fused writers read the mapping's expressions where the codec reads the
// held value, and everything else — names, order, escaping — is the codec's.
func TestFusedWritesFromTheGivenReads(t *testing.T) {
	loaded := loadFixture(t)
	builder := subject.New(subject.Config{Fset: loaded.Fset, Owned: loaded.Owned(), Docs: loaded.FieldDocs()})

	pkg, ok := loaded.Package(modelPkg)
	if !ok {
		t.Fatalf("the fixture has no package %s", modelPkg)
	}

	built, problems := builder.Build(named(t, loaded, "Address"), subject.Site{})
	if !problems.Empty() {
		t.Fatalf("modelling Address: %s", problems.Render())
	}

	// A source type stands in through go/types; only its name and kind reach
	// the signature.
	srcObj := types.NewTypeName(token.NoPos, pkg.Types, "Feed", nil)
	source := types.NewNamed(srcObj, types.NewStruct(nil, nil), nil)

	var asked string
	unit, err := jsoncodec.Fused(&plugin.Context{
		Model: &plugin.Model{
			Name: "Wire", Form: plugin.FormSpec, Subject: built, Source: source,
			Pkg: pkg, Pos: token.Position{Filename: "spec.go"},
		},
	}, func(src string) map[string]string {
		asked = src
		out := make(map[string]string, len(built.Fields))
		for _, field := range built.Fields {
			out[field.Name] = src + ".X" + field.Name + "()"
		}
		return out
	})
	if err != nil {
		t.Fatalf("fusing: %v", err)
	}
	if asked != "src" {
		t.Errorf("the reads were asked for %q, want src", asked)
	}

	text := rendered(t, unit)
	for _, want := range []string{
		"func AppendAddressJSONFromFeed(dst []byte, src *Feed) ([]byte, error)",
		"func WriteAddressJSONFromFeed(w io.Writer, src *Feed) (int64, error)",
		"src.XCity()",
		"src.XPost()",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the fused unit does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "v.City") {
		t.Errorf("a held-value read survived into the fused body:\n%s", text)
	}
}

// A mapping that hands no read for a member is forge's own bug, and the
// failure names the member rather than emitting a body that does not compile.
func TestFusedRefusesAMissingRead(t *testing.T) {
	loaded := loadFixture(t)
	builder := subject.New(subject.Config{Fset: loaded.Fset, Owned: loaded.Owned(), Docs: loaded.FieldDocs()})

	pkg, _ := loaded.Package(modelPkg)
	built, problems := builder.Build(named(t, loaded, "Address"), subject.Site{})
	if !problems.Empty() {
		t.Fatalf("modelling Address: %s", problems.Render())
	}

	srcObj := types.NewTypeName(token.NoPos, pkg.Types, "Feed", nil)
	source := types.NewNamed(srcObj, types.NewStruct(nil, nil), nil)

	_, err := jsoncodec.Fused(&plugin.Context{
		Model: &plugin.Model{
			Name: "Wire", Form: plugin.FormSpec, Subject: built, Source: source,
			Pkg: pkg, Pos: token.Position{Filename: "spec.go"},
		},
	}, func(src string) map[string]string {
		return map[string]string{"City": src + ".City"} // Post is missing
	})
	if err == nil || !strings.Contains(err.Error(), "Post") {
		t.Fatalf("a missing read was not refused by name: %v", err)
	}
}

// rendered prints a unit the way the emitter would, so a test can read it.
func rendered(t *testing.T, unit plugin.Unit) string {
	t.Helper()

	file := emit.File{Package: "model"}
	file.Sections = append(file.Sections, emit.Section{
		Decls: unit.Decls, Comments: unit.Comments, Fset: unit.Fset,
	})
	file.Imports = append(file.Imports, unit.Imports...)

	out, err := file.Render()
	if err != nil {
		t.Fatalf("rendering the fused unit: %v", err)
	}
	return string(out)
}
