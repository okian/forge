package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/token"
	"go/types"
	"io"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/resolve"
	"github.com/okian/forge/internal/tags"
)

// asking runs explain over a stand-in holding these declarations.
func asking(t *testing.T, declarations []request, args ...string) ran {
	t.Helper()

	s := &stack{modelled: declarations}

	var out, errs bytes.Buffer
	env := &environment{stdout: &out, stderr: &errs, pipeline: over(s)}

	cmd, ok := lookup("explain")
	if !ok {
		t.Fatal("explain is not a command")
	}

	// Through the same mapping a run ends with, so that what a caller would be
	// told is what a test reads back.
	status := status(&out, &errs, cmd.run(env, cmd, args))

	return ran{status: status, out: out.String(), err: errs.String()}
}

// declaring builds a request the way the walk would, from a name and the layers
// the declaration names.
func declaring(name string, layers ...string) request {
	stack := make([]model.LayerRef, len(layers))
	for i, marker := range layers {
		stack[i] = model.LayerRef{Origin: model.TypeRef{Pkg: model.MarkerPkg, Name: marker}}
	}

	return request{
		Declaration: resolve.Declaration{
			Candidate: discover.Candidate{Name: name, Form: model.FormSpec},
			Stack:     stack,
			Subject:   subjectType,
		},
	}
}

// subjectType is a named struct standing in for the type a stack is
// specialised to, built rather than loaded because what explaining it needs is
// a name and a field count.
var subjectType = types.NewNamed(
	types.NewTypeName(token.NoPos, types.NewPackage("example.com/model", "model"), "Person", nil),
	types.NewStruct(nil, nil), nil)

// The question is about one declaration, so the answer is about that one and
// not about whichever the walk happened to find first.
func TestExplainingTheDeclarationThatWasAskedAbout(t *testing.T) {
	got := asking(t, []request{
		declaring("Sessions", "Collection"),
		declaring("Persons", "Collection", "Ring", "Json"),
		declaring("Orders", "Collection"),
	}, "-t", "Persons")

	if !strings.Contains(got.out, "Persons") {
		t.Errorf("the answer is not about Persons:\n%s", got.out)
	}
	for _, other := range []string{"Sessions", "Orders"} {
		if strings.Contains(got.out, other) {
			t.Errorf("the answer mentions %s:\n%s", other, got.out)
		}
	}
	// Every layer of the stack that was asked about, in the order resolution
	// reaches them.
	for _, want := range []string{"Json", "Ring", "Collection"} {
		if !strings.Contains(got.out, want) {
			t.Errorf("the answer does not reach %s:\n%s", want, got.out)
		}
	}
}

// A name that matches nothing is a mistake in the question, and the usual
// reason is a typo or the wrong package — both answered by seeing what the
// package does declare.
func TestExplainingSomethingNobodyDeclared(t *testing.T) {
	got := asking(t, []request{declaring("Persons", "Collection")}, "-t", "Person")

	if got.status == diag.ExitOK {
		t.Fatal("a name nothing declares was answered")
	}
	if !strings.Contains(got.err, "Persons") {
		t.Errorf("the failure does not say what the package declares:\n%s", got.err)
	}
}

// And when it declares nothing at all, saying so is better than listing an
// empty list.
func TestExplainingInAPackageThatDeclaresNothing(t *testing.T) {
	got := asking(t, nil, "-t", "Persons")

	if got.status == diag.ExitOK {
		t.Fatal("a name nothing declares was answered")
	}
	if !strings.Contains(got.err, "none of any name") {
		t.Errorf("the failure does not say the package declares nothing:\n%s", got.err)
	}
}

// read is the document as a program reads it, which is the whole point of there
// being one: a table nobody can parse would make every reader write this.
type read struct {
	Name  string `json:"name"`
	Steps []step `json:"steps"`
}

