package load_test

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/load"
)

// fixture returns the directory of a fixture module.
func fixture(t *testing.T, name string) string {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("resolve fixture %s: %v", name, err)
	}
	return dir
}

// loadFixture loads a fixture module and fails the test if the load could not
// be attempted. Diagnostics are left for the caller to assert on.
func loadFixture(t *testing.T, name string, patterns ...string) *load.Session {
	t.Helper()

	session, err := load.Load(load.Config{Dir: fixture(t, name), Patterns: patterns})
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return session
}

// find returns the loaded package with the given import path.
func find(t *testing.T, session *load.Session, path string) *packages.Package {
	t.Helper()

	pkg, ok := session.Package(path)
	if !ok {
		paths := make([]string, len(session.Packages))
		for i, p := range session.Packages {
			paths[i] = p.PkgPath
		}
		t.Fatalf("package %s not loaded; got %v", path, paths)
	}
	return pkg
}

// This is the load forge has to survive to be able to bootstrap: a spec file
// declaring a type, a caller using methods that only generation will create,
// and no generated file anywhere.
func TestLoadsAPackageBeforeAnythingIsGenerated(t *testing.T) {
	session := loadFixture(t, "clean")

	if !session.Diagnostics.Empty() {
		t.Fatalf("clean fixture reported diagnostics:\n%s", session.Diagnostics.Render())
	}

	pkg := find(t, session, "cleanfixture/model")

	// The spec file is in scope, so the declaration it carries exists even
	// though no generated file does.
	obj := pkg.Types.Scope().Lookup("Persons")
	if obj == nil {
		t.Fatalf("Persons not declared; scope holds %v", pkg.Types.Scope().Names())
	}

	// The subject is fully type-checked, which is what every later stage reads.
	person, ok := pkg.Types.Scope().Lookup("Person").Type().Underlying().(*types.Struct)
	if !ok {
		t.Fatal("Person is not a struct")
	}
	if person.NumFields() != 2 {
		t.Errorf("Person has %d fields, want 2", person.NumFields())
	}
	if got, want := person.Tag(0), `json:"name"`; got != want {
		t.Errorf("first field tag = %q, want %q", got, want)
	}

	// The caller's signature survives; only its body is gone.
	record, ok := pkg.Types.Scope().Lookup("Record").Type().(*types.Signature)
	if !ok {
		t.Fatal("Record is not a function")
	}
	if record.Params().Len() != 2 || record.Results().Len() != 1 {
		t.Errorf("Record has signature %s, want two parameters and one result", record)
	}
}

// Without the spec tag the declaration is not in scope at all, which is the
// difference the session's build flags make.
func TestSpecDeclarationsNeedTheSpecTag(t *testing.T) {
	session := loadFixture(t, "clean")
	pkg := find(t, session, "cleanfixture/model")

	if pkg.Types.Scope().Lookup("Persons") == nil {
		t.Fatal("Persons not declared with the spec tag set")
	}

	// Load the same package the way the compiler would by default.
	plain, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
		Dir:  fixture(t, "clean"),
	}, "./model")
	if err != nil {
		t.Fatalf("plain load: %v", err)
	}
	if len(plain) != 1 {
		t.Fatalf("plain load returned %d packages, want 1", len(plain))
	}
	if plain[0].Types != nil && plain[0].Types.Scope().Lookup("Persons") != nil {
		t.Error("Persons is in scope without the spec tag; the spec file is not guarded")
	}
}

// A package that genuinely does not build is the author's problem and has to be
// reported, with the compiler's own message and position.
func TestReportsAGenuineBuildError(t *testing.T) {
	session := loadFixture(t, "broken")

	if session.Diagnostics.Empty() {
		t.Fatal("broken fixture reported nothing")
	}

	rendered := session.Diagnostics.Render()
	if !strings.Contains(rendered, "FRG5001") {
		t.Errorf("diagnostic is not a build error:\n%s", rendered)
	}
	if !strings.Contains(rendered, "app.go") {
		t.Errorf("diagnostic does not point at the file:\n%s", rendered)
	}

	all := session.Diagnostics.All()
	if got := all[0].Pos.Line; got != 5 {
		t.Errorf("diagnostic points at line %d, want the line the error is on", got)
	}
	if all[0].Hint == "" {
		t.Error("build error carries no hint")
	}
}

