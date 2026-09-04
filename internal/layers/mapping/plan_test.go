package mapping

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/subject"
	"github.com/okian/forge/plugin"
)

// The fixture's two packages: the pairs that plan, and the pairs that refuse.
const (
	modelPkg   = "mapfixture/model"
	refusedPkg = "mapfixture/refused"
)

// pair names one constructor to plan: a source and a target from one fixture
// package, generated into a package of the test's choosing.
type pair struct {
	pkg    string
	source string
	target string

	// into is the package the constructor is generated into; the pair's own
	// package when empty.
	into string

	// ignore is the option's value, written as the author would: "B" or "A,B".
	ignore string

	// hint names a fixture function to attach, as the pipeline's matcher
	// would.
	hint string

	// json writes the codec beneath the bridge, which asks for the fused
	// writers.
	json bool
}

// loadFixture loads the fixture module once per call.
func loadFixture(t *testing.T) *load.Session {
	t.Helper()

	dir, err := filepath.Abs(filepath.Join("testdata", "mapping"))
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
	return loaded
}

// lookup returns a named type from a fixture package.
func lookup(t *testing.T, loaded *load.Session, pkgPath, name string) *types.Named {
	t.Helper()

	pkg, ok := loaded.Package(pkgPath)
	if !ok {
		t.Fatalf("the fixture has no package %s", pkgPath)
	}
	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", pkgPath, name)
	}
	held, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok {
		t.Fatalf("%s is a %T, want a named type", name, obj.Type())
	}
	return held
}

// planFor builds the pair's context the way the pipeline would and asks the
// ladder to settle it.
func planFor(t *testing.T, p pair) (*plan, error) {
	t.Helper()
	return planned(contextFor(t, loadFixture(t), p))
}