// step is one entry of a read document.
type step struct {
	Step int    `json:"step"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// The document is for a program, so it parses, and it carries the same walk the
// table shows.
func TestExplainingAsADocument(t *testing.T) {
	got := asking(t, []request{declaring("Persons", "Collection", "Ring", "Json")}, "-t", "Persons", "-json")

	var document read
	if err := json.Unmarshal([]byte(got.out), &document); err != nil {
		t.Fatalf("the document does not parse: %v\n%s", err, got.out)
	}

	if document.Name != "Persons" {
		t.Errorf("the document is about %s", document.Name)
	}
	want := []string{"Person", "Json", "Ring", "Collection"}
	if len(document.Steps) != len(want) {
		t.Fatalf("the document holds %d steps, want %d", len(document.Steps), len(want))
	}
	for i, step := range document.Steps {
		if step.Step != i+1 {
			t.Errorf("step %d is numbered %d", i+1, step.Step)
		}
		if step.Name != want[i] {
			t.Errorf("step %d is %s, want %s", i+1, step.Name, want[i])
		}
	}
}

// The answer goes to stdout, because it is what was asked for and somebody will
// pipe it into a program that reads documents.
func TestTheAnswerGoesToStandardOutput(t *testing.T) {
	got := asking(t, []request{declaring("Persons", "Collection")}, "-t", "Persons")

	if got.out == "" {
		t.Error("the answer went nowhere")
	}
	if got.err != "" {
		t.Errorf("the answer arrived on stderr:\n%s", got.err)
	}
}

// A declaration that reached here without a package or a position is still
// worth explaining, and reading through what is not there is a crash in the one
// verb somebody runs when they are already confused.
func TestExplainingADeclarationWithNoProvenance(t *testing.T) {
	got := asking(t, []request{declaring("Persons", "Collection")}, "-t", "Persons")

	if got.status != diag.ExitOK {
		t.Fatalf("exited %d:\n%s", got.status, got.err)
	}
	// token.Position spells an absent position "-", which reads as a file with
	// that name.
	if strings.Contains(got.out, "\n  -\n") {
		t.Errorf("an absent position was reported as one:\n%s", got.out)
	}
}

// The walk runs before the answer, so a package with something wrong elsewhere
// in it still answers the question that was asked — with the reason in view.
func TestExplainingAlongsideWhatWentWrong(t *testing.T) {
	s := &stack{
		modelled: []request{declaring("Persons", "Collection")},
		built:    complaint(diag.Code(2002), "subject *Person is a pointer"),
	}

	var out, errs bytes.Buffer
	env := &environment{stdout: &out, stderr: &errs, pipeline: over(s)}

	cmd, _ := lookup("explain")
	err := cmd.run(env, cmd, []string{"-t", "Persons"})

	// The answer is worth having either way; the status is what says this run
	// found something wrong with the declaration it was asked about.
	if !errors.Is(err, errReported) {
		t.Fatalf("a run that reported did not end as one: %v", err)
	}
	if !strings.Contains(errs.String(), "is a pointer") {
		t.Errorf("the diagnostic was not reported:\n%s", errs.String())
	}
	if !strings.Contains(out.String(), "Persons") {
		t.Errorf("the question was not answered:\n%s", out.String())
	}
}

// Explaining is a question, and asking it must not depend on being able to
// write the answer: a closed stream is reported rather than ignored.
func TestAnAnswerThatCannotBeWritten(t *testing.T) {
	s := &stack{modelled: []request{declaring("Persons", "Collection")}}
	env := &environment{stdout: refusing{}, stderr: io.Discard, pipeline: over(s)}

	cmd, _ := lookup("explain")
	if err := cmd.run(env, cmd, []string{"-t", "Persons"}); err == nil {
		t.Error("an answer that could not be written was reported as given")
	}
	if err := cmd.run(env, cmd, []string{"-t", "Persons", "-json"}); err == nil {
		t.Error("a document that could not be written was reported as given")
	}
}

// refusing is a stream that will not take anything.
type refusing struct{}

func (refusing) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// Where a declaration was written and which package it lives in are the first
// two things somebody checks when the answer is not the one they expected.
func TestExplainingSaysWhereTheDeclarationIs(t *testing.T) {
	asked := declaring("Persons", "Collection")
	asked.Declaration.Candidate.Pkg = &packages.Package{PkgPath: "example.com/model"}
	asked.Declaration.Candidate.Pos = token.Position{Filename: "model/spec.go", Line: 8, Column: 6}

	got := asking(t, []request{asked}, "-t", "Persons")

	for _, want := range []string{"model/spec.go:8:6", "example.com/model", "spec form"} {
		if !strings.Contains(got.out, want) {
			t.Errorf("the answer does not say %q:\n%s", want, got.out)
		}
	}
}

// Nothing about the shape of a command line says the flags come first, and the
// documented spelling of this one puts them last. Reading only the leading
// flags would take -t for a package name and then report that no type was
// asked about — denying the author supplied the flag they are looking at.
func TestFlagsWrittenAfterThePackage(t *testing.T) {
	held := []request{declaring("Persons", "Collection")}

	spellings := map[string][]string{
		"flags first":           {"-t", "Persons", "./model"},
		"package first":         {"./model", "-t", "Persons"},
		"flags either side":     {"-json", "./model", "-t", "Persons"},
		"package in the middle": {"-t", "Persons", "./model", "-json"},
	}

	for _, name := range []string{"flags either side", "flags first", "package first", "package in the middle"} {
		t.Run(name, func(t *testing.T) {
			s := &stack{modelled: held}

			var out, errs bytes.Buffer
			env := &environment{stdout: &out, stderr: &errs, pipeline: over(s)}

			cmd, _ := lookup("explain")
			if err := cmd.run(env, cmd, spellings[name]); err != nil {
				t.Fatalf("the question was not answered: %v", err)
			}
			if !strings.Contains(out.String(), "Persons") {
				t.Errorf("the answer is not about Persons:\n%s", out.String())
			}
			if got := strings.Join(s.given.Patterns, " "); got != "./model" {
				t.Errorf("loaded %q, want %q", got, "./model")
			}
		})
	}
}

// Two packages may each declare a Persons, and nothing about the name says
// which was meant — so a name that matches twice is refused rather than
// answered about whichever was found first.
func TestExplainingSomethingDeclaredTwice(t *testing.T) {
	first := declaring("Persons", "Collection")
	first.Declaration.Candidate.Pkg = &packages.Package{PkgPath: "example.com/model"}

	second := declaring("Persons", "Ring")
	second.Declaration.Candidate.Pkg = &packages.Package{PkgPath: "example.com/store"}

	got := asking(t, []request{first, second}, "-t", "Persons", "./...")

	if got.status != diag.ExitUsage {
		t.Errorf("exited %d, want %d", got.status, diag.ExitUsage)
	}
	for _, want := range []string{"example.com/model", "example.com/store"} {
		if !strings.Contains(got.err, want) {
			t.Errorf("the failure does not name %s:\n%s", want, got.err)
		}
	}
	if got.out != "" {
		t.Errorf("an ambiguous question was answered anyway:\n%s", got.out)
	}
}

// A list of names drawn from several packages is qualified, since an
// unqualified one invites the reader to go looking for all of them in one file.
func TestWhatIsDeclaredIsSaidWithItsPackage(t *testing.T) {
	first := declaring("Persons", "Collection")
	first.Declaration.Candidate.Pkg = &packages.Package{PkgPath: "example.com/model"}

	second := declaring("Sessions", "Ring")
	second.Declaration.Candidate.Pkg = &packages.Package{PkgPath: "example.com/store"}

	got := asking(t, []request{first, second}, "-t", "Nope", "./...")

	for _, want := range []string{"example.com/model.Persons", "example.com/store.Sessions"} {
		if !strings.Contains(got.err, want) {
			t.Errorf("the failure does not name %s:\n%s", want, got.err)
		}
	}
}

// And within one package they are not, since the package is the one the
// question was already about.
func TestWhatIsDeclaredInOnePackage(t *testing.T) {
	first := declaring("Persons", "Collection")
	first.Declaration.Candidate.Pkg = &packages.Package{PkgPath: "example.com/model"}

	got := asking(t, []request{first}, "-t", "Nope")

	if strings.Contains(got.err, "example.com/model.Persons") {
		t.Errorf("a name was qualified with the only package there is:\n%s", got.err)
	}
	if !strings.Contains(got.err, "Persons") {
		t.Errorf("the failure does not say what is declared:\n%s", got.err)
	}
}

// A pattern that matched nothing and a package that will not build both end
// with no declaration of any name, and blaming the argument that was right
// sends the reader to check the one thing that was not wrong.
func TestExplainingWhereNothingWouldLoad(t *testing.T) {
	s := &stack{loaded: complaint(diag.Code(5002), "stat ./nope: directory not found")}

	var out, errs bytes.Buffer
	env := &environment{stdout: &out, stderr: &errs, pipeline: over(s)}

	cmd, _ := lookup("explain")
	if err := cmd.run(env, cmd, []string{"-t", "Persons", "./nope"}); err == nil {
		t.Fatal("a package that would not load was answered about")
	}

	if !strings.Contains(errs.String(), "directory not found") {
		t.Errorf("the reason nothing was found was not reported:\n%s", errs.String())
	}
}

// A name that is only whitespace is no name at all, and the answer is the flags
// this command takes rather than the list of every command there is.
func TestExplainingSomethingUnnamed(t *testing.T) {
	for _, name := range []string{"", "   ", "\t"} {
		got := asking(t, []request{declaring("Persons", "Collection")}, "-t", name)

		if got.status != diag.ExitUsage {
			t.Errorf("-t %q exited %d, want %d", name, got.status, diag.ExitUsage)
		}
		if !strings.Contains(got.err, "-t") {
			t.Errorf("-t %q: the failure does not say what to type:\n%s", name, got.err)
		}
		if strings.Contains(got.err, "Commands:") {
			t.Errorf("-t %q: the command list was printed instead of this command's flags:\n%s", name, got.err)
		}
	}
}

// The module being generated for decides whether forge may attach a method to a
// type, and the stage that builds subjects is the one that needs it.
func TestTheModuleReachesTheSubjectBuilder(t *testing.T) {
	s := &stack{
		session:  loadedFrom("example.com/thismodule"),
		modelled: []request{declaring("Persons", "Collection")},
	}

	var out, errs bytes.Buffer
	env := &environment{stdout: &out, stderr: &errs, pipeline: over(s)}

	cmd, _ := lookup("explain")
	if err := cmd.run(env, cmd, []string{"-t", "Persons"}); err != nil {
		t.Fatalf("the question was not answered: %v", err)
	}

	if s.modelling.Module != "example.com/thismodule" {
		t.Errorf("subjects were built for module %q", s.modelling.Module)
	}
}

// Everything after a bare -- is an argument whatever it looks like. The
// terminator is how a caller passes a package whose name begins with a dash,
// and a promise that lasted one word would not be one.
func TestArgumentsAfterTheTerminator(t *testing.T) {
	held := []request{declaring("Persons", "Collection")}

	// Two arguments after it, so that a terminator whose effect ends at the
	// first is caught by the second.
	s := &stack{modelled: held}
	env := &environment{stdout: io.Discard, stderr: io.Discard, pipeline: over(s)}

	cmd, _ := lookup("explain")
	err := cmd.run(env, cmd, []string{"-t", "Persons", "--", "./model", "-json"})

	if _, wrong := errors.AsType[misuse](err); !wrong {
		t.Fatalf("two packages after the terminator were accepted: %v", err)
	}
	if !strings.Contains(err.Error(), "got 2") {
		t.Errorf("the second argument was read as a flag: %v", err)
	}
}

// A flag written before the terminator is still a flag.
func TestFlagsBeforeTheTerminator(t *testing.T) {
	s := &stack{modelled: []request{declaring("Persons", "Collection")}}

	var out bytes.Buffer
	env := &environment{stdout: &out, stderr: io.Discard, pipeline: over(s)}

	cmd, _ := lookup("explain")
	if err := cmd.run(env, cmd, []string{"-t", "Persons", "-json", "--", "./model"}); err != nil {
		t.Fatalf("the question was not answered: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(out.String()), "{") {
		t.Errorf("the answer is not a document:\n%s", out.String())
	}
	if got := strings.Join(s.given.Patterns, " "); got != "./model" {
		t.Errorf("loaded %q, want %q", got, "./model")
	}
}

// A name is looked up the way it was checked, or a name that passes the guard
// finds nothing.
func TestANameWrittenWithSpaceAroundIt(t *testing.T) {
	got := asking(t, []request{declaring("Persons", "Collection")}, "-t", "  Persons  ")

	if got.status != diag.ExitOK {
		t.Fatalf("exited %d:\n%s", got.status, got.err)
	}
	if !strings.Contains(got.out, "Persons") {
		t.Errorf("the answer is not about Persons:\n%s", got.out)
	}
}

// What a package declares reads the same way twice, whichever order the walk
// found it in, and says each name once.
func TestWhatIsDeclaredReadsTheSameWayTwice(t *testing.T) {
	held := []request{
		declaring("Sessions", "Collection"),
		declaring("Persons", "Collection"),
		declaring("Persons", "Ring"),
		declaring("Orders", "Collection"),
	}
	for i := range held {
		held[i].Declaration.Candidate.Pkg = &packages.Package{PkgPath: "example.com/model"}
	}

	got := asking(t, held, "-t", "Nope")

	// Sorted, so that one package reads one way; and each name once, so that a
	// name declared twice is not offered twice as the thing to type.
	if !strings.Contains(got.err, "Orders, Persons, Sessions") {
		t.Errorf("what is declared is not in one order, once each:\n%s", got.err)
	}
}

// The answer about a declaration whose subject was modelled says what was in
// it, which is the half of the report a reader checks first.
func TestExplainingADeclarationWithARealSubject(t *testing.T) {
	asked := declaring("Persons", "Collection")
	asked.Model = &model.Struct{Fields: []model.Field{
		{Name: "ID", Exported: true, Tags: []tags.Tag{{Key: "json", Name: "id"}}},
		{Name: "Name", Exported: true},
	}}

	got := asking(t, []request{asked}, "-t", "Persons")

	if got.status != diag.ExitOK {
		t.Fatalf("exited %d:\n%s", got.status, got.err)
	}
	if !strings.Contains(got.out, "struct model: 2 fields, 1 tag") {
		t.Errorf("the answer does not describe the subject:\n%s", got.out)
	}
}

// A package that does not build is a package whose types are partly worked out,
// so an answer about a declaration in it is worth having with the reason in
// view rather than presented as sound. This is the verb somebody runs while a
// package is under repair, and it must not be the one that stays quiet about it.
func TestExplainingInAPackageThatDoesNotBuild(t *testing.T) {
	s := &stack{
		modelled: []request{declaring("Persons", "Collection")},
		loaded:   complaint(diag.Code(5001), "undefined: Address"),
	}

	var out, errs bytes.Buffer
	env := &environment{stdout: &out, stderr: &errs, pipeline: over(s)}

	cmd, _ := lookup("explain")
	err := cmd.run(env, cmd, []string{"-t", "Persons"})

	if !errors.Is(err, errReported) {
		t.Fatalf("a package that does not build was answered about in silence: %v", err)
	}
	if !strings.Contains(errs.String(), "undefined: Address") {
		t.Errorf("the build failure was not reported:\n%s", errs.String())
	}
	if !strings.Contains(out.String(), "Persons") {
		t.Errorf("the question was not answered:\n%s", out.String())
	}
}

// What is wrong with a neighbour is not what is wrong with the declaration the
// question was about, and reporting the neighbours is how this verb stops being
// worth running in the package it exists for.
func TestExplainingBesideADeclarationThatIsBroken(t *testing.T) {
	asked := declaring("Persons", "Collection")
	neighbour := declaring("Sessions", "Collection")
	neighbour.Diagnostics = complaint(diag.Code(2002), "subject *Session is a pointer")

	var out, errs bytes.Buffer
	s := &stack{modelled: []request{asked, neighbour}}
	env := &environment{stdout: &out, stderr: &errs, pipeline: over(s)}

	cmd, _ := lookup("explain")
	if err := cmd.run(env, cmd, []string{"-t", "Persons"}); err != nil {
		t.Fatalf("a neighbour's problem was reported as this declaration's: %v", err)
	}
	if strings.Contains(errs.String(), "Session") {
		t.Errorf("a neighbour was reported:\n%s", errs.String())
	}
}
