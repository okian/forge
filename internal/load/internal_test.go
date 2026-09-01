package load

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
)

// parseSource parses a source string the way the loader does, so that build
// constraints and comments are present.
func parseSource(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "spec.go", src, parseMode)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return fset, file
}

// specSource wraps a build constraint around a minimal package.
func specSource(constraintLine string) string {
	if constraintLine == "" {
		return "package model\n\ntype Persons []int\n"
	}
	return constraintLine + "\n\npackage model\n\ntype Persons []int\n"
}

// Which files are spec files decides which declarations forge owns outright, so
// each shape of constraint is worth pinning down.
func TestSpecFile(t *testing.T) {
	cases := map[string]struct {
		constraint string
		want       bool
	}{
		"no constraint":            {"", false},
		"spec tag":                 {"//go:build forgespec", true},
		"spec tag with another":    {"//go:build forgespec && linux", true},
		"generated half":           {"//go:build !forgespec", false},
		"unrelated tag":            {"//go:build linux", false},
		"spec tag in a disjuction": {"//go:build forgespec || linux", false},
		"malformed":                {"//go:build forgespec &&", false},
		"plus build":               {"// +build forgespec", true},
		"plus build negated":       {"// +build !forgespec", false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fset, file := parseSource(t, specSource(tc.constraint))
			if got := SpecFile(fset, file); got != tc.want {
				t.Errorf("SpecFile() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Two +build lines are separate conditions and all of them have to hold, so
// they combine rather than the last one winning.
func TestSpecFileCombinesPlusBuildLines(t *testing.T) {
	fset, file := parseSource(t, "// +build forgespec\n// +build linux\n\npackage model\n\ntype Persons []int\n")
	if !SpecFile(fset, file) {
		t.Error("SpecFile() = false, want true for a file that needs the tag and a platform")
	}
}

// A //go:build line wins outright when a file carries both forms, matching the
// go command.
func TestSpecFilePrefersGoBuild(t *testing.T) {
	fset, file := parseSource(t, "//go:build !forgespec\n// +build forgespec\n\npackage model\n\ntype Persons []int\n")
	if SpecFile(fset, file) {
		t.Error("SpecFile() = true; the //go:build line excludes the tag and should win")
	}
}

// An ordinary comment before the package clause — a licence header, most
// often — is not a constraint and must not be mistaken for the absence of one.
func TestSpecFileSkipsOrdinaryHeaderComments(t *testing.T) {
	fset, file := parseSource(t, "// Copyright the authors.\n// Use of this source is governed by a licence.\n\n//go:build forgespec\n\npackage model\n\ntype Persons []int\n")
	if !SpecFile(fset, file) {
		t.Error("SpecFile() = false; a header comment hid the constraint below it")
	}

	plainFset, plain := parseSource(t, "// Copyright the authors.\n\npackage model\n\ntype Persons []int\n")
	if SpecFile(plainFset, plain) {
		t.Error("SpecFile() = true for a file whose only comment is a header")
	}
}

// A constraint has to appear before the package clause to be one at all.
func TestSpecFileIgnoresConstraintsAfterThePackageClause(t *testing.T) {
	fset, file := parseSource(t, "package model\n\n//go:build forgespec\n\ntype Persons []int\n")
	if SpecFile(fset, file) {
		t.Error("SpecFile() = true for a constraint below the package clause")
	}
}

func TestStripBodiesLeavesEverythingElse(t *testing.T) {
	_, file := parseSource(t, `package app

import "fmt"

type Person struct{ Name string }

func (p Person) Greet() string { return fmt.Sprint(p.Name) }

func Free[T any](v T) (T, error) { return v, nil }

func init() { _ = fmt.Sprint("x") }

var Literal = func() int { return 1 }
`)
	stripBodies(file)

	// An ordinary function loses its body outright, which is legal Go and is
	// what keeps a call to a method that does not exist yet from failing.
	// A generic function and an init function may not be written that way, so
	// they keep a body that does nothing.
	want := map[string]bool{"Greet": false, "Free": true, "init": true}

	var seen int
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		seen++

		keeps, known := want[fn.Name.Name]
		if !known {
			t.Errorf("unexpected function %s", fn.Name.Name)
			continue
		}

		switch {
		case keeps && fn.Body == nil:
			t.Errorf("%s has no body, which the type-checker rejects for it", fn.Name.Name)
		case keeps && len(fn.Body.List) != 1:
			t.Errorf("%s has a body of %d statements, want one panic", fn.Name.Name, len(fn.Body.List))
		case !keeps && fn.Body != nil:
			t.Errorf("%s kept its body", fn.Name.Name)
		}

		if fn.Type.Params == nil {
			t.Errorf("%s lost its signature", fn.Name.Name)
		}
		if !fn.Name.Pos().IsValid() {
			t.Errorf("%s lost its position", fn.Name.Name)
		}
	}

	if seen != len(want) {
		t.Fatalf("found %d function declarations, want %d", seen, len(want))
	}

	// The receiver and the type parameter survive, because a later stage reads
	// both from the signature.
	greet := file.Decls[2].(*ast.FuncDecl)
	if greet.Recv == nil || len(greet.Recv.List) != 1 {
		t.Error("Greet lost its receiver")
	}
	free := file.Decls[3].(*ast.FuncDecl)
	if free.Type.TypeParams == nil || len(free.Type.TypeParams.List) != 1 {
		t.Error("Free lost its type parameter")
	}

	// A function literal is part of an expression the type-checker still has to
	// evaluate, so it keeps its body. Emptying it would be a missing return.
	var literals int
	ast.Inspect(file, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			literals++
			if lit.Body == nil || len(lit.Body.List) == 0 {
				t.Error("a function literal lost its body")
			}
		}
		return true
	})
	if literals != 1 {
		t.Errorf("found %d function literals, want 1", literals)
	}
}

// A method on a generic type keeps its type parameters on the receiver, so it
// is an ordinary declaration and does not need the synthetic body.
func TestStripBodiesLeavesMethodsOnGenericTypesAlone(t *testing.T) {
	_, file := parseSource(t, "package app\n\ntype Box[T any] []T\n\nfunc (b Box[T]) Len() int { return len(b) }\n")
	stripBodies(file)

	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			t.Errorf("%s kept a body it does not need", fn.Name.Name)
		}
	}
}