// contextFor builds the context the pipeline would hand the layer for one
// pair.
func contextFor(t *testing.T, loaded *load.Session, p pair) *plugin.Context {
	t.Helper()

	source := lookup(t, loaded, p.pkg, p.source)
	target := lookup(t, loaded, p.pkg, p.target)

	builder := subject.New(subject.Config{
		Fset:  loaded.Fset,
		Owned: loaded.Owned(),
		Docs:  loaded.FieldDocs(),
	})
	built, problems := builder.Build(target, subject.Site{})
	if !problems.Empty() {
		t.Fatalf("modelling %s: %s", p.target, problems.Render())
	}

	into := p.into
	if into == "" {
		into = p.pkg
	}
	pkg, ok := loaded.Package(into)
	if !ok {
		t.Fatalf("the fixture has no package %s", into)
	}

	options := plugin.Options{Layer: "map"}
	if p.ignore != "" {
		options.Entries = append(options.Entries, model.Option{Key: "ignore", Value: p.ignore})
	}

	var hints []model.Hint
	if p.hint != "" {
		hints = append(hints, hintNamed(t, loaded, p.pkg, p.hint))
	}

	stack := []model.LayerRef{{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Map"}}}
	if p.json {
		stack = append(stack, model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Json"}})
	}

	return &plugin.Context{
		Model: &plugin.Model{
			Name:    p.source + "To" + p.target,
			Form:    plugin.FormSpec,
			Subject: built,
			Source:  source,
			Stack:   stack,
			Pkg:     pkg,
			Pos:     token.Position{Filename: "spec.go", Line: 1, Column: 1},
			Hints:   hints,
		},
		Options: options,
	}
}

// hintNamed builds the model.Hint the pipeline's matcher would hand over, by
// finding the fixture function's declaration.
func hintNamed(t *testing.T, loaded *load.Session, pkgPath, name string) model.Hint {
	t.Helper()

	pkg, ok := loaded.Package(pkgPath)
	if !ok {
		t.Fatalf("the fixture has no package %s", pkgPath)
	}

	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != name {
				continue
			}
			return model.Hint{Fn: fn, Pkg: pkg, Pos: loaded.Fset.Position(fn.Name.Pos())}
		}
	}

	t.Fatalf("%s declares no function %s", pkgPath, name)
	return model.Hint{}
}

// settlement is what a test asserts about one member: how it was settled and
// by which source member.
type settlement struct {
	via      settled
	from     string
	folded   bool
	tagged   bool
	overrode string
}

// settlements reduces a plan to what the tests compare.
func settlements(built *plan) map[string]settlement {
	out := make(map[string]settlement, len(built.members))
	for _, member := range built.members {
		out[member.field.Name] = settlement{
			via: member.via, from: member.from,
			folded: member.folded, tagged: member.tagged, overrode: member.overrode,
		}
	}
	return out
}

// The ladder settles every member of the plain pair, rung by rung: the field
// of the same name first, the method of the same name next, and the unique
// fold last.
func TestTheLadderMatchesByName(t *testing.T) {
	built, err := planFor(t, pair{pkg: modelPkg, source: "User", target: "Person"})
	if err != nil {
		t.Fatalf("the plain pair was refused: %v", err)
	}

	want := map[string]settlement{
		"ID":       {via: settledField, from: "ID"},
		"Email":    {via: settledField, from: "Email"},
		"Age":      {via: settledField, from: "Age"},
		"FullName": {via: settledMethod, from: "FullName"},
		"Nickname": {via: settledMethod, from: "NickName", folded: true},
	}

	got := settlements(built)
	if len(got) != len(want) {
		t.Fatalf("planned %d members, want %d", len(got), len(want))
	}
	for name, held := range want {
		if got[name] != held {
			t.Errorf("%s settled as %+v, want %+v", name, got[name], held)
		}
	}
}

// An interface source offers methods and nothing else, and they settle members
// exactly as a struct's would.
func TestAnInterfaceSourceMatchesByMethod(t *testing.T) {
	built, err := planFor(t, pair{pkg: modelPkg, source: "Reader", target: "Card"})
	if err != nil {
		t.Fatalf("the interface pair was refused: %v", err)
	}

	want := map[string]settlement{
		"ID":    {via: settledMethod, from: "ID"},
		"Label": {via: settledMethod, from: "Label"},
	}

	if got := settlements(built); len(got) != len(want) {
		t.Fatalf("planned %d members, want %d", len(got), len(want))
	} else {
		for name, held := range want {
			if got[name] != held {
				t.Errorf("%s settled as %+v, want %+v", name, got[name], held)
			}
		}
	}
}

// A target's unexported field is a member like any other when the constructor
// is generated into the target's own package, and the ladder settles it.
func TestALocalUnexportedMemberIsSettled(t *testing.T) {
	built, err := planFor(t, pair{pkg: modelPkg, source: "Entitled", target: "Titled"})
	if err != nil {
		t.Fatalf("the local pair was refused: %v", err)
	}

	got := settlements(built)
	if want := (settlement{via: settledField, from: "Title", folded: true}); got["title"] != want {
		t.Errorf("title settled as %+v, want %+v", got["title"], want)
	}
}

// Every way the ladder refuses, with the code that says which and a message
// that names what was wrong.
func TestWhatTheLadderRefuses(t *testing.T) {
	cases := map[string]struct {
		pair  pair
		code  string
		says  string
		hints string
	}{
		"a member settled no way": {
			pair: pair{pkg: refusedPkg, source: "Src", target: "Unmatched"},
			code: "FRG2035", says: "B", hints: "hint",
		},
		"two members claim one": {
			pair: pair{pkg: refusedPkg, source: "Forked", target: "Foobar"},
			code: "FRG2036", says: "Foobar", hints: "hint",
		},
		"a match that does not assign": {
			pair: pair{pkg: refusedPkg, source: "Src", target: "Aged"},
			code: "FRG2037", says: "int", hints: "hint",
		},
		"a source with nothing to read": {
			pair: pair{pkg: refusedPkg, source: "Empty", target: "Aged"},
			code: "FRG2034", says: "Empty", hints: "export",
		},
		"a target out of reach": {
			pair: pair{pkg: refusedPkg, source: "Src", target: "Sealed", into: modelPkg},
			code: "FRG2038", says: "secret", hints: "package",
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := planFor(t, want.pair)
			if err == nil {
				t.Fatalf("a constructor was planned for %s", want.pair.target)
			}

			reported, ok := plugin.From(err)
			if !ok {
				t.Fatalf("%v is not a diagnostic", err)
			}
			if got := reported.Code.String(); got != want.code {
				t.Errorf("reported as %s, want %s: %s", got, want.code, reported.Message)
			}
			if !strings.Contains(reported.Message, want.says) {
				t.Errorf("the complaint does not mention %s:\n%s", want.says, reported.Message)
			}
			if !strings.Contains(reported.Hint, want.hints) {
				t.Errorf("the hint does not say %q:\n%s", want.hints, reported.Hint)
			}
		})
	}
}

