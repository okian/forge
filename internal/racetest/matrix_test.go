package racetest_test

import (
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/compose"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/racetest"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/words"
)

// Where the matrix lives, and what it is generated as.
const (
	matrixDir  = "matrix"
	matrixPkg  = "github.com/okian/forge/internal/racetest/matrix"
	matrixName = "matrix"

	// markerPkg is the import path the declarations name their markers from.
	markerPkg = "github.com/okian/forge"

	// specFile is where the declarations are written, which is where a
	// diagnostic about one of them points.
	specFile = "zz_race_spec.go"
)

// The methods this harness knows how to call, by the name the contract gives
// them.
//
// Named here rather than assumed anywhere, because a layer that offers
// something else is a layer this harness cannot write a test for — and the
// point of naming them is that the harness says so instead of writing a
// shorter test.
const (
	writeScope  = "Do"
	readScope   = "RDo"
	walkMethod  = "All"
	addMethod   = "AppendSeq"
	countMethod = "Len"
	codecMethod = "MarshalJSON"
)

// The stack every concurrent layer is put through, beneath the layer itself.
//
// A bounded container of elements that carry a codec, which is the arrangement
// with the most surface for a concurrent layer to get wrong: a walk, a count, a
// copy and a document, each holding the value for a different length of time.
var beneath = []string{"Ring", "Json"}

// capacity is what the ring beneath each declaration holds.
const capacity = 64

// Every layer that says a stack is safe to share has a stress test, and it is
// the one this harness writes today.
//
// The matrix grows on its own, which is the whole of why it is generated: the
// declarations come from the catalog rather than from a file somebody edits, so
// a concurrent layer added on a busy week arrives with a package, a stress test
// and a run under the detector, and arrives with them in a diff. So does an
// option — a layer that offers a choice about how it locks gets a declaration
// per choice, because the choice is the thing worth stressing.
//
// What is checked here is that the committed files are what the harness
// produces now. What the files themselves check is the thing no test in this
// package can — that the generated code does not race — and they check it by
// being compiled and run under the detector along with everything else.
func TestTheRaceMatrix(t *testing.T) {
	registry := layers.Builtins()
	held := declaring(concurrent(t, registry))

	spec, err := racetest.Spec(matrixName, markerPkg, held)
	if err != nil {
		t.Fatalf("writing the declarations: %v", err)
	}

	written := map[string][]byte{specFile: spec}

	// Every declaration through one call, because generation answers for a
	// package rather than for a declaration: the stub file and the shared
	// helpers are one file each however many declarations asked for them, and
	// a call per declaration would have each one's copy overwrite the last.
	requests := make([]generate.Request, len(held))
	for i, one := range held {
		requests[i] = request(one, at(t, spec, one.Name))
	}

	files, diags := generate.Package(matrixPkg, matrixName, requests, config(registry))
	if !diags.Empty() {
		t.Fatalf("generating the matrix was refused:\n%s", diags.Render())
	}
	for _, file := range files {
		written[file.Name] = file.Content
	}

	for i, one := range held {
		stress, err := racetest.Write(asking(t, registry, requests[i], one))
		if err != nil {
			t.Fatalf("writing the stress test for %s: %v", one.Name, err)
		}
		written["zz_race_"+strings.ToLower(one.Name)+"_test.go"] = stress
	}

	recorded(t, written)
}

// concurrent returns every layer in the catalog that makes a stack safe to
// share, in registry order.
//
// By what it exposes rather than by name, because that is the claim the matrix
// exists to hold layers to: a layer that adds Concurrent is telling the layers
// above it that they may run goroutines against this stack, and this is what
// asks whether that is true.
//
// A layer that says it is unfinished is left out, for the reason the
// composition matrix leaves one out: a stub generates nothing, and running a
// stress test against nothing would be asserting that unwritten code is safe.
func concurrent(t *testing.T, registry *layer.Registry) []layer.Layer {
	t.Helper()

	var out []layer.Layer
	for _, one := range registry.All() {
		if described, ok := one.(layer.Described); ok && described.Stage() != layer.StageReady {
			continue
		}
		if one.Shape(nil, shape.Shape{}).Caps.Has(shape.Concurrent) {
			out = append(out, one)
		}
	}

	if len(out) == 0 {
		t.Fatal("no layer in the catalog says a stack is safe to share, so the matrix runs nothing")
	}
	return out
}

