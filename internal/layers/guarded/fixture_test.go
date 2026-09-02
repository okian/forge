package guarded_test

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/compose"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/merge"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/plugin"
)

// The package the fixtures are generated into, and where their declarations
// were written.
const local = "example.com/model"

var declaredAt = token.Position{Filename: "model/person.go", Line: 12, Column: 6}

// subjectSource is the hand-written half of the fixture package: the subject a
// stack is built over, and nothing else.
//
// No declared type. Every stack here holds a lock, so none of them is
// transparent and all of them are written in the spec form — where the
// declaration is forge's and lives under the tag that excludes the generated
// file.
const subjectSource = "package model\n\n" +
	"// Person is what the containers hold.\n" +
	"type Person struct {\n\tID int\n\tName string\n}\n"

// timestampedSource is the same subject with a field from another package.
//
// For the stacks that put a refining layer beneath the lock. A projection is
// named after a field and spelled with that field's type, so this one arrives
// in the scope as a method naming a package the lock never heard of — which is
// the ordinary case rather than an exotic one, since most subjects worth
// locking have a timestamp on them.
const timestampedSource = "package model\n\n" +
	"import \"time\"\n\n" +
	"// Person is what the containers hold.\n" +
	"type Person struct {\n\tID int\n\tName string\n\tJoined time.Time\n}\n"

// person is the model of that subject.
func person() *plugin.Struct { return declaredIn(local, "model") }

// timestamped is the model of the subject that carries a foreign field.
func timestamped() *plugin.Struct {
	held := person()
	held.Fields = append(held.Fields, plugin.Field{
		Name: "Joined", Exported: true, Type: plugin.Classified{Type: stamp()},
	})

	return held
}

// stamp is the type of that field, built the way go/types would hand it over.
func stamp() types.Type {
	pkg := types.NewPackage("time", "time")
	obj := types.NewTypeName(token.NoPos, pkg, "Time", nil)

	return types.NewNamed(obj, types.NewStruct(nil, nil), nil)
}

// declaredIn is the same subject, declared in the package named.
//
// For the tests about a subject the file being generated into cannot write
// down. A package of the author's own two directories over is one forge will
// happily model and one this package has no bare name for, and it is the case
// that a check asking whether the subject is in this *module* lets through.
func declaredIn(path, name string) *plugin.Struct {
	pkg := types.NewPackage(path, name)
	obj := types.NewTypeName(token.NoPos, pkg, "Person", nil)

	return &plugin.Struct{
		Named: types.NewNamed(obj, types.NewStruct(nil, nil), nil),
		Fields: []plugin.Field{
			{Name: "ID", Exported: true, Type: plugin.Classified{Type: types.Typ[types.Int]}},
			{Name: "Name", Exported: true, Type: plugin.Classified{Type: types.Typ[types.String]}},
		},
	}
}

// over builds a declaration whose stack is the named markers, outermost first,
// with the directives an author would have written above it.
func over(declared string, markers []string, directives ...string) generate.Request {
	return of(person(), declared, markers, directives...)
}

// of is the same, over a subject the caller chooses.
func of(subject *plugin.Struct, declared string, markers []string, directives ...string) generate.Request {
	stack := make([]plugin.LayerRef, len(markers))
	for i, name := range markers {
		stack[i] = plugin.LayerRef{Origin: marker(name)}
	}

	written := make([]discover.Directive, len(directives))
	for i, text := range directives {
		name, args, _ := strings.Cut(strings.TrimPrefix(text, "//forge:"), " ")
		written[i] = discover.Directive{
			Layer: name, Args: args, Text: text,
			ArgsOffset: len("//forge:") + len(name) + 1,
			Pos:        token.Position{Filename: declaredAt.Filename, Line: declaredAt.Line - 1, Column: 1},
		}
	}

	return generate.Request{
		Model: &plugin.Model{
			Name: declared, Form: plugin.FormSpec, Subject: subject, Stack: stack,
			Pkg: &packages.Package{PkgPath: local},
			Pos: declaredAt,
		},
		Directives: written,
	}
}

// config is what a run generates with.
func config() generate.Config {
	return generate.Config{
		Catalog:   compose.Catalog{Registry: layers.Builtins(), DefaultStorage: layers.DefaultStorage()},
		Forge:     "v1.2.3",
		Markers:   "v1.2.3",
		Toolchain: "go1.27.0",
	}
}

// asked builds what the layer is handed for one declaration, without going
// through composition.
//
// For the tests that put a question to the layer directly. What composition
// would have worked out — the name to declare onto, the shape beneath — is
// given here instead, so that a test about one answer does not depend on every
// layer that would have contributed to it.
func asked(declared string, options ...string) *plugin.Context {
	entries := make([]model.Option, 0, len(options))
	for _, one := range options {
		key, value, _ := strings.Cut(one, "=")
		entries = append(entries, model.Option{Key: key, Value: value})
	}

	return &plugin.Context{
		Model: &plugin.Model{
			Name: declared, Form: plugin.FormSpec, Subject: person(),
			Pkg: &packages.Package{PkgPath: local},
			Pos: declaredAt,
		},
		Options: plugin.Options{Layer: "guarded", Entries: entries, Pos: declaredAt},
	}
}

// walking returns what a container beneath a lock offers, so that a test can
// take one thing away from it and see what the layer makes of the rest.
func walking(elem string) plugin.Shape {
	owner := marker("Slice")

	return plugin.Shape{
		Caps: plugin.Caps(plugin.Sized, plugin.Ordered, plugin.Indexed, plugin.Streamable, plugin.Structured),
		Surface: []plugin.Method{
			{Name: "Len", Signature: "() int", Owner: owner, Doc: "how many elements the container holds"},
			{Name: "All", Signature: "() iter.Seq[" + elem + "]", Owner: owner, Pointer: true, Doc: "walks the elements"},
			{Name: "Backward", Signature: "() iter.Seq[" + elem + "]", Owner: owner, Pointer: true, Doc: "walks them backwards"},
			{Name: "AppendSeq", Signature: "(seq iter.Seq[" + elem + "])", Owner: owner, Pointer: true, Doc: "adds what a sequence yields"},
			{Name: "Reset", Signature: "()", Owner: owner, Pointer: true, Doc: "empties the container"},
		},
	}
}

// marker builds a reference to one of the markers forge declares.
func marker(name string) plugin.TypeRef {
	return plugin.TypeRef{Pkg: plugin.MarkerPkg, Name: name}
}

// printed renders what a layer contributed as the file it would go into, so
// that a test about one layer's output can read it as source.
//
// Through the same merge and emit steps a run uses. What a layer hands over is
// syntax, and what anybody reviews is text.
func printed(t *testing.T, unit plugin.Unit) string {
	t.Helper()

	merged := merge.Units(unit)

	out, err := emit.File{
		Package:  "model",
		Decl:     "Persons",
		Pos:      declaredAt,
		Imports:  merged.Imports,
		Sections: merged.Sections,
	}.Render()
	if err != nil {
		t.Fatalf("rendering the file: %v", err)
	}

	return string(out)
}