// An ignore settles a member nothing else does, on purpose; the same ignore on
// a member the ladder settles is a contradiction to report.
func TestIgnoreSettlesAMemberOnPurpose(t *testing.T) {
	built, err := planFor(t, pair{pkg: refusedPkg, source: "Src", target: "Unmatched", ignore: "B"})
	if err != nil {
		t.Fatalf("the ignored pair was refused: %v", err)
	}
	if got := settlements(built)["B"]; got.via != settledIgnored {
		t.Errorf("B settled as %+v, want ignored", got)
	}

	_, err = planFor(t, pair{pkg: refusedPkg, source: "Src", target: "Unmatched", ignore: "A,B"})
	if err == nil {
		t.Fatal("an ignore of a settled member was accepted")
	}
	reported, ok := plugin.From(err)
	if !ok {
		t.Fatalf("%v is not a diagnostic", err)
	}
	if got := reported.Code.String(); got != "FRG3031" {
		t.Errorf("reported as %s, want FRG3031: %s", got, reported.Message)
	}
	if !strings.Contains(reported.Message, "A") {
		t.Errorf("the complaint does not name the member:\n%s", reported.Message)
	}
}

// A hint settles what the ladder cannot: a renamed member, and one that needs
// a conversion.
func TestAHintSettlesWhatTheLadderCannot(t *testing.T) {
	built, err := planFor(t, pair{pkg: modelPkg, source: "User", target: "Renamed", hint: "renamedFromUser"})
	if err != nil {
		t.Fatalf("the hinted pair was refused: %v", err)
	}

	got := settlements(built)
	if got["Moniker"].via != settledHint {
		t.Errorf("Moniker settled as %+v, want the hint", got["Moniker"])
	}
	if want := (settlement{via: settledField, from: "ID"}); got["ID"] != want {
		t.Errorf("ID settled as %+v, want %+v", got["ID"], want)
	}

	converted, err := planFor(t, pair{pkg: modelPkg, source: "User", target: "Converted", hint: "convertedFromUser"})
	if err != nil {
		t.Fatalf("the converting pair was refused: %v", err)
	}
	if got := settlements(converted); got["Age"].via != settledHint {
		t.Errorf("Age settled as %+v, want the hint", got["Age"])
	}
}

// A hint outranks the ladder: an assignment to a member the ladder would
// settle displaces the match, and the displaced name is recorded for the
// ledger.
func TestAHintOverridesAMatch(t *testing.T) {
	built, err := planFor(t, pair{pkg: modelPkg, source: "User", target: "Person", hint: "personFromUser"})
	if err != nil {
		t.Fatalf("the overridden pair was refused: %v", err)
	}

	got := settlements(built)
	if want := (settlement{via: settledHint, overrode: "Email"}); got["Email"] != want {
		t.Errorf("Email settled as %+v, want %+v", got["Email"], want)
	}
	if want := (settlement{via: settledField, from: "ID"}); got["ID"] != want {
		t.Errorf("ID settled as %+v, want %+v", got["ID"], want)
	}
}

