package jsoncodec_test

import (
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers/jsoncodec"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/subject"
)

// The declaration decides what an untagged field is called on the wire.
//
// Read from the written source rather than by round-tripping, because the thing
// under test is the name itself. A round trip through one codec agrees with
// itself whatever it calls a member, which is precisely the mistake a wire
// format cannot afford: the reader that disagrees is in another program.
func TestWhatAnUntaggedFieldIsCalled(t *testing.T) {
	// Names with an initialism at each end and one in the middle, because those
	// are where the styles differ: a rule that only lowered the first letter
	// would produce jSONValue and pass any test written against Name alone.
	cases := map[string]struct {
		style string
		want  []string
	}{
		"left as it is": {
			"", []string{`"UserID"`, `"JSONValue"`, `"HTTPServer"`, `"ID"`, `"Name"`},
		},
		"asked for as it is": {
			"asis", []string{`"UserID"`, `"JSONValue"`, `"HTTPServer"`, `"ID"`, `"Name"`},
		},
		"in snake case": {
			"snake", []string{`"user_id"`, `"json_value"`, `"http_server"`, `"id"`, `"name"`},
		},
		"in camel case": {
			"camel", []string{`"userID"`, `"jsonValue"`, `"httpServer"`, `"id"`, `"name"`},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			source := written(t, "Naming", options(tc.style, false))

			for _, want := range tc.want {
				if !strings.Contains(source, want) {
					t.Errorf("no member called %s:\n%s", want, source)
				}
			}
		})
	}
}

// A name a tag gives is the name, whatever style the declaration asked for.
//
// The tag is what the rest of the ecosystem reads. A field renamed by forge and
// not by the standard library goes out under one name and is looked for under
// another, and no style is worth that.
func TestATagOutranksTheStyle(t *testing.T) {
	source := written(t, "Tagged", options("snake", false))

	if !strings.Contains(source, `"renamed_here"`) {
		t.Errorf("the tagged name is missing:\n%s", source)
	}
	if strings.Contains(source, `"Hidden"`) || strings.Contains(source, `"hidden"`) {
		t.Errorf("a field tagged out is written anyway:\n%s", source)
	}
}

// The declaration can omit every zero-valued member without a tag on each one.
//
// What "zero" means depends on the type, so the condition is checked per shape
// rather than as one rule: a string compares against the empty string, a number
// against zero, and a slice against nil, which is the only comparison a slice
// has.
func TestOmittingEveryZeroValuedMember(t *testing.T) {
	source := written(t, "Composites", options("", true))

	for _, want := range []string{
		"v.Strings != nil",
		"v.Lookup != nil",
		"v.Pointer != nil",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("no condition %q:\n%s", want, source)
		}
	}

	// Without it, nothing is conditional at all.
	plain := written(t, "Composites", options("", false))
	if strings.Contains(plain, "v.Strings != nil") {
		t.Errorf("a member was omitted without being asked for:\n%s", plain)
	}
}

// options builds what a declaration wrote for this layer.
func options(style string, omitZero bool) model.Options {
	out := model.Options{Layer: "json"}
	if style != "" {
		out.Entries = append(out.Entries, model.Option{Key: "names", Value: style})
	}
	if omitZero {
		out.Entries = append(out.Entries, model.Option{Key: "omitzero", Value: "true"})
	}
	return out
}

// written returns the source of the codec generated for one fixture subject.
func written(t *testing.T, name string, opts model.Options) string {
	t.Helper()

	loaded := loadFixture(t)
	builder := subject.New(subject.Config{
		Fset:  loaded.Fset,
		Owned: loaded.Owned(),
		Docs:  loaded.FieldDocs(),
	})

	pkg, ok := loaded.Package(modelPkg)
	if !ok {
		t.Fatalf("the fixture has no package %s", modelPkg)
	}

	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", modelPkg, name)
	}
	held, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok {
		t.Fatalf("%s is a %T, want a named type", name, obj.Type())
	}

	built, problems := builder.Build(held, subject.Site{})
	if !problems.Empty() {
		t.Fatalf("modelling %s: %s", name, problems.Render())
	}

	unit, err := jsoncodec.New().Generate(&layer.Context{
		Model: &model.Model{
			Name: name, Form: model.FormInline, Subject: built,
			Pkg: pkg, Pos: token.Position{Filename: "person.go"},
		},
		Options: opts,
	}, shape.Shape{})
	if err != nil {
		t.Fatalf("generating a codec for %s: %v", name, err)
	}

	file := emit.File{Package: "model"}
	for _, key := range sortedKeys(unit.Provides) {
		one := unit.Provides[key]
		file.Sections = append(file.Sections, emit.Section{
			Decls: one.Decls, Comments: one.Comments, Fset: one.Fset,
		})
		file.Imports = append(file.Imports, one.Imports...)
	}

	out, err := file.Render()
	if err != nil {
		t.Fatalf("rendering the codec for %s: %v", name, err)
	}
	return string(out)
}

// A codec binds every package its body names, not only the one the type it is
// about comes from.
//
// Asked of the unit rather than of a rendered file, because a package holds one
// file for everything its declarations share: a second codec in it that happens
// to name the same package would bind it, and the first one's missing import
// would go unnoticed until somebody generated a package where it was the only
// codec. What a unit carries is what that package would have.
func TestWhatACodecBinds(t *testing.T) {
	cases := map[string]string{
		// Reached through named scalars alone, so nothing else in the codec
		// spells the package.
		"Scalared": "codecfixture/other",

		// Reached through a struct as well, which is the easy case and is here
		// so that the hard one is not the only one.
		"Elsewhere": "codecfixture/other",
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			unit := codecUnit(t, modelPkg, name)

			held, ok := unit.Provides[name]
			if !ok {
				// The key is the fully qualified type, which the test does not
				// spell out; the only unit about a struct of this name will do.
				for key, one := range unit.Provides {
					if strings.HasSuffix(key, "."+name) {
						held, ok = one, true
						break
					}
				}
			}
			if !ok {
				t.Fatalf("no codec was written for %s", name)
			}

			var bound []string
			for _, one := range held.Imports {
				bound = append(bound, one.Path)
			}
			if !slices.Contains(bound, want) {
				t.Errorf("%s's codec binds %v, and its body names %s", name, bound, want)
			}
		})
	}
}

// A codec binds nothing its body does not name.
//
// The other direction of [TestWhatACodecBinds], and the one a fixture cannot
// show by compiling: an import too many is a file that does not build, and a
// package whose other codecs happen to name the same import would hide it. A
// member written by a codec of its own is the case that produces one — it is
// reached without being spelled, so its package is a candidate that nothing
// names.
func TestWhatACodecDoesNotBind(t *testing.T) {
	for _, name := range []string{"Borrowed", "Scalared", "Elsewhere", "Nested", "Composites", "Stamped"} {
		t.Run(name, func(t *testing.T) {
			for key, held := range codecUnit(t, modelPkg, name).Provides {
				named := emit.Qualifiers(held.Decls)

				for _, one := range held.Imports {
					if one.Name == "_" || one.Name == "." {
						continue
					}
					if !named[one.Name] {
						t.Errorf("%s binds %s as %s, and never names it", key, one.Path, one.Name)
					}
				}
			}
		})
	}
}
