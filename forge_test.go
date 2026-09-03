package forge_test

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
)

// markerPath is the import path spec files use to reach the markers.
const markerPath = "github.com/okian/forge"

// shape is the declaration a marker's kind requires of it. Storage, refining,
// decorator and transport markers are slices, so a single-layer inline
// declaration has a usable underlying type; element markers cannot be
// transparent and are zero-sized phantom structs instead.
type shape int

const (
	shapeContainer shape = iota
	shapeElement

	// shapeBridge is a marker over two types: a source it reads and a target
	// it writes. Zero-sized like an element, and the one marker that takes two
	// type parameters — a bridge composes with nothing, so no stack threads
	// through it.
	shapeBridge
)

// markers lists every marker the package declares, with the shape its kind
// requires. It is the specification the tests below check the package against:
// a marker added without being listed here, removed, or declared with the
// wrong underlying type is a failure.
var markers = map[string]shape{
	// Element.
	"Json":     shapeElement,
	"Validate": shapeElement,
	"Clone":    shapeElement,
	"Hash":     shapeElement,
	"Builder":  shapeElement,
	"Patch":    shapeElement,
	"Redact":   shapeElement,
	"Enum":     shapeElement,
	"Default":  shapeElement,
	"Diff":     shapeElement,
	"Fault":    shapeElement,
	"Binary":   shapeElement,

	// Storage.
	"Slice": shapeContainer,
	"Ring":  shapeContainer,
	"Set":   shapeContainer,
	"LRU":   shapeContainer,
	"Index": shapeContainer,
	"Heap":  shapeContainer,

	// Refining.
	"Collection": shapeContainer,
	"Sorted":     shapeContainer,
	"Page":       shapeContainer,

	// Decorator.
	"Guarded": shapeContainer,
	"Atomic":  shapeContainer,

	// Transport.
	"Csv": shapeContainer,

	// Bridge.
	"Map": shapeBridge,
}

// refusingImporter rejects every import. Markers must be reachable from a spec
// file without dragging anything else along, so an import in the marker
// package is a defect rather than a detail.
type refusingImporter struct{}

func (refusingImporter) Import(path string) (*types.Package, error) {
	return nil, fmt.Errorf("marker package must not import %q", path)
}

// markerImporter resolves the marker package and nothing else, so a spec
// fixture that reaches for an unrelated dependency fails loudly.
type markerImporter struct {
	markers *types.Package
}

func (i markerImporter) Import(path string) (*types.Package, error) {
	if path == markerPath {
		return i.markers, nil
	}
	return nil, fmt.Errorf("spec fixture must not import %q", path)
}

// loadMarkers type-checks the marker package from its own sources, which sit in
// the current directory because that is where go test runs a package's tests.
//
// File selection goes through go/build rather than a directory listing, so
// build constraints, platform suffixes and the ignored file-name prefixes apply
// exactly as they do to a real build. Checking a set of files the compiler
// would never assemble would make every assertion below meaningless.
func loadMarkers(t *testing.T) (*token.FileSet, *types.Package) {
	t.Helper()

	dir, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	if len(dir.GoFiles) == 0 {
		t.Fatal("no marker sources found in the package directory")
	}

	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(dir.GoFiles))
	for _, name := range dir.GoFiles {
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, file)
	}

	conf := types.Config{Importer: refusingImporter{}}
	pkg, err := conf.Check(markerPath, fset, files, nil)
	if err != nil {
		t.Fatalf("marker package does not type-check: %v", err)
	}
	return fset, pkg
}

