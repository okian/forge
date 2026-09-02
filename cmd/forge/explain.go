package main

import (
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/okian/forge/internal/compose"
	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	resolution "github.com/okian/forge/internal/explain"
	generated "github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/options"
)

// here is the package a question is asked about when the command line names
// none.
const here = "."

// explain shows what one declaration resolves to.
func explain(env *environment, cmd command, args []string) error {
	flags := flagsFor(cmd)
	name := flags.String("t", "", "the declared type to explain (required)")
	document := flags.Bool("json", false, "write the resolution as JSON")

	packages, on, err := parse(env, cmd, flags, args)
	if !on || err != nil {
		return err
	}

	// Required, because explaining every declaration in a package is what the
	// other verbs are for: this one exists to answer a question about one
	// declaration, and without a name there is no question.
	if strings.TrimSpace(*name) == "" {
		return answered(cmd, flags, "explain needs a declaration to explain, as in -t Persons")
	}

	// One package, defaulted to the one forge was started in. Explaining a
	// declaration is a question about one of them, and a wider pattern is both
	// slower to load and ambiguous: two packages may each declare a Persons,
	// and nothing about -t says which was meant.
	where := here
	switch len(packages) {
	case 0:
	case 1:
		where = packages[0]
	default:
		return answered(cmd, flags, "explain takes one package, got %d", len(packages))
	}

	found, err := env.pipeline.follow(env, env.loadConfig(where))
	if err != nil {
		return err
	}

	asked, err := declared(cmd, flags, found.Requests, strings.TrimSpace(*name))
	if err != nil {
		// Everything, since a pattern that matched nothing and a package that
		// will not build both end here with no declaration of any name — and
		// blaming the argument that was right sends the reader to check the one
		// thing that was not wrong.
		env.report(found.All())
		return err
	}

	catalog := layers.Builtins()

	held := specialised(asked, catalog)
	reported := env.report(wrong(found, asked, catalog))
	stack := composed(asked, held, catalog)

	answer := resolution.Of(resolution.Declaration{
		Name:        asked.Declaration.Candidate.Name,
		Package:     within(asked.Declaration.Candidate),
		Position:    written(asked.Declaration.Candidate),
		Form:        asked.Declaration.Candidate.Form,
		Stack:       stack,
		Subject:     asked.Model,
		SubjectName: model.TypeString(asked.Declaration.Subject),
		Layout:      model.LayoutOf(stack, asked.Declaration.Subject),
		Model:       held,
	}, catalog)

	if err := write(env, answer, *document); err != nil {
		return err
	}

	// The answer is worth having either way and has already gone to standard
	// output in full. What the status reports is that this run found something
	// wrong — with the declaration or with the package holding it — which is
	// what every other verb's status reports too, and what a caller under set
	// -e has to know before piping the answer anywhere.
	if reported {
		return errReported
	}
	return nil
}

// wrong gathers everything a reader of this answer has to know before trusting
// it.
//
// What is wrong with this declaration and what is wrong with the package it
// sits in. Not what is wrong with its neighbours: a package under repair
// usually holds several that are nothing to do with the question, and answering
// a question about one of them by reporting the others is how a verb stops
// being worth running.
//
// The package's own faults are not the neighbours' faults, though. A type the
// package does not declare leaves this subject modelled from what the
// type-checker could still work out, and an answer built on that is worth
// having with the reason in view rather than presented as sound.
//
// And what would stop it being generated, which is the question behind the
// question. Somebody runs this because a run refused, or because the file does
// not hold what they expected.
func wrong(found resolved, one request, catalog *layer.Registry) diag.Set {
	out := found.Diagnostics
	out.Merge(&one.Diagnostics)

	refusals := refused(found, one, catalog)
	out.Merge(&refusals)

	return out
}

