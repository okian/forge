package generate_test

import (
	"bytes"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
	subjects "github.com/okian/forge/internal/subject"
)

// sharedPkg is the fixture package the two subjects are declared in.
const sharedPkg = "sharedfixture/model"

// Whatever a layer writes about a type is written once for the package, however
// many declarations reach it.
//
// It is the claim the shared file exists for and the one nothing else checks: a
// package holding one function twice does not compile, so a layer that
// contributed the same helper from two declarations would produce output that
// fails to build — and it would fail only for a package that happened to have
// two declarations, which is not the package anybody writes a test for.
//
// Two ways of reaching it, because they are different mistakes. Two
// declarations over one subject each ask their element layers for the same
// thing about the same type. Two declarations over *different* subjects that
// each hold an Address ask about a type neither of them is.
func TestWhatIsSharedIsWrittenOnce(t *testing.T) {
	files, diags := generate.Package(sharedPkg, "model", []generate.Request{
		declaring(t, "People", "Person"),
		declaring(t, "Everybody", "Person"),
		declaring(t, "Employers", "Employer"),
	}, config())

	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	shared := string(written(t, files, generate.Name()))

	// One of each, for the type two subjects reach and for the type two
	// declarations share.
	once := map[string]string{
		"the codec for the shared struct":   "func (v Address) AppendJSON(",
		"the reader for the shared struct":  "func (v *Address) UnmarshalJSON(",
		"the check for the shared struct":   "func (v Address) Validate() error {",
		"the copy for the shared struct":    "func (v Address) Clone() Address {",
		"the codec for the shared subject":  "func (v Person) AppendJSON(",
		"the copy for the shared subject":   "func (v Person) Clone() Person {",
		"the codec for the other subject":   "func (v Employer) AppendJSON(",
		"the call-through for the subject":  "func appendModelPersonJSON(",
		"the call-through for the reached":  "func appendModelAddressJSON(",
		"the copy through for the subject":  "func clonePerson(v Person) Person {",
		"the check through for the subject": "func validatePerson(v Person) error {",
	}

	for what, held := range once {
		if got := strings.Count(shared, held); got != 1 {
			t.Errorf("%s appears %d times, want once:\n%q", what, got, held)
		}
	}
}

// And the package the two declarations make compiles, which is the claim every
// count above is standing in for.
func TestAPackageOfSeveralDeclarationsCompiles(t *testing.T) {
	// The redacting declarations are among them because the tests that assert
	// what they write assert it about a unit rather than about a package — and
	// a method written twice about one subject passes at that boundary and
	// fails as a package, which is the whole of what went wrong when the layer
	// and the tag both wrote a log value.
	files, diags := generate.Package(sharedPkg, "model", []generate.Request{
		declaring(t, "People", "Person"),
		declaring(t, "Everybody", "Person"),
		declaring(t, "Employers", "Employer"),
		redacting(t, "Holders", "Holder"),
		declaring(t, "Accounts", "Account"),
		redacting(t, "Writtens", "Written"),
	}, config())

	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	spec := goldentest.Source{Name: "spec.go", Content: []byte(
		"//go:build forgespec\n\npackage model\n\n" +
			"type People struct{}\ntype Everybody struct{}\ntype Employers struct{}\n" +
			"type Holders struct{}\ntype Accounts struct{}\ntype Writtens struct{}\n")}

	sources := []goldentest.Source{{Name: "model.go", Content: sharedSource(t)}, spec}
	for _, file := range files {
		sources = append(sources, goldentest.Source{Name: file.Name, Content: file.Content, Generated: true})
	}

	for _, tags := range [][]string{nil, {"forgespec"}} {
		if err := goldentest.Compiles(goldentest.Package{Path: "model", Tags: tags, Files: sources}); err != nil {
			t.Errorf("the package does not compile with tags %v: %v", tags, err)
		}
	}
}

