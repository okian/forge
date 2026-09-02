package collection_test

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/layers/collection"
	"github.com/okian/forge/internal/layers/slice"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/plugin"
)

// What the layer tells the layers above it is on the declared type has to be
// what it puts there.
//
// The two are written separately on purpose — one is a report and one is a
// builder — because a surface read back from the syntax would report whatever
// the builder happened to emit, a helper it keeps to itself included, and a
// layer above written against a helper would break the first time one was
// renamed. Nothing but this holds them together.
//
// The unexported helpers the built methods call are excluded here rather than
// listed, since they are the half of the output that is not a contract.
func TestTheSurfaceIsWhatIsEmitted(t *testing.T) {
	// Both counts of sort key, because one of them decides whether two more
	// methods exist. A declaration naming exactly one order gets the three
	// sort.Sort takes and one naming two gets none of them, so a surface
	// checked only against the second would never see them missing.
	for _, directive := range []string{
		"//forge:collection sort=Name,ID index=Name",
		"//forge:collection sort=Name index=ID",
	} {
		ctx := declaration(t, directive)

		// The whole stack rather than this layer alone, because that is what
		// the output holds: the file these tests render is the storage layer's
		// methods and this layer's together, and comparing half a file against
		// half a report would pass whichever half went wrong.
		promised := names(overStorage(ctx))
		slices.Sort(promised)

		emitted := published(t, generated(t, ctx), ctx.Model.Name)
		if !slices.Equal(emitted, promised) {
			t.Errorf("under %s the stack says %v and the output holds %v", directive, promised, emitted)
		}
	}
}

// Every method the surface promises is spelled the way the output declares it,
// so that a layer above can wrap one without reading the file.
//
// The names alone would pass over a projection reported as returning the
// subject, which is the mistake a decorator generating a wrapper would turn
// into output that does not compile.
func TestTheSurfaceSpellsWhatIsEmitted(t *testing.T) {
	ctx := declaration(t, "//forge:collection sort=Name index=ID")

	declared := signatures(t, generated(t, ctx), ctx.Model.Name)
	for _, method := range exposed(ctx).Surface {
		want, ok := declared[method.Name]
		if !ok {
			t.Errorf("the surface promises %s and the output does not declare it", method.Name)
			continue
		}
		if method.Signature != want.signature {
			t.Errorf("the surface says %s%s and the output declares %s%s",
				method.Name, method.Signature, method.Name, want.signature)
		}
		if method.Pointer != want.pointer {
			t.Errorf("the surface says %s takes a pointer receiver = %v and the output declares %v",
				method.Name, method.Pointer, want.pointer)
		}
		if method.Pointer {
			t.Errorf("%s is reported as taking a pointer receiver, and every method here answers "+
				"a question rather than changing the collection", method.Name)
		}
		if method.Owner != collection.New().Origin() {
			t.Errorf("%s is owned by %s, want the layer that emits it", method.Name, method.Owner)
		}
	}
}

// What a declaration asked for is what the surface reports, so an option that
// adds a method adds it here too.
func TestTheSurfaceFollowsTheDeclaration(t *testing.T) {
	// One projection per exported field, and nothing else. The subject's
	// unexported field is not among them.
	bare := names(exposed(declaration(t)))
	want := []string{"Seq", "IDs", "Names", "Addresses", "Cities", "Joineds", "Tags"}
	if !slices.Equal(bare, want) {
		t.Errorf("a declaration asking for nothing exposes %v, want %v", bare, want)
	}

	asked := names(exposed(declaration(t, "//forge:collection sort=Name index=ID")))
	want = append(slices.Clone(want), "SortedByName", "ByID")
	if !slices.Equal(asked, want) {
		t.Errorf("a declaration asking for a sort and an index exposes %v, want %v", asked, want)
	}
}

// A layer asked what it exposes without a declaration answers with the part
// that does not depend on one, rather than refusing or guessing.
//
// What asks that way is asking what the layer is — the list command, a registry
// walk — rather than what it would emit for anybody in particular. Every other
// method this layer emits is named after a field or an option, and there is no
// honest answer for those without the declaration that names them.
func TestASurfaceWithoutADeclaration(t *testing.T) {
	for _, ctx := range []*plugin.Context{nil, {}, {Model: nil}} {
		got := collection.New().Shape(ctx, plugin.Shape{})

		if want := []string{"Seq"}; !slices.Equal(names(got), want) {
			t.Errorf("a shape asked without a declaration exposes %v, want %v", names(got), want)
		}

		// And without a signature, since the type it returns is named after the
		// declared type and a rendering of a name nothing here knows is a
		// string a reader would take for source.
		seq, _ := got.Method("Seq")
		if seq.Signature != "" {
			t.Errorf("Seq is spelled %q with no declaration to spell it from", seq.Signature)
		}
	}
}