// declaring returns the declarations each layer is put through: one with
// nothing written, and one per choice the layer offers.
//
// A choice about how a layer locks is the thing most worth stressing, and it is
// the half a declaration written with defaults never reaches — encoding that
// holds the lock across a caller's writer is a different arrangement from
// encoding a copy, and only one of them is the default.
//
// Every value except the default, since a declaration writing the default is
// the declaration above it with an extra line.
//
// A choice spelled any way other than a fixed set of values gets no declaration
// of its own, because there is no set of values to enumerate: a number or a
// field name is a choice with no shortlist, and picking one for it would be
// this harness inventing a declaration nobody wrote. The day a concurrent layer
// offers one, it wants a case written for it by whoever knows what values are
// worth stressing.
//
// Nor are combinations covered. Two choices are two declarations rather than
// four, because what a stress test is for is the arrangement each choice
// produces and not the cross-product of them — and a cross-product grows
// faster than anybody reads.
func declaring(held []layer.Layer) []racetest.Declared {
	var out []racetest.Declared

	for _, one := range held {
		name := one.Origin().Name
		stack := append([]string{name}, beneath...)

		out = append(out, racetest.Declared{
			Name: name + "Persons", Layer: name, Stack: stack, Subject: "Person",
			Directives: []string{ringOption()},
		})

		for _, option := range one.OptionSchema() {
			for _, value := range option.Values {
				if value == option.Default {
					continue
				}

				out = append(out, racetest.Declared{
					Name:    name + "Persons" + words.Upper(option.Key) + words.Upper(value),
					Layer:   name,
					Stack:   stack,
					Subject: "Person",
					Directives: []string{
						ringOption(),
						"forge:" + strings.ToLower(name) + " " + option.Key + "=" + value,
					},
				})
			}
		}
	}

	return out
}

// ringOption is the directive that sizes the container beneath every
// declaration.
func ringOption() string { return "forge:ring cap=" + strconv.Itoa(capacity) }

// at returns the line a declaration was written on in the spec file.
//
// Found in what was written rather than assumed, because there is more than one
// declaration and a constant shared between them would point every diagnostic
// at whichever one it was written for.
//
// Not finding one is reported rather than passed over. It cannot happen while
// the writer of the spec file and this agree about how a declaration is
// spelled, which is the point: the day they stop agreeing, every position in
// the run becomes a line nobody wrote, and a zero returned quietly here is how
// that would arrive.
func at(t *testing.T, spec []byte, name string) token.Position {
	t.Helper()

	for i, line := range strings.Split(string(spec), "\n") {
		if strings.HasPrefix(line, "type "+name+" ") {
			return token.Position{Filename: specFile, Line: i + 1, Column: 6}
		}
	}

	t.Fatalf("%s declares no %s, so nothing generated from it can be pointed at", specFile, name)
	return token.Position{}
}

// asking works out what the stress test for a declaration is written from.
//
// Off the composed stack rather than off the layer, because what a stress test
// reaches is what the declaration ended up offering: which methods are on the
// declared type, which are reached through a scope, and whether the elements
// turned out to have a codec. A harness that read the layer would be describing
// what the layer does in isolation, which is not what anybody calls.
func asking(t *testing.T, registry *layer.Registry, req generate.Request, held racetest.Declared) racetest.Asked {
	t.Helper()

	composed, diags := compose.Compose(compose.Declaration{
		Stack: req.Model.Stack, Subject: req.Model.Subject, Pos: req.Model.Pos, Model: req.Model,
	}, catalog(registry))
	if !diags.Empty() {
		t.Fatalf("composing %s was refused:\n%s", held.Name, diags.Render())
	}

	// The outermost step is the concurrent layer, and what is beneath it is
	// what a scope reaches — which is where the walking and adding come from.
	top := composed.Steps[len(composed.Steps)-1]

	out := racetest.Asked{
		Package:  matrixName,
		Declared: held.Name,
		Elem:     held.Subject,
		Make:     making(t, top, held.Name),
		View:     handing(composed.Exposed),

		// Looked up rather than assumed, so that a layer offering something
		// else reaches the harness's own refusal — which says what it needed —
		// rather than a file that does not compile.
		Scope:     offered(composed.Exposed, writeScope),
		ReadScope: offered(composed.Exposed, readScope),
		Walk:      offered(top.Below, walkMethod),
		Append:    offered(top.Below, addMethod),
		Counts:    offered(composed.Exposed, countMethod),
		Encodes:   offered(composed.Exposed, codecMethod),
	}

	out.Copies = copying(composed.Exposed, held.Subject)

	out.Reads = rest(t, composed.Exposed, held.Name, out)

	return out
}

