package main

import (
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/okian/forge/internal/discover"
	resolution "github.com/okian/forge/internal/explain"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
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

	// What is wrong with this declaration, and what is wrong with the package
	// it sits in. Not what is wrong with its neighbours: a package under repair
	// usually holds several that are nothing to do with the question, and
	// answering a question about one of them by reporting the others is how a
	// verb stops being worth running.
	//
	// The package's own faults are not the neighbours' faults, though. A type
	// the package does not declare leaves this subject modelled from what the
	// type-checker could still work out, and an answer built on that is worth
	// having with the reason in view rather than presented as sound.
	about := found.Diagnostics
	about.Merge(&asked.Diagnostics)
	reported := env.report(about)

	answer := resolution.Of(resolution.Declaration{
		Name:        asked.Declaration.Candidate.Name,
		Package:     within(asked.Declaration.Candidate),
		Position:    written(asked.Declaration.Candidate),
		Form:        asked.Declaration.Candidate.Form,
		Stack:       asked.Declaration.Stack,
		Subject:     asked.Model,
		SubjectName: model.TypeString(asked.Declaration.Subject),
		Layout:      model.LayoutOf(asked.Declaration.Stack, asked.Declaration.Subject),
	}, layer.Builtins())

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