// A layer that writes a method a tag would also earn is the one that writes it.
//
// Both answer the same question about the same type. A redact tag earns a log
// value on its own, without any declaration naming a layer, and the redaction
// layer writes one because it was asked to — so a package that ran both would
// hold one method twice and not compile.
//
// The layer's is the one that stays, and not by accident of ordering: it was
// asked for by name, and it is the fuller answer, since it walks everything the
// subject reaches where what a tag earns is written about the subject alone.
func TestALayerSupersedesWhatATagWouldEarn(t *testing.T) {
	files, diags := generate.Package(sharedPkg, "model",
		[]generate.Request{redacting(t, "Accounts", "Account")}, config())

	if !diags.Empty() {
		t.Fatalf("generating both was refused:\n%s", diags.Render())
	}

	held := shared(t, files)
	if got := strings.Count(held, "func (v Account) LogValue()"); got != 1 {
		t.Fatalf("the log value is written %d times, want once:\n%s", got, held)
	}

	// The layer's rather than the tag's, told apart by the one thing only the
	// layer does: it writes for what the subject reaches as well.
	if !strings.Contains(held, `slog.String("Token", "[redacted]")`) {
		t.Errorf("the log value does not mask the secret:\n%s", held)
	}
}

// And the tag alone still earns one, which is what says the layer superseded
// something rather than replacing nothing.
func TestATagStillEarnsOneWithoutTheLayer(t *testing.T) {
	files, diags := generate.Package(sharedPkg, "model",
		[]generate.Request{declaring(t, "Accounts", "Account")}, config())

	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	if held := shared(t, files); !strings.Contains(held, "func (v Account) LogValue()") {
		t.Errorf("a redact tag earned no log value of its own:\n%s", held)
	}
}

// It supersedes for the package rather than for the declaration that asked.
//
// One declaration redacting and its neighbour not is an ordinary arrangement,
// and what a layer wrote is a method on a subject rather than on whoever asked
// — so a neighbour earning one from the same subject's tag would put the method
// into the package twice. Asked per declaration, each would answer correctly
// about itself and the package would not compile.
//
// Through a reached type as well as through the subject. What the layer wrote
// for Account here was written because Holder reaches it, so a check that only
// counted methods on a declaration's own subject would not see it at all.
func TestALayerSupersedesAcrossDeclarations(t *testing.T) {
	files, diags := generate.Package(sharedPkg, "model", []generate.Request{
		redacting(t, "Holders", "Holder"),
		declaring(t, "Accounts", "Account"),
	}, config())

	if !diags.Empty() {
		t.Fatalf("a package with one declaration redacting was refused:\n%s", diags.Render())
	}

	src := shared(t, files)
	if got := strings.Count(src, "func (v Account) LogValue()"); got != 1 {
		t.Errorf("the log value is written %d times, want once:\n%s", got, src)
	}
}

// And a method the author wrote is not written a second time by either of them.
//
// The layer stands down for it, which is the override every closure layer
// offers. What a tag earns has to stand down for it too, and did not: the check
// that keeps a method off a subject looked for a field of that name and never
// for a method, so the duplicate moved rather than went away.
func TestNeitherWritesOverAMethodTheAuthorWrote(t *testing.T) {
	files, diags := generate.Package(sharedPkg, "model",
		[]generate.Request{redacting(t, "Writtens", "Written")}, config())

	if !diags.Empty() {
		t.Fatalf("a subject whose author wrote the method was refused:\n%s", diags.Render())
	}

	if src := shared(t, files); strings.Contains(src, "func (v Written) LogValue()") {
		t.Errorf("a log value was written beside the author's:\n%s", src)
	}
}