// offered returns a method's name where the shape has one, and nothing where it
// does not.
func offered(of shape.Shape, name string) string {
	if _, has := of.Method(name); !has {
		return ""
	}
	return name
}

// copying returns the method that hands back a copy of the elements, found by
// what it answers with rather than by what it is called.
//
// By shape because the name is the layer's to choose and the shape is not: a
// method taking nothing and answering with a slice of the element is a copy
// whatever it is called, and reading one element by element is what turns a
// copy that aliased the container into a race somebody is part of. Looking it
// up by name would leave a layer that called it anything else with its copy
// discarded — which is exactly the gap this closes.
//
// What it does not find is a copy of the *container*: a Clone answering with
// the declared type is a copy whose elements this has no way to reach, and it
// is read as an ordinary discarded call. Closing that needs the harness to know
// that what came back is another container, which is a question about the type
// rather than about the signature.
func copying(exposed shape.Shape, elem string) string {
	for _, one := range exposed.Surface {
		params, results, err := one.Rendered()
		if err != nil || len(params) != 0 || len(results) != 1 {
			continue
		}
		if results[0] == "[]"+elem {
			return one.Name
		}
	}
	return ""
}

// rest returns the methods on the declared type the harness has not already
// accounted for, and complains about a readable one it cannot call.
//
// The complaint is the point. A method that takes nothing and answers with
// something is a way to read a value several goroutines share, so one the
// stress test never calls is a way in nothing checks — and a harness that
// quietly skipped it would go on claiming, in the comment it writes into the
// test, that every route is taken.
//
// A method that answers with nothing is left alone and is not claimed. What it
// does cannot be read off its signature: it changes something, or it blocks, or
// both — Lock is all three — and a harness that called one because it took no
// arguments would be as likely to deadlock the run as to stress anything. The
// generated comment names what is taken rather than promising everything, so
// leaving these out understates the test instead of misdescribing it.
func rest(t *testing.T, exposed shape.Shape, declared string, of racetest.Asked) []string {
	t.Helper()

	accounted := []string{of.Scope, of.ReadScope, of.Counts, of.Copies, of.Encodes}
	if of.Encodes != "" {
		// The appender is the marshaller's implementation: the harness calling
		// MarshalJSON stresses AppendJSON on every call, so the way in is
		// checked under the name the harness can call it by.
		accounted = append(accounted, "AppendJSON")
	}

	var out []string
	for _, one := range exposed.Surface {
		if slices.Contains(accounted, one.Name) {
			continue
		}

		params, results, err := one.Rendered()
		if err == nil && len(params) == 0 && len(results) == 0 {
			continue
		}
		if err != nil || len(params) != 0 || len(results) != 1 {
			t.Errorf("%s offers %s%s, which the race harness has no way to call — "+
				"every method that reads a shared value is a way in, and one nothing "+
				"calls is a way in nothing checks", declared, one.Name, one.Signature)
			continue
		}

		out = append(out, one.Name)
	}

	return out
}

// handing returns the type a scope hands its function, read off the scope's own
// signature.
//
// Read rather than assembled from the declaration's name, because what a view
// is called is the layer's to decide: a harness that spelled it itself would
// agree with the layer until the day one of them changed.
//
// Nothing where the signature is not one function taking one named type, which
// leaves the harness with no view and its own refusal to give.
func handing(exposed shape.Shape) string {
	one, has := exposed.Method(writeScope)
	if !has {
		return ""
	}

	params, results, err := one.Rendered()
	if err != nil || len(params) != 1 || len(results) != 0 {
		return ""
	}

	// Written as "func(v X)", so what is wanted is the name before the closing
	// bracket — and nothing if it is spelled any other way.
	held, closes := strings.CutSuffix(params[0], ")")
	if !closes {
		return ""
	}

	at := strings.LastIndex(held, " ")
	if at < 0 {
		return ""
	}
	return held[at+1:]
}

