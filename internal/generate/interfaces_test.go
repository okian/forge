package generate

import (
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/compose"
	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
	builder "github.com/okian/forge/internal/subject"
)

// interfacePkg is the fixture package the pack generates into.
const interfacePkg = "interfacefixture/model"

// The interfaces the pack expects to see claimed, written as a claim writes
// them.
//
// A list rather than a derivation. What the pack proves is that a run over
// these subjects produces exactly this, and a list computed from the same table
// the run reads would agree with a table that had gone wrong.
var expected = []string{
	"encoding.TextAppender",
	"encoding.TextMarshaler",
	"encoding.TextUnmarshaler",
	"fmt.Stringer",
	"io.ReaderFrom",
	"io.WriterTo",
	"json.Marshaler",
	"json.Unmarshaler",
	"slog.LogValuer",
	"sort.Interface",
	"sync.Locker",
}

// Which declarations a claim can be turned off from.
//
// A skip is a directive, and a directive is written above a declaration — so
// what one can turn off is a claim about that declaration or about its subject,
// and nothing else. Written down here because it is the shape of the pack: the
// container earns the rows about many values, the wrapper earns the rows about
// one, and fmt.Stringer is earned by both subjects, so turning it off takes a
// skip on each.
var turnedFrom = map[string][]string{
	"encoding.TextAppender":    {"Codes"},
	"encoding.TextMarshaler":   {"Codes"},
	"encoding.TextUnmarshaler": {"Codes"},
	"fmt.Stringer":             {"People", "Codes"},
	"io.ReaderFrom":            {"People"},
	"io.WriterTo":              {"People"},
	"json.Marshaler":           {"People"},
	"json.Unmarshaler":         {"People"},
	"slog.LogValuer":           {"People"},
	"sort.Interface":           {"People"},
	"sync.Locker":              {"Locked"},
}

// Every interface this build can claim is one the pack earns, and every one the
// pack expects is one this build can claim.
//
// The gate the pack exists to be. A row added to the table without a subject
// that earns it is a row nothing exercises, and a table nothing exercises is
// wrong the day somebody relies on it. Held both ways, because the two failures
// are different: a row nothing covers is a gap, and an expectation no row backs
// is a test asserting something that was removed.
func TestThePackCoversEveryRow(t *testing.T) {
	written := make([]string, 0, len(synthesised))
	for _, row := range synthesised {
		written = append(written, row.spelled())
	}

	for _, row := range written {
		if !slices.Contains(expected, row) {
			t.Errorf("%s is a row the pack does not earn", row)
		}
	}
	for _, want := range expected {
		if !slices.Contains(written, want) {
			t.Errorf("the pack expects %s, which no row claims", want)
		}
		if len(turnedFrom[want]) == 0 {
			t.Errorf("%s is claimed by the pack and nothing says where it can be turned off", want)
		}
	}

	// And nothing says where an interface can be turned off that the pack does
	// not claim, which is the same discipline the other list is held to: a key
	// left behind for a row that was removed is an entry nothing exercises.
	for want := range turnedFrom {
		if !slices.Contains(expected, want) {
			t.Errorf("%s is named as skippable and the pack does not claim it", want)
		}
	}
}

// What the pack generates is recorded, so that a change to any of it is a diff
// somebody reads.
//
// Goldens rather than assertions about substrings. What these rows produce is
// code an author has to live with, and the thing worth reviewing when it
// changes is the code rather than a list of the names in it.
func TestTheInterfacePack(t *testing.T) {
	files, diags := interfacing(t, nil)
	if !diags.Empty() {
		t.Fatalf("the pack was refused:\n%s", diags.Render())
	}

	for _, file := range files {
		goldentest.Compare(t, file.Name, file.Content)
	}

	// And nothing recorded has stopped being written. Comparing per file only
	// asks about the ones that arrived: a file that stopped being produced
	// leaves its golden on disk unread, and the run that dropped it passes.
	recorded(t, files)

	// And it compiles, which is the half a golden cannot answer: a recorded
	// file is only a record, and every one of these claims is a line the
	// compiler either accepts or does not.
	packCompiles(t, files)
}

// recorded reports a golden nothing produced any more.
func recorded(t *testing.T, files []File) {
	t.Helper()

	held, err := os.ReadDir(filepath.Join("testdata", t.Name()))
	if err != nil {
		t.Fatalf("reading what was recorded: %v", err)
	}

	made := make([]string, 0, len(files))
	for _, file := range files {
		made = append(made, file.Name)
	}

	for _, one := range held {
		if !slices.Contains(made, one.Name()) {
			t.Errorf("%s is recorded and nothing writes it any more; delete it or find out why", one.Name())
		}
	}
}