// refused says what is wrong with this declaration that would stop it being
// generated.
//
// By generating it and throwing the files away, rather than by repeating the
// checks. Everything after resolution can refuse: an option naming a field the
// subject has not got, a stack that does not compose, a layer that cannot build
// what it was asked for, two methods wanting one name. Each of those is
// reported by the stage that found it, none of them by the two calls this verb
// makes to describe a stack, and a verb that re-implemented the subset it felt
// like reporting would drift from the generator the first time either changed.
//
// It is the question behind the question. Somebody runs explain because a run
// refused, or because the file does not hold what they expected; an explanation
// that described the declaration cheerfully while the generator refused it
// would send them looking in the one place the fault is not.
//
// Two things are left out on purpose, both because they are faults of a set of
// declarations rather than of this one. Its neighbours are not generated, so
// nothing they are wrong about is reported here; and two declarations wanting
// one file cannot arise from generating a declaration on its own. Somebody
// asking about one declaration is asking about that one.
//
// What its neighbours will write is passed in all the same, because that is not
// a fault of theirs but a fact about this declaration's own field types: a
// closed set declared next door decides whether a field of it goes over the
// wire as a name, and a report that generated without knowing would describe a
// codec the run does not write.
//
// A layer whose generator is not written is left out as well. It says forge has
// not got there yet rather than that the author did anything wrong, and the
// report has a column for it: a step whose work is pending is marked as pending
// rather than blamed. Explaining a stack of markers that mostly have no
// generator yet is what this verb is for.
//
// A declaration with no model generates nothing and is refused for a reason
// already reported — the subject builder said it — so nothing is asked here.
//
// Against the load, like the verbs that keep their files. This one is usually
// run on a package that has already been generated, so a collision check unable
// to tell a previous run's declarations from the author's would answer about
// every name the last run left in the package; [against] says why the load is
// what settles that.
func refused(found resolved, one request, catalog *layer.Registry) diag.Set {
	if one.Model == nil {
		return diag.Set{}
	}

	pkg := one.Declaration.Candidate.Pkg
	if pkg == nil {
		return diag.Set{}
	}

	cfg := against(catalog, found.Session)
	cfg.Writes = generated.Writes(alongside(found.Requests, pkg.PkgPath), cfg)

	_, problems := generated.Package(pkg.PkgPath, pkg.Name,
		[]generated.Request{{Model: built(one), Directives: one.Declaration.Candidate.Directives}},
		cfg)

	// By code rather than by wording, and the code holds because a stub reports
	// a diagnostic rather than a plain error: an ordinary error is given a code
	// of its own by the stage that receives it, and would come back through
	// here as something else entirely.
	var out diag.Set
	for _, held := range problems.All() {
		if held.Code != layers.NotImplemented {
			out.Add(held)
		}
	}
	return out
}

// composed returns the stack as it will be generated, which is not always the
// stack as it was written.
//
// A refining layer written over no storage has one filled in beneath it, and an
// explanation that left it out would describe a stack nothing generates: the
// declared type would appear to have the collection's methods and none of the
// container's, and a reader counting on that would find four more in the file.
//
// A stack that does not compose is explained as it was written, which is the
// only rendering there is: a stack refused for its shape composes nothing at
// all, and one refused by a layer composes as far as that layer and stops. An
// answer built on either would drop the layers above without saying so, and
// those are the half of the declaration somebody in trouble is usually asking
// about.
//
// What composition said is dropped here and not lost: [refused] composes the
// same declaration through the generator and reports every word of it. Saying
// it twice would make one fault read as two.
//
// A declaration with no model is not composed at all. The shape a stack is
// checked against starts at the subject, so a subject that could not be
// modelled makes every layer above it refuse for a reason that is not theirs.
func composed(one request, held *model.Model, catalog *layer.Registry) []model.LayerRef {
	written := one.Declaration.Stack
	if held == nil {
		return written
	}

	out, problems := compose.Compose(compose.Declaration{
		Stack:   written,
		Subject: one.Model,
		Pos:     one.Declaration.Candidate.Pos,
		Model:   held,
	}, compose.Catalog{Registry: catalog, DefaultStorage: layers.DefaultStorage()})

	if !problems.Empty() {
		return written
	}
	return out.Stack()
}

// specialised builds the model a layer is asked what it exposes against, or nil
// when the declaration has no model to build one from.
//
// The options are read here rather than left out, because a layer whose methods
// are named after them cannot say what it emits without them: a collection told
// to sort by Name puts SortedByName on the declared type, and an explanation
// that listed only the methods every collection has would be answering a
// simpler question than the one asked.
//
// What was accepted rather than what was written, because that is what a layer
// is handed when it generates and what an explanation is describing. A refused
// option is not generated from, and [refused] reports every one of them by
// reading the same directives through the same reader.
func specialised(one request, catalog *layer.Registry) *model.Model {
	held := built(one)
	if held == nil {
		return nil
	}

	held.Options, _ = options.Read(options.Declaration{
		Pos:        held.Pos,
		Directives: one.Declaration.Candidate.Directives,
		Stack:      held.Stack,
		Subject:    held.Subject,
	}, catalog)

	return held
}

