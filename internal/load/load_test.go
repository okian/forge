package load_test

import (
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