// Each of the interfaces is claimed.
//
// Read off the whole output rather than asserted per row in its own test,
// because what is being checked is that they arrive together: a stack earns its
// codec, its order and its rendering from three different places, and the way
// that goes wrong is one of them quietly not arriving.
func TestWhatThePackClaims(t *testing.T) {
	held := packed(t)
	made := claims(held)

	for _, want := range expected {
		if !slices.Contains(made, want) {
			t.Errorf("the pack does not claim %s, only %v:\n%s", want, made, held)
		}
	}

	// The walk, which is claimed as a method expression rather than as an
	// interface and would pass the loop above however it was written.
	if !strings.Contains(held, "iter.Seq[Person] = (*People).All") {
		t.Errorf("the pack does not claim its walk:\n%s", held)
	}
}

// claims returns the interfaces a body of generated source claims.
//
// Read rather than matched as substrings, because what a claim is written as is
// the file's business: gofmt aligns a var block, so the space between the name
// and the equals sign is however many the widest line needed, and a test that
// matched on one of them would pass or fail on the length of an unrelated row.
func claims(held string) []string {
	var out []string

	for _, line := range strings.Split(held, "\n") {
		fields := strings.Fields(line)

		// A claim alone stands outside a group — var _ X = … — and one with
		// company stands inside one, as _ X = …; both are claims.
		if len(fields) >= 2 && fields[0] == "var" && fields[1] == "_" {
			fields = fields[1:]
		}
		if len(fields) < 3 || fields[0] != "_" || fields[2] != "=" {
			continue
		}
		if !slices.Contains(out, fields[1]) {
			out = append(out, fields[1])
		}
	}

	return out
}

// A skip turns off one of them and leaves the rest, whichever one it is.
//
// Run over every interface the pack claims rather than over one of them. The
// imports are why: each claim brings the import its interface is written under,
// and a claim turned off must not bring one — which is a different question per
// row, since some share a package and some are the only reason theirs is
// imported at all. Nothing here asserts an import: what catches one left behind
// is the type-checker in packCompiles, refusing a file that imports something
// nothing in it names.
func TestSkippingEachOfThem(t *testing.T) {
	for _, one := range expected {
		t.Run(one, func(t *testing.T) {
			on := make(map[string][]string, len(turnedFrom[one]))
			for _, where := range turnedFrom[one] {
				on[where] = []string{"//forge:skip " + one}
			}

			files, diags := interfacing(t, on)
			if !diags.Empty() {
				t.Fatalf("skipping %s was refused:\n%s", one, diags.Render())
			}

			held := joined(files)
			made := claims(held)

			if slices.Contains(made, one) {
				t.Errorf("%s is claimed after being skipped:\n%s", one, held)
			}

			for _, other := range expected {
				if other != one && !slices.Contains(made, other) {
					t.Errorf("skipping %s dropped %s, leaving %v:\n%s", one, other, made, held)
				}
			}

			packCompiles(t, files)
		})
	}
}

// And skipping everything one package gave still compiles.
//
// The case a per-interface skip cannot reach. Each claim brings the import its
// interface is written under, so skipping one of three leaves the import wanted
// by the other two — and it is only when the last of them goes that the file
// has an import nothing names, which is not a warning in Go but a package that
// does not build.
func TestSkippingEverythingOnePackageGave(t *testing.T) {
	cases := map[string]map[string][]string{
		"encoding": {"Codes": {
			"//forge:skip encoding.TextAppender",
			"//forge:skip encoding.TextMarshaler",
			"//forge:skip encoding.TextUnmarshaler",
		}},
		"io": {"People": {
			"//forge:skip io.WriterTo",
			"//forge:skip io.ReaderFrom",
		}},
		"encoding/json/v2": {"People": {
			"//forge:skip json.Marshaler",
			"//forge:skip json.Unmarshaler",
		}},
	}

	for name, on := range cases {
		t.Run(name, func(t *testing.T) {
			files, diags := interfacing(t, on)
			if !diags.Empty() {
				t.Fatalf("skipping everything %s gave was refused:\n%s", name, diags.Render())
			}

			packCompiles(t, files)
		})
	}
}