// Everything a hint may not say, each with the code that refuses it.
func TestWhatAHintMayNotSay(t *testing.T) {
	cases := map[string]struct {
		pair pair
		code string
		says string
	}{
		"a local declaration": {
			pair: pair{pkg: modelPkg, source: "User", target: "Renamed", hint: "declares"},
			code: "FRG3032", says: "declares",
		},
		"a branch": {
			pair: pair{pkg: modelPkg, source: "User", target: "Renamed", hint: "branches"},
			code: "FRG3032", says: "branches",
		},
		"an assignment into the source": {
			pair: pair{pkg: modelPkg, source: "User", target: "Renamed", hint: "writesBack"},
			code: "FRG3032", says: "writesBack",
		},
		"a member the target does not declare": {
			pair: pair{pkg: modelPkg, source: "User", target: "Based", hint: "promotes"},
			code: "FRG3032", says: "Core",
		},
		"a member assigned twice": {
			pair: pair{pkg: modelPkg, source: "User", target: "Renamed", hint: "twice"},
			code: "FRG3033", says: "Moniker",
		},
		"an expression reaching through an import": {
			pair: pair{pkg: modelPkg, source: "User", target: "Renamed", hint: "imported"},
			code: "FRG3032", says: "strings",
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := planFor(t, want.pair)
			if err == nil {
				t.Fatal("the hint was read as though it kept the grammar")
			}

			reported, ok := plugin.From(err)
			if !ok {
				t.Fatalf("%v is not a diagnostic", err)
			}
			if got := reported.Code.String(); got != want.code {
				t.Errorf("reported as %s, want %s: %s", got, want.code, reported.Message)
			}
			if !strings.Contains(reported.Message, want.says) {
				t.Errorf("the complaint does not mention %s:\n%s", want.says, reported.Message)
			}
			if reported.Hint == "" {
				t.Error("the refusal carries no hint")
			}
		})
	}
}

// Respelling renames the hint's parameters to the constructor's identifiers
// and nothing else — and never edits the author's own syntax.
func TestRespellingRenamesOnlyTheParameters(t *testing.T) {
	loaded := loadFixture(t)
	held := hintNamed(t, loaded, modelPkg, "renamedFromUser")

	assign, ok := held.Fn.Body.List[0].(*ast.AssignStmt)
	if !ok {
		t.Fatalf("the fixture hint's statement is a %T, want an assignment", held.Fn.Body.List[0])
	}

	got := respelled(assign.Rhs[0], map[string]string{"src": "from", "dst": "out"})
	if want := "from.Email"; got != want {
		t.Errorf("respelled to %q, want %q", got, want)
	}

	// The author's tree is untouched: the same expression still prints its own
	// names.
	if again := types.ExprString(assign.Rhs[0]); again != "src.Email" {
		t.Errorf("the author's expression now prints %q; the tree was edited in place", again)
	}
}

// A from tag pins a member to the source member it names: qualified entries
// pick by source, parens assert a method, and a bare entry applies to
// whichever source maps in.
func TestATagPinsAMember(t *testing.T) {
	account, err := planFor(t, pair{pkg: modelPkg, source: "Account", target: "Rolodex"})
	if err != nil {
		t.Fatalf("the tagged pair was refused: %v", err)
	}
	got := settlements(account)
	if want := (settlement{via: settledField, from: "Contact", tagged: true}); got["Email"] != want {
		t.Errorf("Email settled as %+v, want %+v", got["Email"], want)
	}
	if want := (settlement{via: settledField, from: "ID"}); got["ID"] != want {
		t.Errorf("ID settled as %+v, want %+v", got["ID"], want)
	}

	company, err := planFor(t, pair{pkg: modelPkg, source: "Company", target: "Rolodex"})
	if err != nil {
		t.Fatalf("the tagged pair was refused: %v", err)
	}
	if want := (settlement{via: settledField, from: "EmailAddress", tagged: true}); settlements(company)["Email"] != want {
		t.Errorf("Email settled as %+v, want %+v", settlements(company)["Email"], want)
	}

	badge, err := planFor(t, pair{pkg: modelPkg, source: "User", target: "Badge"})
	if err != nil {
		t.Fatalf("the method-pinned pair was refused: %v", err)
	}
	got = settlements(badge)
	if want := (settlement{via: settledMethod, from: "NickName", tagged: true}); got["Name"] != want {
		t.Errorf("Name settled as %+v, want %+v", got["Name"], want)
	}
	if want := (settlement{via: settledField, from: "Email", tagged: true}); got["Contact"] != want {
		t.Errorf("Contact settled as %+v, want %+v", got["Contact"], want)
	}
}