// write renders a resolution in whichever form was asked for.
func write(env *environment, answer resolution.Resolution, document bool) error {
	if document {
		return answer.JSON(env.stdout)
	}
	return answer.Text(env.stdout)
}

// answered reports a mistake in the command line whose answer is this command's
// own flags rather than the list of every command there is.
//
// Somebody who typed the right verb and the wrong flag is told what that verb
// takes; the command list does not contain it, and printing it makes them read
// past six lines that cannot help.
func answered(cmd command, flags *flag.FlagSet, format string, args ...any) error {
	return misuse{
		err:    fmt.Errorf(format, args...),
		answer: func(w io.Writer) { describe(w, cmd, flags) },
	}
}

// written says where a declaration was written, or nothing when it has no
// position. token.Position spells the absence "-", which reads as a file with
// that name rather than as an absence.
func written(c discover.Candidate) string {
	if !c.Pos.IsValid() {
		return ""
	}
	return c.Pos.String()
}

// within names the package a declaration lives in.
//
// A candidate carries the package it was found in, and a declaration assembled
// by anything other than a load may not have one. Reading through it would turn
// a missing field into a crash in the one verb somebody runs when they are
// already confused about what they wrote.
func within(c discover.Candidate) string {
	if c.Pkg == nil {
		return ""
	}
	return c.Pkg.PkgPath
}

// declared finds the one declaration a question is about.
//
// By the declared type's own name, which is what an author types and what a
// failure can echo back. Two packages may each declare a Persons and nothing
// about the name says which was meant, so a name matching twice is refused
// rather than answered about whichever was found first.
func declared(cmd command, flags *flag.FlagSet, requests []request, name string) (request, error) {
	var matched []request
	for _, one := range requests {
		if one.Declaration.Candidate.Name == name {
			matched = append(matched, one)
		}
	}

	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return request{}, missing(cmd, flags, requests, name)
	default:
		return request{}, answered(cmd, flags, "%s is declared in %s; name one of them",
			name, strings.Join(packagesOf(matched), " and "))
	}
}

// missing says that nothing of this name was found, and what was.
//
// The usual reason is a typo or the wrong package, and both are answered by
// seeing the list. Names are qualified when more than one package was loaded,
// since an unqualified list of seven names drawn from three packages invites
// the reader to go looking for all of them in one file.
func missing(cmd command, flags *flag.FlagSet, requests []request, name string) error {
	if len(requests) == 0 {
		return answered(cmd, flags, "no declaration named %s here, and none of any name", name)
	}

	qualify := len(packagesOf(requests)) > 1

	known := make([]string, 0, len(requests))
	for _, one := range requests {
		found := one.Declaration.Candidate.Name
		if pkg := within(one.Declaration.Candidate); qualify && pkg != "" {
			found = pkg + "." + found
		}
		known = append(known, found)
	}

	slices.Sort(known)
	return answered(cmd, flags, "no declaration named %s here; what is declared is %s",
		name, strings.Join(slices.Compact(known), ", "))
}

// packagesOf names the packages a set of declarations came from, once each and
// in one order.
func packagesOf(requests []request) []string {
	var found []string
	for _, one := range requests {
		if pkg := within(one.Declaration.Candidate); pkg != "" {
			found = append(found, pkg)
		}
	}

	slices.Sort(found)
	return slices.Compact(found)
}

// alongside returns every request of one package as generation reads them.
//
// The package and not the run, for the reason [generated.Config.Writes] gives:
// what a declaration generates may not turn on which other packages somebody
// happened to name.
func alongside(requests []request, path string) []generated.Request {
	var out []generated.Request

	for _, one := range requests {
		if one.Model == nil {
			continue
		}
		if pkg := one.Declaration.Candidate.Pkg; pkg == nil || pkg.PkgPath != path {
			continue
		}
		out = append(out, generated.Request{
			Model:      built(one),
			Directives: one.Declaration.Candidate.Directives,
		})
	}

	return out
}