// A skip written on one declaration turns off a claim its subject earned from
// another, and is not reported as turning nothing off.
//
// The case two stages get wrong. A claim about a subject is written where a
// package's subjects share a file; a skip is written above a declaration. With
// two declarations over one subject, the one carrying the skip may not be the
// one whose layers earned the claim — so a stage judging the directive against
// its own declaration alone would report a skip that had already taken effect,
// and refuse a run that was correct.
func TestASkipForAClaimAnotherDeclarationEarned(t *testing.T) {
	files, diags := interfacing(t, map[string][]string{
		// Crowd is the second declaration over Person and has no codec of its
		// own, so the claim being turned off is one People's layers earned.
		"Crowd": {"//forge:skip json.Marshaler"},
	})

	if !diags.Empty() {
		t.Fatalf("a skip for a claim another declaration earned was reported:\n%s", diags.Render())
	}

	held := joined(files)
	if strings.Contains(held, "json.Marshaler   = *new(Person)") {
		t.Errorf("the claim was not turned off:\n%s", held)
	}

	// And the rest of what the subject claims is still there, so the skip took
	// one claim rather than the block.
	if !strings.Contains(held, "slog.LogValuer") {
		t.Errorf("skipping one claim dropped the others:\n%s", held)
	}
}

// A skip naming a claim some other subject earned turns nothing off, and is
// reported.
//
// The set a skip is judged against has to be the set it acts on. What a skip
// written on a declaration reaches is that declaration's claims and its
// subject's — so judging it against every subject in the package would accept a
// directive that changes nothing, which is the one thing this diagnostic
// exists to prevent.
func TestASkipNamingAnotherSubjectsClaim(t *testing.T) {
	cases := map[string]struct{ on, skip string }{
		"a wrapper's row on the container":  {"People", "encoding.TextAppender"},
		"the container subject's on a code": {"Codes", "slog.LogValuer"},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			files, diags := interfacing(t, map[string][]string{
				one.on: {"//forge:skip " + one.skip},
			})

			if diags.Empty() {
				t.Fatalf("a skip naming another subject's claim was accepted:\n%s", joined(files))
			}

			found := reportedIn(t, diags, "FRG3019")
			if !strings.Contains(found.Message, one.skip) {
				t.Errorf("the complaint does not name the skip:\n%s", found.Message)
			}
		})
	}
}

// reportedIn returns the one diagnostic carrying a code.
func reportedIn(t *testing.T, diags diag.Set, code string) diag.Diagnostic {
	t.Helper()

	var found []diag.Diagnostic
	for _, one := range diags.All() {
		if one.Code.String() == code {
			found = append(found, one)
		}
	}

	if len(found) != 1 {
		t.Fatalf("%d diagnostics carry %s, want one:\n%s", len(found), code, diags.Render())
	}
	return found[0]
}