// A declaration whose surface cannot be worked out is described as far as it
// can be, rather than reported here.
//
// Reading the declaration can fail — an option naming a field the subject does
// not have, two methods wanting one name — and generation says so, with the
// position and the caret. A shape saying it again would make one mistake read
// as two, and a caller asking what a layer exposes has not asked to be told.
func TestASurfaceOverADeclarationThatCannotBeGenerated(t *testing.T) {
	// A name this layer generates that the stack beneath it already has. The
	// two cannot both be reached, generation says so with the position and the
	// caret, and nothing about that is a shape's to report.
	ctx := declaration(t)
	below := plugin.Shape{Caps: plugin.Caps(plugin.Streamable)}.
		WithMethods(plugin.Method{Name: "Names", Signature: "() []string"})

	got := collection.New().Shape(ctx, below)
	if !slices.Contains(names(got), "Seq") {
		t.Errorf("a shape over a name that collides exposes %v, want the methods it could work out", names(got))
	}

	if _, err := collection.New().Generate(ctx, below); err == nil {
		t.Error("generating the same declaration was not refused, so the shape had nothing to stay quiet about")
	}
}

// exposed is what this layer alone reports for a declaration.
func exposed(ctx *plugin.Context) plugin.Shape {
	return collection.New().Shape(ctx, plugin.Shape{Caps: plugin.Caps(plugin.Streamable)})
}

// overStorage is what the whole stack reports: the storage beneath, and this
// layer over what it exposes.
func overStorage(ctx *plugin.Context) plugin.Shape {
	storage := slice.New().Shape(ctx, shape.Subject(ctx.Model.Subject))
	return collection.New().Shape(ctx, storage)
}

// names returns the surface's method names in the order the layer reports them,
// which is the order they are emitted in.
func names(exposed plugin.Shape) []string { return exposed.Names() }

// published returns the exported methods the source declares on a type, sorted.
//
// Exported, because the unexported ones are the helpers the built methods hand
// their work to. They are not part of what a layer above may call and are not in
// the surface, which is the distinction this draws.
func published(t *testing.T, source []byte, receiver string) []string {
	t.Helper()

	var found []string
	for name := range signatures(t, source, receiver) {
		if ast.IsExported(name) {
			found = append(found, name)
		}
	}

	slices.Sort(found)
	return found
}

// signatures returns every method the source declares on a type, by name, as
// the surface describes one.
func signatures(t *testing.T, source []byte, receiver string) map[string]declared {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "zz_forge_persons.go", source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing the output: %v", err)
	}

	out := make(map[string]declared)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		named, pointer := receiverOf(fn.Recv.List[0].Type)
		if named != receiver {
			continue
		}

		out[fn.Name.Name] = declared{signature: rendered(t, fset, fn.Type), pointer: pointer}
	}
	return out
}

// declared is one method as the source declares it: its signature, and whether
// it took its receiver by pointer — which decides whether a value of the type
// has it at all, and so is half of what a surface promises.
type declared struct {
	signature string
	pointer   bool
}

// receiverOf returns the type a receiver names, and whether it named it through
// a pointer.
func receiverOf(expr ast.Expr) (string, bool) {
	pointer := false
	if star, ok := expr.(*ast.StarExpr); ok {
		expr, pointer = star.X, true
	}
	if name, ok := expr.(*ast.Ident); ok {
		return name.Name, pointer
	}
	return "", pointer
}

// rendered writes a function's parameters and results the way a surface spells
// them: the signature with the leading func and the receiver taken off.
func rendered(t *testing.T, fset *token.FileSet, sig *ast.FuncType) string {
	t.Helper()

	var b strings.Builder
	if err := printer.Fprint(&b, fset, sig); err != nil {
		t.Fatalf("rendering a signature: %v", err)
	}

	// A parsed func type prints as "func() []string"; a surface spells the same
	// method "() []string", since the receiver it hangs off is already known
	// wherever one is read.
	return strings.TrimPrefix(b.String(), "func")
}