// A compile-time assertion about a generated method cannot hold before
// generation has run, and it sits outside any body where stripping would reach
// it.
func TestStripAssertions(t *testing.T) {
	_, file := parseSource(t, `package app

import "io"

type Persons []int

var _ io.WriterTo = (*Persons)(nil)

var _ func(*Persons) int = (*Persons).Len

var _, _ io.Writer = nil, nil

var Kept = 3

var _ = mustStay()

var _, Named io.Writer = nil, nil
`)
	stripAssertions(file)

	kept := map[string]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			kept[value.Names[0].Name+":"+strconv.Itoa(len(value.Values))] = true
		}
	}

	// Assertions lose their values; anything with a usable name, or with no
	// type to fall back on, is left exactly as written.
	for _, want := range []string{"_:0", "Kept:1", "_:1", "_:2"} {
		if !kept[want] {
			t.Errorf("declaration %s missing; got %v", want, kept)
		}
	}
}

func TestParsePosition(t *testing.T) {
	cases := map[string]token.Position{
		"":                    {},
		"app.go":              {Filename: "app.go"},
		"app.go:12":           {Filename: "app.go", Line: 12},
		"app.go:12:6":         {Filename: "app.go", Line: 12, Column: 6},
		"/a/b/app.go:12:6":    {Filename: "/a/b/app.go", Line: 12, Column: 6},
		"/od:d/app.go:12:6":   {Filename: "/od:d/app.go", Line: 12, Column: 6},
		"app.go:not-a-number": {Filename: "app.go:not-a-number"},
	}

	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			if got := parsePosition(input); got != want {
				t.Errorf("position(%q) = %+v, want %+v", input, got, want)
			}
		})
	}
}

func TestSplitMessage(t *testing.T) {
	cases := map[string]struct {
		msg     string
		message string
		hint    string
	}{
		"single line": {
			"cannot use \"x\" as int value",
			"cannot use \"x\" as int value",
			"",
		},
		"go command suggestion": {
			"no required module provides package p; to add it:\n\tgo get p",
			"no required module provides package p; to add it:",
			"go get p",
		},
		"several continuation lines": {
			"a\n\tb\n\n\tc",
			"a",
			"b; c",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			message, hint := splitMessage(tc.msg)
			if message != tc.message {
				t.Errorf("message = %q, want %q", message, tc.message)
			}
			if hint != tc.hint {
				t.Errorf("hint = %q, want %q", hint, tc.hint)
			}
		})
	}
}