// Everything a from tag can get wrong, each with the code that refuses it.
func TestWhatAFromTagMayNotSay(t *testing.T) {
	cases := map[string]struct {
		pair  pair
		code  string
		says  string
		hints string
	}{
		"a member the source does not offer": {
			pair: pair{pkg: refusedPkg, source: "Src", target: "Mistagged"},
			code: "FRG3035", says: "Nothing", hints: "from",
		},
		"an entry that is not Source.Member": {
			pair: pair{pkg: refusedPkg, source: "Src", target: "Malformed"},
			code: "FRG3034", says: "a.b.c", hints: "Member",
		},
		"parens on a field": {
			pair: pair{pkg: refusedPkg, source: "Src", target: "AssertedField"},
			code: "FRG3035", says: "A()", hints: "method",
		},
		"two entries answering one mapping": {
			pair: pair{pkg: refusedPkg, source: "Src", target: "TaggedTwice"},
			code: "FRG3034", says: "Src.A", hints: "one entry",
		},
		"a pinned member whose types do not assign": {
			pair: pair{pkg: refusedPkg, source: "Src", target: "AgedTag"},
			code: "FRG2037", says: "int", hints: "hint",
		},
		"a tag and a hint both settling one member": {
			pair: pair{pkg: modelPkg, source: "User", target: "Sticker", hint: "stickerFromUser"},
			code: "FRG3036", says: "Email", hints: "one of them",
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := planFor(t, want.pair)
			if err == nil {
				t.Fatalf("a constructor was planned for %s", want.pair.target)
			}

			reported, ok := plugin.From(err)
			if !ok {
				t.Fatalf("%v is not a diagnostic", err)
			}
			if got := reported.Code.String(); got != want.code {
				t.Errorf("reported as %s, want %s: %s", got, want.code, reported.Message)
			}
			if !strings.Contains(reported.Message, want.says) {
				t.Errorf("the complaint does not mention %s:\n%s", want.says, reported.Message)
			}
			if !strings.Contains(reported.Hint, want.hints) {
				t.Errorf("the hint does not say %q:\n%s", want.hints, reported.Hint)
			}
		})
	}
}

// An ignore on a member the tag pins is the same contradiction as an ignore on
// a member the ladder settles.
func TestIgnoreOfATaggedMemberIsRefused(t *testing.T) {
	_, err := planFor(t, pair{pkg: modelPkg, source: "User", target: "Sticker", ignore: "Email"})
	if err == nil {
		t.Fatal("an ignore of a tag-pinned member was accepted")
	}
	reported, ok := plugin.From(err)
	if !ok {
		t.Fatalf("%v is not a diagnostic", err)
	}
	if got := reported.Code.String(); got != "FRG3031" {
		t.Errorf("reported as %s, want FRG3031: %s", got, reported.Message)
	}
}

// A tag may name a promoted member — the author wrote the name and Go
// resolves it — and a tag displaces the exact match the ladder would have
// made.
func TestATagReachesPromotedAndDisplacesTheLadder(t *testing.T) {
	rooted, err := planFor(t, pair{pkg: modelPkg, source: "Rooted", target: "Cored"})
	if err != nil {
		t.Fatalf("the promoted pair was refused: %v", err)
	}
	if want := (settlement{via: settledField, from: "Core", tagged: true}); settlements(rooted)["Core"] != want {
		t.Errorf("Core settled as %+v, want %+v", settlements(rooted)["Core"], want)
	}

	poached, err := planFor(t, pair{pkg: modelPkg, source: "Rich", target: "Poached"})
	if err != nil {
		t.Fatalf("the displacing pair was refused: %v", err)
	}
	if want := (settlement{via: settledField, from: "Contact", tagged: true}); settlements(poached)["Email"] != want {
		t.Errorf("Email settled as %+v, want %+v", settlements(poached)["Email"], want)
	}
}