// interfacing generates the pack, with whatever directives were written on the
// container.
func interfacing(t *testing.T, on map[string][]string) ([]File, diag.Set) {
	t.Helper()

	loaded := interfaceFixture(t)

	pkg, ok := loaded.Package(interfacePkg)
	if !ok {
		t.Fatalf("the fixture has no package %s", interfacePkg)
	}

	cfg := Config{
		Catalog:   compose.Catalog{Registry: layers.Builtins(), DefaultStorage: layers.DefaultStorage()},
		Forge:     "v1.2.3",
		Markers:   "v1.2.3",
		Toolchain: "go1.27.0",
		Generated: loaded.Generated(),
	}

	return Package(interfacePkg, "model", []Request{
		{
			Model: &model.Model{
				Name: "People", Form: model.FormSpec,
				Subject: modelling(t, loaded, pkg, "Person"),
				Pkg:     pkg, Pos: interfaceAt,
				Stack: []model.LayerRef{
					{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Collection"}, Kind: model.KindRefining},
					{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Slice"}, Kind: model.KindStorage},
					{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Json"}, Kind: model.KindElement},
				},
			},
			Directives: directed(append([]string{"//forge:collection sort=ID"}, on["People"]...)...),
		},
		{
			// A second declaration, over the wrapper, so that the rows about
			// one value are earned beside the rows about many. Nothing is asked
			// of its stack: what a wrapper earns comes from its own tag, and a
			// stack over it would only add rows already covered.
			Model: &model.Model{
				Name: "Codes", Form: model.FormSpec,
				Subject: modelling(t, loaded, pkg, "Code"),
				Pkg:     pkg, Pos: interfaceAt,
				Stack: []model.LayerRef{
					{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Slice"}, Kind: model.KindStorage},
				},
			},
			Directives: directed(on["Codes"]...),
		},
		{
			// A second declaration over Person, with no codec of its own. It is
			// what makes a skip written here one about a claim earned
			// elsewhere.
			Model: &model.Model{
				Name: "Crowd", Form: model.FormSpec,
				Subject: modelling(t, loaded, pkg, "Person"),
				Pkg:     pkg, Pos: interfaceAt,
				Stack: []model.LayerRef{
					{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Slice"}, Kind: model.KindStorage},
				},
			},
			Directives: directed(on["Crowd"]...),
		},
		{
			// A declaration behind a lock, with the lock exposed. It is the one
			// row nothing else here earns: a concurrency layer holds a lock and
			// does not export it, so sync.Locker is claimed only by a
			// declaration that asked for it in so many words.
			Model: &model.Model{
				Name: "Locked", Form: model.FormSpec,
				Subject: modelling(t, loaded, pkg, "Person"),
				Pkg:     pkg, Pos: interfaceAt,
				Stack: []model.LayerRef{
					{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Guarded"}, Kind: model.KindDecorator},
					{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: "Slice"}, Kind: model.KindStorage},
				},
			},
			Directives: directed(append([]string{"//forge:guarded expose=locker"}, on["Locked"]...)...),
		},
	}, cfg)
}

// interfaceAt is where the fixture's declarations were written.
var interfaceAt = token.Position{Filename: "model.go", Line: 12, Column: 6}

// directed builds the directives an author would have written above the
// container.
func directed(texts ...string) []discover.Directive {
	out := make([]discover.Directive, 0, len(texts))

	for i, text := range texts {
		name, args, _ := strings.Cut(strings.TrimPrefix(text, "//forge:"), " ")
		out = append(out, discover.Directive{
			Layer: name, Args: args, Text: text,
			ArgsOffset: len("//forge:") + len(name) + 1,
			Pos: token.Position{
				Filename: interfaceAt.Filename,
				Line:     interfaceAt.Line - len(texts) + i,
				Column:   1,
			},
		})
	}

	return out
}

// packed returns everything the pack wrote, as one body of source.
func packed(t *testing.T) string {
	t.Helper()

	files, diags := interfacing(t, nil)
	if !diags.Empty() {
		t.Fatalf("the pack was refused:\n%s", diags.Render())
	}

	return joined(files)
}

// joined runs the generated files together, for a test about what is in them
// rather than about which file it is in.
func joined(files []File) string {
	var out strings.Builder
	for _, file := range files {
		out.Write(file.Content)
		out.WriteString("\n")
	}
	return out.String()
}

// packCompiles type-checks the pack's output beside the subjects it is about,
// in both of the builds it is written for.
//
// Both, because the two halves of a spec-form declaration are never in one
// build and each is only compiled under its own tag. Everything is handed over
// each time and the constraints decide what is in scope, which is the arrangement
// under test as much as the declarations are: a file whose tag were wrong would
// be missing from one build or duplicated into the other, and only asking twice
// can tell.
func packCompiles(t *testing.T, files []File) {
	t.Helper()

	sources := []goldentest.Source{
		{Name: "model.go", Content: fixtureSource(t, "model.go")},
		{Name: "spec.go", Content: fixtureSource(t, "spec.go")},
	}

	for _, file := range files {
		sources = append(sources, goldentest.Source{
			Name: file.Name, Content: file.Content, Generated: true,
		})
	}

	for _, tags := range [][]string{nil, {"forgespec"}} {
		held := "the ordinary build"
		if len(tags) > 0 {
			held = "the " + tags[0] + " build"
		}

		if err := goldentest.Compiles(goldentest.Package{
			Path: "model", Tags: tags, Files: sources,
		}); err != nil {
			t.Fatalf("what the pack wrote does not compile in %s: %v", held, err)
		}
	}
}

// fixtureSource returns one of the fixture's own files, read rather than copied
// so that there is one account of it.
func fixtureSource(t *testing.T, name string) []byte {
	t.Helper()

	held, err := os.ReadFile(filepath.Join("testdata", "interfaces", "model", name))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	return held
}

// interfaceLoaded holds the load, which is read and never written.
var interfaceLoaded *load.Session

// interfaceFixture loads the fixture module once for the whole file.
func interfaceFixture(t *testing.T) *load.Session {
	t.Helper()

	if interfaceLoaded != nil {
		return interfaceLoaded
	}

	dir, err := filepath.Abs(filepath.Join("testdata", "interfaces"))
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

	interfaceLoaded = loaded
	return loaded
}

// modelling builds the model of one fixture subject.
func modelling(t *testing.T, loaded *load.Session, pkg *packages.Package, name string) *model.Struct {
	t.Helper()

	obj := pkg.Types.Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", interfacePkg, name)
	}

	held, is := types.Unalias(obj.Type()).(*types.Named)
	if !is {
		t.Fatalf("%s is a %T, want a named type", name, obj.Type())
	}

	built, problems := builder.New(builder.Config{
		Fset:      loaded.Fset,
		Owned:     loaded.Owned(),
		Docs:      loaded.FieldDocs(),
		Generated: loaded.Generated(),
	}).Build(held, builder.At(interfaceAt))
	if !problems.Empty() {
		t.Fatalf("modelling %s: %s", name, problems.Render())
	}

	return built
}