// Stripping the bodies removes the only use those imports had. The type-checker
// reports them; forge does not, because the report would be about how forge
// reads the package rather than about the package.
func TestDropsUnusedImportsCausedByStripping(t *testing.T) {
	session := loadFixture(t, "bodies")

	if !session.Diagnostics.Empty() {
		t.Fatalf("reported diagnostics that stripping caused:\n%s", session.Diagnostics.Render())
	}

	pkg := find(t, session, "bodiesfixture/app")

	// The type-checker did report them; they were dropped on the way out.
	var unused int
	for _, err := range pkg.Errors {
		if strings.HasSuffix(err.Msg, "and not used") {
			unused++
		}
	}
	if unused != 2 {
		t.Errorf("fixture produced %d unused-import errors, want 2; it no longer exercises the case", unused)
	}

	// A function with results keeps its signature and does not become a missing
	// return, which an emptied body rather than an absent one would.
	describe, ok := pkg.Types.Scope().Lookup("Describe").Type().(*types.Signature)
	if !ok {
		t.Fatal("Describe is not a function")
	}
	if describe.Results().Len() != 2 {
		t.Errorf("Describe returns %d values, want 2", describe.Results().Len())
	}
}

// Walking the packages has to give the same answer every run, or everything
// built by walking them inherits the variation.
func TestPackagesAreOrdered(t *testing.T) {
	session := loadFixture(t, "clean")

	paths := make([]string, len(session.Packages))
	for i, pkg := range session.Packages {
		paths[i] = pkg.PkgPath
	}

	want := []string{"cleanfixture/markers", "cleanfixture/model"}
	if len(paths) != len(want) {
		t.Fatalf("loaded %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("Packages[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}

func TestLoadPatternsAndLookup(t *testing.T) {
	session := loadFixture(t, "clean", "./model")

	if len(session.Packages) != 1 {
		t.Fatalf("loaded %d packages for ./model, want 1", len(session.Packages))
	}
	if _, ok := session.Package("cleanfixture/markers"); ok {
		t.Error("./model pulled in the markers package as a root")
	}

	pkg := find(t, session, "cleanfixture/model")
	if name := session.FileName(pkg.Syntax[0]); !strings.HasSuffix(name, ".go") {
		t.Errorf("FileName returned %q, want a Go file", name)
	}

	// A nil session and a nil file answer rather than panicking, because a
	// caller holding one has already failed and should not fail again here.
	var missing *load.Session
	if _, ok := missing.Package("anything"); ok {
		t.Error("a nil session reported a package")
	}
	if got := missing.FileName(nil); got != "" {
		t.Errorf("a nil session returned the file name %q", got)
	}
	if got := session.FileName(nil); got != "" {
		t.Errorf("a nil file returned the file name %q", got)
	}
}

// A pattern nothing matches is not a crash and not silence: it is the most
// likely thing to have gone wrong when a run does nothing.
//
// go/packages answers such a pattern with a synthetic package carrying the go
// command's own complaint, which is reported as-is — the tool knows better than
// forge does why a path did not resolve.
func TestReportsWhenNothingMatches(t *testing.T) {
	cases := map[string]struct {
		pattern  string
		mentions string
	}{
		"missing directory tree": {"./nowhere/...", "./nowhere/..."},
		"missing package":        {"nonexistent.example/pkg", "nonexistent.example/pkg"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			session, err := load.Load(load.Config{Dir: fixture(t, "clean"), Patterns: []string{tc.pattern}})
			if err != nil {
				t.Fatalf("load: %v", err)
			}

			rendered := session.Diagnostics.Render()
			if !strings.Contains(rendered, "FRG5002") {
				t.Errorf("diagnostic is not the no-packages one:\n%s", rendered)
			}
			if !strings.Contains(rendered, tc.mentions) {
				t.Errorf("diagnostic does not mention %q:\n%s", tc.mentions, rendered)
			}
			if strings.Count(rendered, "\n  hint: ") != 1 {
				t.Errorf("diagnostic does not carry exactly one hint line:\n%s", rendered)
			}
		})
	}
}

// The go command appends its suggestions as indented continuation lines. A
// diagnostic is one line plus a hint, so the continuation becomes the hint
// rather than breaking the format — and the tool's own advice beats forge's.
func TestGoCommandSuggestionBecomesTheHint(t *testing.T) {
	session, err := load.Load(load.Config{
		Dir:      fixture(t, "clean"),
		Patterns: []string{"nonexistent.example/pkg"},
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	all := session.Diagnostics.All()
	if len(all) != 1 {
		t.Fatalf("reported %d diagnostics, want 1:\n%s", len(all), session.Diagnostics.Render())
	}

	if strings.Contains(all[0].Message, "\n") {
		t.Errorf("message spans lines, which breaks the rendering: %q", all[0].Message)
	}
	if !strings.Contains(all[0].Hint, "go get") {
		t.Errorf("hint = %q, want the go command's own suggestion", all[0].Hint)
	}
}

// A load that cannot be attempted at all is an error, not a diagnostic: there
// is no position to report it at and nothing the author wrote is wrong.
func TestLoadFailsOnAnUnusableConfiguration(t *testing.T) {
	_, err := load.Load(load.Config{
		Dir:      fixture(t, "clean"),
		Patterns: []string{"./..."},
		Env:      []string{"GO111MODULE=off", "GOPATH=", "GOFLAGS=-mod=bogus"},
	})
	if err == nil {
		t.Fatal("load succeeded with an unusable environment")
	}
}

// This is the shape of an ordinary Go package, and every part of it is one
// that a naive stripping would break: an init function and a generic function
// may not be written without a body, and a compile-time assertion about a
// generated method cannot hold before generation has run.
func TestLoadsAnOrdinaryPackage(t *testing.T) {
	session := loadFixture(t, "realistic")

	if !session.Diagnostics.Empty() {
		t.Fatalf("an ordinary package did not load clean:\n%s", session.Diagnostics.Render())
	}

	pkg := find(t, session, "realisticfixture/app")
	for _, name := range []string{"Sum", "Persons", "Describe"} {
		if pkg.Types.Scope().Lookup(name) == nil {
			t.Errorf("%s is missing; scope holds %v", name, pkg.Types.Scope().Names())
		}
	}

	// The generic function keeps its signature, which is what a later stage
	// would read.
	sum, ok := pkg.Types.Scope().Lookup("Sum").Type().(*types.Signature)
	if !ok {
		t.Fatal("Sum is not a function")
	}
	if sum.TypeParams().Len() != 1 {
		t.Errorf("Sum has %d type parameters, want 1", sum.TypeParams().Len())
	}
}

// A function literal keeps its body, so an error inside one is a real error
// that stripping did not cause and must not be filtered away with the unused
// imports.
func TestReportsErrorsInsideFunctionLiterals(t *testing.T) {
	session := loadFixture(t, "literals")

	if session.Diagnostics.Empty() {
		t.Fatal("an unused type-switch guard inside a literal was not reported")
	}
	if got := session.Diagnostics.Render(); !strings.Contains(got, "declared and not used") {
		t.Errorf("diagnostic does not name the error:\n%s", got)
	}
}

// A module with nothing to build is not a crash and not silence.
func TestReportsAModuleWithNoPackages(t *testing.T) {
	session := loadFixture(t, "nopackages")

	if session.Diagnostics.Empty() {
		t.Fatal("a module with no buildable package reported nothing")
	}
	if got := session.Diagnostics.Render(); !strings.Contains(got, "FRG5002") {
		t.Errorf("diagnostic is not the no-packages one:\n%s", got)
	}
}

// A directory that exists but puts no Go files in scope is the same answer as a
// pattern that matched nothing: there is nothing here to generate for. What it
// must not do is advise the author to run from inside the module, which is
// where they already are.
func TestReportsDirectoriesWithNoGoFilesInScope(t *testing.T) {
	cases := map[string]string{
		"all files excluded by a tag": "./tagged",
		"no Go files at all":          "./asm",
	}

	for name, pattern := range cases {
		t.Run(name, func(t *testing.T) {
			session := loadFixture(t, "excluded", pattern)

			rendered := session.Diagnostics.Render()
			if !strings.Contains(rendered, "FRG5002") {
				t.Errorf("diagnostic is not the no-Go-files one:\n%s", rendered)
			}
			if strings.Contains(rendered, "run forge from inside the module") {
				t.Errorf("diagnostic gives advice the author has already followed:\n%s", rendered)
			}
		})
	}
}

// go/packages returns roots in the order the patterns named them, so two
// patterns written out of order are what the sort is for.
func TestPackagesAreSortedRegardlessOfPatternOrder(t *testing.T) {
	session := loadFixture(t, "order", "./zz", "./aa")

	paths := make([]string, len(session.Packages))
	for i, pkg := range session.Packages {
		paths[i] = pkg.PkgPath
	}

	want := []string{"orderfixture/aa", "orderfixture/zz"}
	if len(paths) != len(want) {
		t.Fatalf("loaded %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("Packages[%d] = %q, want %q — patterns were given in the other order", i, paths[i], want[i])
		}
	}
}

// Discovery decides how to treat a declaration from whether its file is a spec
// file, and it asks through a real load rather than a hand-parsed string.
func TestSpecFileThroughARealLoad(t *testing.T) {
	session := loadFixture(t, "clean")
	pkg := find(t, session, "cleanfixture/model")

	found := map[string]bool{}
	for _, file := range pkg.Syntax {
		name := filepath.Base(session.FileName(file))
		found[name] = load.SpecFile(session.Fset, file)
	}

	if !found["spec.go"] {
		t.Error("spec.go is not recognised as a spec file")
	}
	for _, ordinary := range []string{"person.go", "caller.go"} {
		if found[ordinary] {
			t.Errorf("%s is recognised as a spec file", ordinary)
		}
	}
}

// A missing name in a package forge writes for is said to be one forge has not
// written yet.
//
// The one build failure forge itself causes, and the one an author cannot
// diagnose from the message. They write the declaration and the call sites
// together; the call sites name a type and a method only the generated file
// declares; the load type-checks before generation runs, so the first run
// refuses. What the compiler says is that a name is undefined, which the author
// can already see. What it cannot say is that running the tool is what defines
// it — and the recovery, commenting the reference out for one run, is trivial
// and unguessable.
//
// Both shapes, because the two ways of reaching a name forge writes produce
// different errors: naming the view type in a signature is an undefined
// identifier, and calling a generated method is a selector on a type that has
// no such member.
func TestAMissingNameInAPackageForgeWritesFor(t *testing.T) {
	session := loadFixture(t, "ungenerated", "./asked")

	// Counted, so that a run reporting nothing at all fails here rather than
	// passing an assertion made about every diagnostic there was.
	all := session.Diagnostics.All()
	if len(all) != 3 {
		t.Fatalf("reported %d diagnostics, want the two missing names and the misspelling:\n%s",
			len(all), session.Diagnostics.Render())
	}

	for _, held := range all {
		// The misspelling in the same file is the exception and has a test of
		// its own; everything else here is a name generating would supply.
		if strings.Contains(held.Message, "ToUppr") {
			continue
		}
		if !strings.Contains(held.Hint, "nothing has written it yet") {
			t.Errorf("%q was not explained as a name forge has not written:\n  hint: %s",
				held.Message, held.Hint)
		}
	}
}

// A name misspelt in another package is not explained as one forge would have
// written.
//
// It arrives in the same shape as the names above it — an undefined identifier,
// in a package holding a directive — and the only thing that tells them apart
// is that this one names a package forge writes nothing for. Without that,
// every typo in a package with a declaration in it is answered with advice to
// comment the line out and run a generator, which cannot work and costs the
// reader the time to find out.
func TestAMisspellingInAnotherPackage(t *testing.T) {
	session := loadFixture(t, "ungenerated", "./asked")

	for _, held := range session.Diagnostics.All() {
		if !strings.Contains(held.Message, "ToUppr") {
			continue
		}
		if strings.Contains(held.Hint, "nothing has written it yet") {
			t.Errorf("a misspelling was blamed on a generator:\n  %s\n  hint: %s",
				held.Message, held.Hint)
		}
		return
	}

	t.Fatal("the fixture no longer produces a misspelling, so nothing here is tested")
}

// And a name the neighbouring package will be given is explained, which is the
// arrangement most likely to produce one.
//
// A repository over a model, with a method handing back the view forge writes.
// The name that cannot be found is in the model rather than in the repository,
// so an answer that only ever asked about the package holding the error would
// be silent in exactly the layout the hint exists for.
func TestAMissingNameTheNeighbourWillBeGiven(t *testing.T) {
	session := loadFixture(t, "ungenerated", "./repo")

	all := session.Diagnostics.All()
	if len(all) == 0 {
		t.Fatal("a package naming an ungenerated type reported nothing")
	}

	for _, held := range all {
		if !strings.Contains(held.Hint, "nothing has written it yet") {
			t.Errorf("%q was not explained as a name the model has not been given:\n  hint: %s",
				held.Message, held.Hint)
		}
	}
}

// And the same missing name in a package that asks forge for nothing is not.
//
// The control, and the whole of what keeps the suggestion honest. The two
// declarations here are the ones next door with the directive taken off them:
// the same names, reached the same two ways, producing the same two errors —
// and only one of the packages is somewhere forge would have written anything. A
// hint that arrived here would be advice to run a generator offered to somebody
// who misspelt a type.
func TestAMissingNameWhereForgeWritesNothing(t *testing.T) {
	session := loadFixture(t, "ungenerated", "./plain")

	all := session.Diagnostics.All()
	if len(all) == 0 {
		t.Fatal("a package that does not build reported nothing")
	}

	for _, held := range all {
		if strings.Contains(held.Hint, "nothing has written it yet") {
			t.Errorf("%q was blamed on a generator that writes nothing here:\n  hint: %s",
				held.Message, held.Hint)
		}
		if held.Hint == "" {
			t.Errorf("%q arrived with no hint at all", held.Message)
		}
	}
}

// A function carrying a forge directive keeps its body, because a stage reads
// it: a hint's statements are the input, and a stripped hint is no input at
// all. Everything else is stripped exactly as before — bodies are bulk the
// pipeline never reads.
func TestADirectiveCarryingFunctionKeepsItsBody(t *testing.T) {
	session := loadFixture(t, "hints", "hintsfixture/app")

	if !session.Diagnostics.Empty() {
		t.Fatalf("hint fixture reported diagnostics:\n%s", session.Diagnostics.Render())
	}
	pkg := find(t, session, "hintsfixture/app")

	held := make(map[string]*ast.FuncDecl)
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				held[fn.Name.Name] = fn
			}
		}
	}

	hint, ok := held["personFromUser"]
	if !ok {
		t.Fatal("the hint was not loaded")
	}
	if hint.Body == nil {
		t.Fatal("the hint's body was stripped; its statements are a stage's input")
	}
	if got := len(hint.Body.List); got != 1 {
		t.Fatalf("the hint's body holds %d statements, want 1", got)
	}

	// And the body was in front of the type-checker: the expression it reads
	// from has a type.
	assign, ok := hint.Body.List[0].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("the hint's statement is a %T, want an assignment", hint.Body.List[0])
	}
	if tv, ok := pkg.TypesInfo.Types[assign.Rhs[0]]; !ok || tv.Type == nil {
		t.Error("the hint's right-hand side carries no type; the body was not type-checked")
	}

	if fn, ok := held["helper"]; !ok {
		t.Fatal("helper was not loaded")
	} else if fn.Body != nil {
		t.Error("helper kept its body; only a directive marks one worth keeping")
	}
}

// A hint that does not type-check is a load diagnostic, not a generator
// mystery: keeping the body puts it in front of the compiler forge already
// runs.
func TestABrokenHintFailsTheLoad(t *testing.T) {
	session := loadFixture(t, "hints", "hintsfixture/broken")

	if session.Diagnostics.Empty() {
		t.Fatal("a hint reading a member the source does not have loaded clean")
	}
	if rendered := session.Diagnostics.Render(); !strings.Contains(rendered, "Missing") {
		t.Errorf("the report does not name the missing member:\n%s", rendered)
	}
}