// The shapes a from tag is refused in, wherever in the tag they hide: a
// contradiction is refused whichever entry would have answered, and an entry
// that is not two identifiers is never mistaken for a sibling mapping's.
func TestAFromTagIsHeldToItsShape(t *testing.T) {
	cases := map[string]struct {
		target string
		says   string
	}{
		"parens in the qualifier":        {target: "QualifierGarbled", says: "Src().A"},
		"a leading dot":                  {target: "LeadingDot", says: ".A"},
		"a tag that says nothing":        {target: "BlankTag", says: "nothing"},
		"bare entries masked by a match": {target: "MaskedBare", says: "X"},
		"a member no declaration could":  {target: "NonIdentifier", says: "Src.-"},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := planFor(t, pair{pkg: refusedPkg, source: "Src", target: want.target})
			if err == nil {
				t.Fatalf("a constructor was planned for %s", want.target)
			}

			reported, ok := plugin.From(err)
			if !ok {
				t.Fatalf("%v is not a diagnostic", err)
			}
			if got := reported.Code.String(); got != "FRG3034" {
				t.Errorf("reported as %s, want FRG3034: %s", got, reported.Message)
			}
			if !strings.Contains(reported.Message, want.says) {
				t.Errorf("the complaint does not mention %q:\n%s", want.says, reported.Message)
			}
		})
	}
}

// renderedUnit prints a unit and everything it provides, the way the emitter
// would, so a test can read the whole of what a declaration generates.
func renderedUnit(t *testing.T, unit plugin.Unit) string {
	t.Helper()

	file := emit.File{Package: "model"}
	file.Sections = append(file.Sections, emit.Section{
		Decls: unit.Decls, Comments: unit.Comments, Fset: unit.Fset,
	})
	file.Imports = append(file.Imports, unit.Imports...)
	for _, held := range unit.Provides {
		file.Sections = append(file.Sections, emit.Section{
			Decls: held.Decls, Comments: held.Comments, Fset: held.Fset,
		})
		file.Imports = append(file.Imports, held.Imports...)
	}

	out, err := file.Render()
	if err != nil {
		t.Fatalf("rendering the unit: %v", err)
	}
	return string(out)
}

// A bridge over the codec generates the fused writers beside the constructor.
func TestABridgeOverTheCodecFuses(t *testing.T) {
	loaded := loadFixture(t)
	unit, err := New().Generate(contextFor(t, loaded, pair{
		pkg: modelPkg, source: "User", target: "Person", json: true,
	}), plugin.Shape{})
	if err != nil {
		t.Fatalf("fusing was refused: %v", err)
	}

	text := renderedUnit(t, unit)
	for _, want := range []string{
		"func PersonFromUser(src *User) Person",
		"func AppendPersonJSONFromUser(dst []byte, src *User) ([]byte, error)",
		"func WritePersonJSONFromUser(w io.Writer, src *User) (int64, error)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the fused unit does not contain %q:\n%s", want, text)
		}
	}
}

// Without the codec beneath it, nothing changes: stage 1's output, no writers.
func TestABridgeAloneStillOnlyConstructs(t *testing.T) {
	loaded := loadFixture(t)
	unit, err := New().Generate(contextFor(t, loaded, pair{
		pkg: modelPkg, source: "User", target: "Person",
	}), plugin.Shape{})
	if err != nil {
		t.Fatalf("the plain pair was refused: %v", err)
	}
	if text := renderedUnit(t, unit); strings.Contains(text, "AppendPersonJSONFromUser") {
		t.Error("a bridge with no codec beneath it grew writers")
	}
}

// A hint that reads the value under construction is fine in a constructor —
// the literal exists by then — and refused under fusion, where no target value
// ever does.
func TestAFusedHintCannotReadDst(t *testing.T) {
	loaded := loadFixture(t)

	if _, err := planned(contextFor(t, loaded, pair{
		pkg: modelPkg, source: "User", target: "Renamed", hint: "echoes",
	})); err != nil {
		t.Fatalf("a constructor-only hint reading dst was refused: %v", err)
	}

	_, err := New().Generate(contextFor(t, loaded, pair{
		pkg: modelPkg, source: "User", target: "Renamed", hint: "echoes", json: true,
	}), plugin.Shape{})
	if err == nil {
		t.Fatal("a fused hint reading dst was accepted")
	}
	reported, ok := plugin.From(err)
	if !ok {
		t.Fatalf("%v is not a diagnostic", err)
	}
	if got := reported.Code.String(); got != "FRG3032" {
		t.Errorf("reported as %s, want FRG3032: %s", got, reported.Message)
	}
	if !strings.Contains(reported.Message, "echoes") {
		t.Errorf("the complaint does not name the hint:\n%s", reported.Message)
	}
}