// Three of the type-checker's messages end in "and not used" and only two of
// them are the artefact. The other two are real errors a function literal can
// still produce, because literals keep their bodies — so matching the suffix
// alone would swallow a package that genuinely does not compile.
func TestUnusedImportPattern(t *testing.T) {
	cases := map[string]struct {
		msg  string
		want bool
	}{
		"unused import": {
			`"fmt" imported and not used`,
			true,
		},
		"unused renamed import": {
			`"strings" imported as str and not used`,
			true,
		},
		"unused blank-renamed import": {
			`"embed" imported as _ and not used`,
			true,
		},
		"unused label": {
			`label loop declared and not used`,
			false,
		},
		"unused type switch guard": {
			`v declared and not used`,
			false,
		},
		"unused variable": {
			`declared and not used: v`,
			false,
		},
		"a real type error": {
			`cannot use "x" (untyped string constant) as int value`,
			false,
		},
		"the phrase inside a longer message": {
			`something about "fmt" imported and not used, but more`,
			false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := unusedImport.MatchString(tc.msg); got != tc.want {
				t.Errorf("MatchString(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

// Silence is the worst answer a generator can give, so the case where nothing
// comes back to explain itself still reports something.
func TestReportNoPackages(t *testing.T) {
	var session Session
	session.reportNoPackages([]string{"./one/...", "./two/..."})

	rendered := session.Diagnostics.Render()
	for _, want := range []string{"FRG5002", "./one/...", "./two/...", "hint:"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("diagnostic does not mention %q:\n%s", want, rendered)
		}
	}
}

func TestConfigDefaults(t *testing.T) {
	if got := (Config{}).patterns(); len(got) != 1 || got[0] != defaultPattern {
		t.Errorf("patterns() = %v, want the default", got)
	}
	if got := (Config{Patterns: []string{"./model"}}).patterns(); len(got) != 1 || got[0] != "./model" {
		t.Errorf("patterns() = %v, want what was configured", got)
	}

	// The spec tag is always set, and never at the cost of the caller's own.
	if got, want := (Config{}).buildFlags()[0], "-tags="+SpecTag; got != want {
		t.Errorf("buildFlags() = %q, want %q", got, want)
	}
	if got, want := (Config{Tags: []string{"integration"}}).buildFlags()[0], "-tags="+SpecTag+",integration"; got != want {
		t.Errorf("buildFlags() = %q, want %q", got, want)
	}
}

// The legacy form only counts when a blank line separates it from the
// documentation below it. A file that gets that wrong is built in every
// configuration, so treating it as a spec file would have forge generate a
// declaration the compiler still has, and the two would collide.
func TestSpecFileRequiresABlankLineAfterAPlusBuildLine(t *testing.T) {
	cases := map[string]struct {
		src  string
		want bool
	}{
		"blank line after": {
			"// +build forgespec\n\npackage model\n\ntype Persons []int\n",
			true,
		},
		"no blank line after": {
			"// +build forgespec\npackage model\n\ntype Persons []int\n",
			false,
		},
		"blank line, then a doc comment": {
			"// +build forgespec\n\n// Package model holds declarations.\npackage model\n\ntype Persons []int\n",
			true,
		},
		"doc comment on the very next line": {
			"// +build forgespec\n// Package model holds declarations.\npackage model\n\ntype Persons []int\n",
			false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fset, file := parseSource(t, tc.src)
			if got := SpecFile(fset, file); got != tc.want {
				t.Errorf("SpecFile() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The modern form carries no such rule, so a //go:build line works wherever it
// is legal.
func TestSpecFileDoesNotRequireABlankLineAfterGoBuild(t *testing.T) {
	fset, file := parseSource(t, "//go:build forgespec\npackage model\n\ntype Persons []int\n")
	if !SpecFile(fset, file) {
		t.Error("SpecFile() = false for a //go:build line with no blank line after it")
	}
}

// The go command reports one bad file as a list error, a parse error and again
// as whatever type error follows from it. An author who made one mistake should
// be told about one mistake.
func TestSessionReportsEachDiagnosticOnce(t *testing.T) {
	session := &Session{dir: "/src"}

	first := diag.New(codeBuildError, token.Position{Filename: "/src/a.go", Line: 1, Column: 1}, "broken")
	session.add(first)
	session.add(first)
	session.add(diag.New(codeBuildError, token.Position{Filename: "/src/a.go", Line: 2, Column: 1}, "broken"))

	if got := session.Diagnostics.Len(); got != 2 {
		t.Errorf("recorded %d diagnostics, want 2:\n%s", got, session.Diagnostics.Render())
	}
}

// The go command reports the same file relative in one error and absolute in
// the next. Diagnostics sort by file name, so leaving both forms in would
// scatter one file's problems across the report.
func TestSessionPositionsAreResolvedAgainstTheLoadDirectory(t *testing.T) {
	session := &Session{dir: "/src/module"}

	cases := map[string]string{
		"app/bad.go:3:5":           "/src/module/app/bad.go",
		"/src/module/app/bad.go:3": "/src/module/app/bad.go",
		"":                         "",
		"./app/bad.go":             "/src/module/app/bad.go",
	}

	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			if got := session.position(input).Filename; got != want {
				t.Errorf("position(%q).Filename = %q, want %q", input, got, want)
			}
		})
	}
}

// A package that is not there asks for nothing.
//
// Reachable rather than defensive: a qualifier can name a type whose package
// the erroring file does not import, and a file too broken to parse is not in
// the package's syntax at all — so the lookup comes back with nothing and the
// answer has to be a false rather than a panic in the middle of reporting why
// somebody's build failed.
func TestNothingAsksForCode(t *testing.T) {
	if asksForCode(nil) {
		t.Error("a package that is not there was read as asking forge to write for it")
	}
}

// What the two patterns match, and what they take the qualifier to be.
//
// Against the checker's messages rather than against a package, because these
// are properties of the patterns: two of the details in them are load-bearing
// and neither is visible in what a fixture produces. Dropping the star before
// the qualifier still resolves an unqualified receiver correctly and quietly
// stops resolving a pointer to one from another package — and generated methods
// on a guarded type take pointer receivers, so that shape is ordinary. Stopping
// the member pattern at the first comma is what keeps the checker's own
// near-misses out, and a fixture cannot hold one, since producing "but does
// have" means writing the name that exists.
func TestWhatAMissingNameIsTakenToBe(t *testing.T) {
	cases := map[string]struct {
		msg       string
		matches   bool
		qualifier string
	}{
		"a bare name":                {msg: "undefined: PersonsSeq", matches: true},
		"a name in another package":  {msg: "undefined: data.PersonsSeq", matches: true, qualifier: "data"},
		"a method on a local type":   {msg: "Persons(nil).Len undefined (type Persons has no field or method Len)", matches: true},
		"a method on a foreign type": {msg: "p.Len undefined (type data.Persons has no field or method Len)", matches: true, qualifier: "data"},
		"a pointer receiver":         {msg: "g.Len undefined (type *data.Guarded has no field or method Len)", matches: true, qualifier: "data"},
		"a generic receiver":         {msg: "b.Get undefined (type Box[string, int] has no field or method Get)", matches: true},

		// The checker found something close, so the name is a misspelling of
		// one that exists rather than one nothing has written.
		"a near miss on a name":   {msg: "undefined: math.SQrt (but have Sqrt)"},
		"a near miss on a member": {msg: "v.FoO undefined (type Person has no field or method FoO, but does have field Foo)"},

		// Neither is a name generating supplies.
		"a field nobody declared": {msg: "unknown field Namex in struct literal of type Person"},
		"an unmet interface":      {msg: "Persons does not implement sort.Interface (missing method Less)"},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			var held []string
			for _, one := range []*regexp.Regexp{undefinedName, missingMember} {
				if held = one.FindStringSubmatch(want.msg); held != nil {
					break
				}
			}

			if (held != nil) != want.matches {
				t.Fatalf("matched = %v, want %v", held != nil, want.matches)
			}
			if !want.matches {
				return
			}
			if held[1] != want.qualifier {
				t.Errorf("qualifier is %q, want %q", held[1], want.qualifier)
			}
		})
	}
}