// checkSpec type-checks src as the sole file of a package that imports the
// markers. Build constraints play no part: go/types never evaluates them, so a
// //go:build line in a fixture is inert and present only because that is how
// the file would be written in a real package.
func checkSpec(fset *token.FileSet, markers *types.Package, src string) (*types.Package, error) {
	file, err := parser.ParseFile(fset, "spec.go", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	conf := types.Config{Importer: markerImporter{markers: markers}}
	return conf.Check("example.com/model", fset, []*ast.File{file}, nil)
}

// bridged asserts the shape a bridge marker must have: a zero-sized phantom
// struct whose underlying type changes with either type parameter. Dropping
// one parameter is checked by holding the other fixed, because two
// instantiations differing in both would still differ with one dropped.
func bridged(t *testing.T, named *types.Named, person, session types.Type) {
	t.Helper()

	inst := instantiate(t, named, person, session)
	structure, ok := inst.Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("%s[Person, Session] has underlying type %s, want a struct", named.Obj().Name(), inst.Underlying())
	}
	if size := types.SizesFor("gc", "amd64").Sizeof(structure); size != 0 {
		t.Errorf("%s[Person, Session] occupies %d bytes, want a zero-sized placeholder", named.Obj().Name(), size)
	}

	if other := instantiate(t, named, person, person); types.Identical(structure, other.Underlying()) {
		t.Errorf("%s[Person, Session] and %s[Person, Person] share underlying type %s; the second type parameter is unused",
			named.Obj().Name(), named.Obj().Name(), structure)
	}
	if other := instantiate(t, named, session, session); types.Identical(structure, other.Underlying()) {
		t.Errorf("%s[Person, Session] and %s[Session, Session] share underlying type %s; the first type parameter is unused",
			named.Obj().Name(), named.Obj().Name(), structure)
	}
}

// namedStruct builds a named struct type without running a type-checker, for
// use as a distinctive type argument.
func namedStruct(pkgPath, pkgName, typeName string) *types.Named {
	pkg := types.NewPackage(pkgPath, pkgName)
	obj := types.NewTypeName(token.NoPos, pkg, typeName, nil)
	return types.NewNamed(obj, types.NewStruct(nil, nil), nil)
}

// instantiate applies concrete type arguments to a generic marker.
func instantiate(t *testing.T, generic *types.Named, args ...types.Type) types.Type {
	t.Helper()

	inst, err := types.Instantiate(nil, generic, args, true)
	if err != nil {
		t.Fatalf("%s does not accept concrete type arguments: %v", generic.Obj().Name(), err)
	}
	return inst
}

// A marker that drops its type parameter, or that declares the wrong
// underlying type for its kind, still compiles and still type-checks every
// spec fixture. Only these assertions catch it.
func TestMarkerDeclarations(t *testing.T) {
	_, pkg := loadMarkers(t)
	scope := pkg.Scope()

	// Distinctive subjects, so that a marker that ignores its type argument
	// cannot be mistaken for one that uses it.
	person := namedStruct("example.com/model", "model", "Person")
	session := namedStruct("example.com/model", "model", "Session")

	for name, want := range markers {
		t.Run(name, func(t *testing.T) {
			obj := scope.Lookup(name)
			if obj == nil {
				t.Fatalf("marker %s is not declared", name)
			}
			named, ok := obj.Type().(*types.Named)
			if !ok {
				t.Fatalf("%s has type %T, want a named type", name, obj.Type())
			}

			// Stacks stay linear because a marker in one takes exactly one
			// type argument: the layer below it. A bridge is in no stack and
			// takes its two types instead.
			expect := 1
			if want == shapeBridge {
				expect = 2
			}
			params := named.TypeParams()
			if params.Len() != expect {
				t.Fatalf("%s takes %d type parameters, want exactly %d", name, params.Len(), expect)
			}
			for i := range params.Len() {
				if got := params.At(i).Constraint().String(); got != "any" {
					t.Errorf("%s constrains type parameter %d to %s, want any", name, i, got)
				}
			}
			if n := named.NumMethods(); n != 0 {
				t.Errorf("%s declares %d methods; a marker carries no behavior", name, n)
			}

			if want == shapeBridge {
				bridged(t, named, person, session)
				return
			}

			inst := instantiate(t, named, person)

			switch want {
			case shapeContainer:
				slice, ok := inst.Underlying().(*types.Slice)
				if !ok {
					t.Fatalf("%s[Person] has underlying type %s, want a slice", name, inst.Underlying())
				}
				if !types.Identical(slice.Elem(), person) {
					t.Errorf("%s[Person] has element type %s, want Person", name, slice.Elem())
				}

			case shapeElement:
				structure, ok := inst.Underlying().(*types.Struct)
				if !ok {
					t.Fatalf("%s[Person] has underlying type %s, want a struct", name, inst.Underlying())
				}
				if size := types.SizesFor("gc", "amd64").Sizeof(structure); size != 0 {
					t.Errorf("%s[Person] occupies %d bytes, want a zero-sized placeholder", name, size)
				}

				// Two instantiations must not share an underlying type, or a
				// value marked for one subject would convert freely to another.
				other := instantiate(t, named, session)
				if types.Identical(structure, other.Underlying()) {
					t.Errorf("%s[Person] and %s[Session] share underlying type %s; the type parameter is unused",
						name, name, structure)
				}
			}
		})
	}
}

