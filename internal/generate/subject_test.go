package generate_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/plugin"
)

// One method written twice across two declarations' files is reported rather
// than written.
//
// The failure a layer outside forge reaches first. A method on the *subject*
// belongs to the package rather than to whichever declaration caused it, so a
// layer that writes one into its own unit — instead of into the section a
// subject shares — has two declarations over one subject produce two files each
// holding it. Each file is internally consistent, neither declaration can see
// the other, and what the author gets is a committed package that does not
// build.
//
// It is forge's report to make. The duplicate is between two generated files
// and the author cannot edit either; a run that wrote them and said nothing
// would send somebody to the compiler to find out what the generator already
// knew.
func TestOneSubjectMethodWrittenTwice(t *testing.T) {
	catalog := layers.Builtins()
	catalog.MustRegister(loose{})

	cfg := config()
	cfg.Catalog.Registry = catalog

	// Two declarations over one subject, which is what makes it possible: one
	// of them alone writes the method once and is right to.
	held := []generate.Request{naming(t, "First"), naming(t, "Second")}

	_, diags := generate.Package(local, "model", held, cfg)
	if diags.Empty() {
		t.Fatal("two declarations wrote one subject method and nothing was said")
	}

	said := diags.Render()
	if !strings.Contains(said, "Person.Loosely") {
		t.Errorf("the report does not name the method written twice:\n%s", said)
	}
	if !strings.Contains(said, "twice") {
		t.Errorf("the report does not say what is wrong:\n%s", said)
	}
}

// One declaration writing it once is not reported, which is what says the check
// above is about the pair rather than about the layer.
func TestOneSubjectMethodWrittenOnce(t *testing.T) {
	catalog := layers.Builtins()
	catalog.MustRegister(loose{})

	cfg := config()
	cfg.Catalog.Registry = catalog

	_, diags := generate.Package(local, "model", []generate.Request{naming(t, "Only")}, cfg)
	if !diags.Empty() {
		t.Errorf("one declaration was refused:\n%s", diags.Render())
	}
}

// naming returns a declaration naming the loose layer over the shared subject.
func naming(t *testing.T, declared string) generate.Request {
	t.Helper()

	one := request(declared)
	one.Model.Form = model.FormSpec
	one.Model.Stack = []model.LayerRef{
		{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Collection"}},
		{Origin: looseRef},
	}

	return one
}

// looseRef is the marker the layer below claims.
var looseRef = model.TypeRef{Pkg: model.MarkerPkg, Name: "Loose"}

// loose is a layer that puts a method on the subject without saying it belongs
// to the subject, which is the mistake under test.
//
// Written here rather than borrowed from the catalog, because every layer forge
// ships does it correctly: what a subject shares goes into the section keyed for
// it, and a layer that had it right would prove nothing about one that does not.
type loose struct{}

// Origin names the marker it claims.
func (loose) Origin() plugin.TypeRef { return looseRef }

// Kind says it is about one value rather than about a container of them.
func (loose) Kind() plugin.Kind { return plugin.KindElement }

// Stage says it is written.
func (loose) Stage() plugin.Stage { return plugin.StageReady }

// Doc is the one line a report puts beside it.
func (loose) Doc() string { return "a method on the subject, written loosely" }

// OptionSchema declares nothing.
func (loose) OptionSchema() []plugin.OptionDef { return nil }

// Accepts takes any stack: what it writes is about the subject.
func (loose) Accepts(plugin.Shape) error { return nil }

// Binds names no package of its own.
func (loose) Binds() []plugin.Import { return nil }

// Writes names the method it puts on the subject, which it does name — the
// mistake is where the method is put, not whether it was declared.
func (loose) Writes() []string { return []string{"Loosely"} }

// Shape adds nothing.
func (loose) Shape(_ *plugin.Context, below plugin.Shape) plugin.Shape { return below }

// Generate writes the method into its own unit, which is where it does not
// belong.
func (loose) Generate(ctx *plugin.Context, _ plugin.Shape) (plugin.Unit, error) {
	src := "package p\n\n// Loosely says nothing.\nfunc (v " +
		ctx.Model.Subject.Ref().Name + ") Loosely() {}\n"

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "loose.go", src,
		parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return plugin.Unit{}, err
	}

	return plugin.Unit{Decls: file.Decls, Comments: file.Comments, Fset: fset}, nil
}