// redacting builds one spec declaration with the redaction layer over it.
func redacting(t *testing.T, declared, of string) generate.Request {
	t.Helper()

	held := declaring(t, declared, of)
	held.Model.Stack = append(held.Model.Stack,
		model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Redact"}, Kind: model.KindElement})

	return held
}

// shared returns the content of the file a package's subjects share.
func shared(t *testing.T, files []generate.File) string {
	t.Helper()

	for _, file := range files {
		if file.Name == generate.Name() {
			return string(file.Content)
		}
	}

	t.Fatal("no shared file was written")
	return ""
}

// declaring builds one spec declaration over a fixture subject, with every
// element layer this build has.
//
// All of them, because what is being asked is whether the package holds one of
// each helper — and a layer left out is a helper that cannot collide.
func declaring(t *testing.T, declared, of string) generate.Request {
	t.Helper()

	return generate.Request{
		Model: &model.Model{
			Name: declared, Form: model.FormSpec, Subject: sharedSubject(t, of),
			Pkg: sharedPackage(t),
			Pos: declaredAt,
			Stack: []model.LayerRef{
				{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Collection"}, Kind: model.KindRefining},
				{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Slice"}, Kind: model.KindStorage},
				{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Json"}, Kind: model.KindElement},
				{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Validate"}, Kind: model.KindElement},
				{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Clone"}, Kind: model.KindElement},
			},
		},
	}
}

// sharedFixture loads the fixture module once for the whole file.
var sharedFixture *load.Session

// loadShared loads the fixture, keeping the one session: loading is what these
// tests spend their time on, and the session is read and never written.
func loadShared(t *testing.T) *load.Session {
	t.Helper()

	if sharedFixture != nil {
		return sharedFixture
	}

	dir, err := filepath.Abs(filepath.Join("testdata", "shared"))
	if err != nil {
		t.Fatalf("resolving the fixture: %v", err)
	}

	loaded, err := load.Load(load.Config{Dir: dir})
	if err != nil {
		t.Fatalf("loading the fixture: %v", err)
	}
	if !loaded.Diagnostics.Empty() {
		t.Fatalf("the fixture does not load clean:\n%s", loaded.Diagnostics.Render())
	}

	sharedFixture = loaded
	return loaded
}

// sharedPackage returns the fixture package the declarations are generated
// into.
func sharedPackage(t *testing.T) *packages.Package {
	t.Helper()

	pkg, ok := loadShared(t).Package(sharedPkg)
	if !ok {
		t.Fatalf("the fixture has no package %s", sharedPkg)
	}
	return pkg
}

// sharedSubject models one of the fixture's subjects.
func sharedSubject(t *testing.T, name string) *model.Struct {
	t.Helper()

	loaded := loadShared(t)
	obj := sharedPackage(t).Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", sharedPkg, name)
	}

	held, is := types.Unalias(obj.Type()).(*types.Named)
	if !is {
		t.Fatalf("%s is a %T, want a named type", name, obj.Type())
	}

	built, problems := subjects.New(subjects.Config{
		Fset:      loaded.Fset,
		Owned:     loaded.Owned(),
		Docs:      loaded.FieldDocs(),
		Generated: loaded.Generated(),
	}).Build(held, subjects.At(token.Position{Filename: "model.go"}))
	if !problems.Empty() {
		t.Fatalf("modelling %s: %s", name, problems.Render())
	}

	return built
}

// sharedSource returns the fixture's own source, so that what is generated is
// compiled against the types it was generated from.
func sharedSource(t *testing.T) []byte {
	t.Helper()

	held, err := os.ReadFile(filepath.Join("testdata", "shared", "model", "model.go"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	return held
}

// Generating one package twice produces one answer, byte for byte.
//
// The shared file is where this is most at risk and least visible: what goes in
// it is gathered from maps keyed by type, and a map is walked in whatever order
// it feels like. Output that varied would make every run a diff, in files that
// are committed — and a repository whose generated code changes without anybody
// changing anything is one where nobody reads it.
//
// Asked of a package with several declarations and several element layers,
// because one of each would be one key in each map and no order to get wrong.
func TestGeneratingOnePackageTwiceGivesOneAnswer(t *testing.T) {
	asked := func() []generate.Request {
		return []generate.Request{
			declaring(t, "People", "Person"),
			declaring(t, "Everybody", "Person"),
			declaring(t, "Employers", "Employer"),
		}
	}

	first, diags := generate.Package(sharedPkg, "model", asked(), config())
	if !diags.Empty() {
		t.Fatalf("generating was refused:\n%s", diags.Render())
	}

	second, diags := generate.Package(sharedPkg, "model", asked(), config())
	if !diags.Empty() {
		t.Fatalf("generating again was refused:\n%s", diags.Render())
	}

	if len(first) != len(second) {
		t.Fatalf("one run wrote %d files and the next wrote %d", len(first), len(second))
	}

	for i, one := range first {
		if one.Name != second[i].Name {
			t.Fatalf("the runs wrote %s and %s in the same place", one.Name, second[i].Name)
		}
		if !bytes.Equal(one.Content, second[i].Content) {
			t.Errorf("%s differs between two runs over the same declarations", one.Name)
		}
	}
}