func TestMarkerSetMatchesTheCatalog(t *testing.T) {
	_, pkg := loadMarkers(t)

	scope := pkg.Scope()
	for _, name := range scope.Names() {
		if !scope.Lookup(name).Exported() {
			continue
		}
		if _, ok := markers[name]; !ok {
			t.Errorf("%s is declared but is not a known marker", name)
		}
	}

	for name := range markers {
		if scope.Lookup(name) == nil {
			t.Errorf("marker %s is missing from the package", name)
		}
	}
}

// A marker nothing claims is a declaration that type-checks and then resolves
// to a stack entry no layer answers for; a layer claiming a marker that is not
// declared can never be reached at all. Both are silent, and each is invisible
// to the other's own tests, so the two lists are compared here.
func TestEveryMarkerIsClaimedByALayer(t *testing.T) {
	registry := layers.Builtins()

	for name := range markers {
		if _, ok := registry.Lookup(model.TypeRef{Pkg: markerPath, Name: name}); !ok {
			t.Errorf("marker %s is declared and no layer claims it", name)
		}
	}

	for _, claimed := range registry.All() {
		origin := claimed.Origin()
		if origin.Pkg != markerPath {
			t.Errorf("%s claims a marker outside the marker package", origin)
			continue
		}
		if _, ok := markers[origin.Name]; !ok {
			t.Errorf("a layer claims %s, which the marker package does not declare", origin.Name)
		}
	}
}

func TestMarkerPackageCarriesNoBehavior(t *testing.T) {
	_, pkg := loadMarkers(t)

	if imports := pkg.Imports(); len(imports) != 0 {
		t.Errorf("marker package imports %v, want nothing", imports)
	}

	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)

		// An unexported declaration is invisible to users and to godoc, so only
		// exported ones are constrained to be types.
		if obj.Exported() {
			if _, ok := obj.(*types.TypeName); !ok {
				t.Errorf("%s is a %T; the marker package exports types only", name, obj)
			}
		}

		// No type in the package, exported or not, may carry a method.
		if named, ok := obj.Type().(*types.Named); ok {
			if n := named.NumMethods(); n != 0 {
				t.Errorf("%s declares %d methods; a marker carries no behavior", name, n)
			}
		}
	}
}

func TestSpecFileTypeChecksDotImported(t *testing.T) {
	fset, markers := loadMarkers(t)

	const src = `//go:build forgespec

package model

import . "` + markerPath + `"

type Person struct {
	Name string ` + "`json:\"name\"`" + `
	Age  int    ` + "`json:\"age\"`" + `
}

type Session struct {
	ID string
}

//forge:collection sort=Age index=Name
//forge:ring cap=1024 overflow=overwrite
//forge:json omitzero=true
type Persons Collection[Ring[Json[Person]]]

//forge:guarded encode=locked
type Sessions Guarded[LRU[Session]]

type Validated Collection[Validate[Person]]
type Rows Csv[Person]
`

	pkg, err := checkSpec(fset, markers, src)
	if err != nil {
		t.Fatalf("spec file does not type-check: %v", err)
	}
	for _, name := range []string{"Persons", "Sessions", "Validated", "Rows"} {
		if pkg.Scope().Lookup(name) == nil {
			t.Errorf("declaration %s missing from the checked package", name)
		}
	}
}