// making returns the expression that makes one of the declared type, and
// nothing where the zero value is one.
func making(t *testing.T, step compose.Step, declared string) string {
	t.Helper()

	if step.Holds == nil {
		return ""
	}
	if len(step.Holds.Params) > 0 {
		// The matrix writes the size into the declaration, so nothing is left
		// for a constructor to be told. One that asked for something would be
		// asking this harness to invent a value, which is a decision it has no
		// grounds to make.
		t.Fatalf("%s is made by a call taking %v, and the matrix has nothing to pass",
			declared, step.Holds.Params)
	}

	return "New" + declared + "()"
}

// request builds the declaration forge generates from.
func request(held racetest.Declared, pos token.Position) generate.Request {
	stack := make([]model.LayerRef, len(held.Stack))
	for i, one := range held.Stack {
		stack[i] = model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: one}}
	}

	written := make([]discover.Directive, len(held.Directives))
	for i, text := range held.Directives {
		name, args, _ := strings.Cut(strings.TrimPrefix(text, "forge:"), " ")
		written[i] = discover.Directive{
			Layer: name, Args: args, Text: "//" + text,
			ArgsOffset: len("//forge:") + len(name) + 1,
			Pos:        token.Position{Filename: pos.Filename, Line: pos.Line - len(held.Directives) + i, Column: 1},
		}
	}

	return generate.Request{
		Model: &model.Model{
			Name: held.Name, Form: model.FormSpec, Subject: person(), Stack: stack,
			Pkg: &packages.Package{PkgPath: matrixPkg},
			Pos: pos,
		},
		Directives: written,
	}
}

// person is the model of the subject the matrix declares over, which is the one
// written by hand in the package itself.
func person() *model.Struct {
	pkg := types.NewPackage(matrixPkg, matrixName)
	obj := types.NewTypeName(token.NoPos, pkg, "Person", nil)

	fields := []model.Field{
		{Name: "ID", Exported: true, Type: model.Classified{Type: types.Typ[types.Int]}},
		{Name: "Name", Exported: true, Type: model.Classified{Type: types.Typ[types.String]}},
	}

	// The underlying struct as well as the field list, so that the model
	// describes one type rather than two half-agreeing ones: nothing reads the
	// underlying struct today, and a model whose two halves disagree is a
	// fixture that will mislead whoever writes the stage that does.
	held := make([]*types.Var, len(fields))
	for i, one := range fields {
		held[i] = types.NewField(token.NoPos, pkg, one.Name, one.Type.Type, false)
	}

	return &model.Struct{
		Named:  types.NewNamed(obj, types.NewStruct(held, nil), nil),
		Fields: fields,
	}
}

// config and catalog are what the matrix generates and composes with.
func config(registry *layer.Registry) generate.Config {
	return generate.Config{
		Catalog:   catalog(registry),
		Forge:     "v1.2.3",
		Markers:   "v1.2.3",
		Toolchain: "go1.27.0",
	}
}

func catalog(registry *layer.Registry) compose.Catalog {
	return compose.Catalog{Registry: registry, DefaultStorage: layers.DefaultStorage()}
}

// recorded holds the matrix package to what the harness produces, and rewrites
// it when asked.
//
// Into the package itself rather than into testdata, because these files are
// meant to be built: a recorded copy proves the harness has not changed, and
// only a compiled and executed one proves the generated code does not race.
//
// It also reports a file nothing writes any more. A stale one would sit in a
// package that compiles, looking like coverage of a layer that no longer has
// any.
func recorded(t *testing.T, written map[string][]byte) {
	t.Helper()

	for _, name := range slices.Sorted(maps.Keys(written)) {
		goldentest.At(t, filepath.Join(matrixDir, name), written[name])
	}

	leftover(t, written)
}

// leftover reports a generated file in the matrix that nothing writes any more.
func leftover(t *testing.T, written map[string][]byte) {
	t.Helper()

	found, err := os.ReadDir(matrixDir)
	if err != nil {
		t.Fatalf("reading the matrix: %v", err)
	}

	for _, one := range found {
		name := one.Name()
		if !strings.HasPrefix(name, "zz_") {
			continue
		}
		if _, writes := written[name]; !writes {
			t.Errorf("%s is committed and nothing writes it any more; delete it or find out why", name)
		}
	}
}