func TestSpecFileTypeChecksQualified(t *testing.T) {
	fset, markers := loadMarkers(t)

	const src = `package model

import "` + markerPath + `"

type Celsius float64

type Reading struct {
	At    int64
	Value Celsius
}

type Readings forge.Collection[forge.Ring[forge.Json[Reading]]]
type Recent forge.Slice[Reading]
`

	if _, err := checkSpec(fset, markers, src); err != nil {
		t.Fatalf("spec file does not type-check: %v", err)
	}
}

// The underlying type a declaration ends up with is what decides whether it may
// be written inline or has to live in a spec file, so each case is worth
// pinning down.
func TestUnderlyingTypesMatchMarkerKinds(t *testing.T) {
	fset, markers := loadMarkers(t)

	const src = `package model

import . "` + markerPath + `"

type Person struct{ Name string }

type Persons Collection[Person]
type Stored Slice[Person]
type Nested Collection[Ring[Person]]
type Marked Json[Person]
`

	pkg, err := checkSpec(fset, markers, src)
	if err != nil {
		t.Fatalf("spec file does not type-check: %v", err)
	}

	qualifier := func(p *types.Package) string { return p.Name() }
	underlying := func(name string) string {
		return types.TypeString(pkg.Scope().Lookup(name).Type().Underlying(), qualifier)
	}

	// A single container over the subject leaves an underlying type a caller can
	// use directly, which is what makes the inline form legal.
	for _, name := range []string{"Persons", "Stored"} {
		if got, want := underlying(name), "[]model.Person"; got != want {
			t.Errorf("%s underlying type = %s, want %s", name, got, want)
		}
	}

	// Nesting does not compose that way. The underlying type is a slice of the
	// layer below, not of the subject, which is why a nested declaration belongs
	// in a spec file where nothing links it.
	if got, want := underlying("Nested"), "[]forge.Ring[model.Person]"; got != want {
		t.Errorf("Nested underlying type = %s, want %s", got, want)
	}

	// An element marker cannot be transparent at all, so its underlying type is
	// a zero-sized placeholder and carries no data.
	marked := pkg.Scope().Lookup("Marked").Type().Underlying()
	if _, ok := marked.(*types.Struct); !ok {
		t.Errorf("Marked underlying type = %s, want a struct placeholder", marked)
	}
	if size := types.SizesFor("gc", "amd64").Sizeof(marked); size != 0 {
		t.Errorf("Marked placeholder occupies %d bytes, want 0", size)
	}
}

func TestSpecFileRejectsInvalidDeclarations(t *testing.T) {
	fset, markers := loadMarkers(t)

	cases := map[string]struct {
		decl string
		// mentions is text the compiler's own error must contain, so that a
		// fixture broken for some unrelated reason cannot pass as a success.
		mentions string
	}{
		"two type arguments":     {"type Bad Collection[Person, Person]", "Collection"},
		"no type argument":       {"type Bad Collection", "Collection"},
		"uninstantiated nesting": {"type Bad Collection[Ring]", "Ring"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			src := "package model\n\nimport . \"" + markerPath + "\"\n\ntype Person struct{ Name string }\n\n" + tc.decl + "\n"

			_, err := checkSpec(fset, markers, src)
			if err == nil {
				t.Fatal("declaration type-checked, want an error")
			}
			if !strings.Contains(err.Error(), tc.mentions) {
				t.Errorf("error %q does not mention %q", err, tc.mentions)
			}
		})
	}
}
