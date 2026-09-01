package main

import (
	"errors"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/resolve"
	"github.com/okian/forge/internal/subject"
)

// A loader turns package patterns into one loaded session.
type loader interface {
	Load(load.Config) (*load.Session, error)
}

// A discoverer finds the declarations in a session that might be requests.
type discoverer interface {
	Discover(*load.Session) ([]discover.Candidate, diag.Set)
}

// A resolver follows each candidate to the stack it names.
type resolver interface {
	Resolve([]discover.Candidate) ([]resolve.Declaration, diag.Set)
}

// A modeller builds the subject each declaration is specialised to.
//
// It takes the whole set rather than one at a time because two declarations
// over one subject share its model, and so do a subject and another subject
// that reaches it — which is what lets generation emit one codec for a type
// rather than one per declaration that mentions it. A stage handed one
// declaration at a time could not share anything.
type modeller interface {
	Model(subject.Config, []resolve.Declaration) ([]request, diag.Set)
}

// pipeline is the path every verb walks before the verbs differ.
//
// Three of them — writing files, checking they are fresh, and explaining what
// would be written — differ only in what they do with a resolved declaration,
// and all three are wrong in the same way if they disagree about how one is
// found. Sharing the path is what stops that: a verb cannot report a stack the
// other verbs would not have resolved.
//
// The stages are interfaces rather than calls so that a verb can be tested
// against declarations that were never on disk. The alternative is a fixture
// module per case, and a load that shells out to the go command for each of
// them — which is a suite people stop running.
type pipeline struct {
	loading     loader
	discovering discoverer
	resolving   resolver
	modelling   modeller
}

// stages returns the pipeline wired to the real ones.
func stages() pipeline {
	return pipeline{
		loading:     loading{},
		discovering: discovering{},
		resolving:   resolving{},
		modelling:   modelling{},
	}
}

// request is one declaration and what the walk has learned about it.
//
// The model is separate from the declaration because a declaration can survive
// resolution and be refused by the stage after it: a stack over a pointer, a
// type parameter or a predeclared type resolves perfectly well and has no
// subject a model can be built from. Recording the refusal here rather than
// dropping the declaration is what lets a later stage say which declaration it
// has nothing to say about, instead of silently having one fewer.
type request struct {
	// Declaration is what resolution found.
	Declaration resolve.Declaration

	// Model is the subject the stack is specialised to, and is nil when the
	// subject was refused. It is not named Subject, because the declaration
	// already carries one under that name and the two are different things: a
	// type there, a model of it here.
	Model *model.Struct

	// Diagnostics holds what was said about this declaration in particular.
	//
	// Kept here as well as in the walk's own set, because a verb that answers a
	// question about one declaration has to know whether that one had a problem
	// — and a package under repair usually has several that are nothing to do
	// with the question.
	Diagnostics diag.Set
}

// resolved is everything the shared path found.
type resolved struct {
	// Session is the load the declarations were found in. Later stages need it
	// for the file set their positions resolve against and for the packages
	// they will emit into.
	Session *load.Session

	// Candidates are every declaration discovery found, in the order it found
	// them, whether or not each went on to resolve or to be modelled.
	//
	// Kept beside the requests rather than folded into them, because the two
	// answer different questions. A verb acting on a declaration wants the ones
	// that survived; a verb asking what a package contains — which file belongs
	// to which declaration, say — wants every one that was written, since a
	// declaration whose marker was misspelled is still a declaration in the
	// package and the file generated for it is still its file.
	Candidates []discover.Candidate

	// Requests are the declarations that resolved, in the order discovery found
	// them, which is by package, then file, then position.
	Requests []request

	// Diagnostics holds what is wrong with the packages rather than with any
	// one declaration in them: a package that does not build, a directive
	// attached to nothing, a stack that does not resolve into a declaration at
	// all. What belongs to a declaration is on the declaration, in
	// [request.Diagnostics].
	//
	// Split that way because a verb answering a question about one declaration
	// has to report both — the fault in that declaration, and the fault in the
	// package that makes any answer about it provisional — while reporting the
	// faults of its neighbours would drown the answer. Every diagnostic is in
	// exactly one of the two, so [resolved.All] is a union rather than a merge.
	Diagnostics diag.Set
}

// All returns everything the walk found, about the packages and about each
// declaration in them, for a verb that acts on all of them.
func (r resolved) All() diag.Set {
	out := r.Diagnostics

	for _, one := range r.Requests {
		out.Merge(&one.Diagnostics)
	}
	return out
}

// follow loads, discovers and resolves, collecting what was wrong on the way.
//
// It returns an error only when the walk could not be attempted — a pattern the
// go command will not take, a toolchain that will not run. A package that does
// not compile is not that: it is a diagnostic, and the declarations in the
// packages that do compile are still worth having, because a run that refused
// to say anything until everything built would be useless in exactly the
// situation the author needs it.
func (p pipeline) follow(env *environment, cfg load.Config) (resolved, error) {
	env.progress("loading %s", strings.Join(cfg.Patterns, " "))

	session, err := p.loading.Load(cfg)
	if err != nil {
		return resolved{}, err
	}
	if session == nil {
		// Every stage below here reads the session, and the ones that take it
		// guard against this individually. Refusing at the seam says which
		// stage produced nothing instead of leaving a dereference to say it.
		return resolved{}, errors.New("the load produced no session")
	}
	env.progress("loaded %d packages", len(session.Packages))

	found := resolved{Session: session}
	found.Diagnostics.Merge(&session.Diagnostics)

	candidates, problems := p.discovering.Discover(session)
	found.Diagnostics.Merge(&problems)
	found.Candidates = candidates
	env.progress("found %d declarations", len(candidates))

	declarations, problems := p.resolving.Resolve(candidates)
	found.Diagnostics.Merge(&problems)
	env.progress("resolved %d stacks", len(declarations))

	// What the subject builder said stays on the declarations it said it about,
	// rather than being merged here as well: a diagnostic in both would be
	// reported twice by a verb that reads both.
	requests, _ := p.modelling.Model(subject.Config{
		Fset:      session.Fset,
		Owned:     session.Owned(),
		Docs:      session.FieldDocs(),
		Generated: session.Generated(),
	}, declarations)
	found.Requests = requests
	env.progress("modelled %d subjects", modelled(requests))

	return found, nil
}

// modelled counts the declarations whose subject a model could be built from.
func modelled(requests []request) int {
	built := 0
	for _, one := range requests {
		if one.Model != nil {
			built++
		}
	}
	return built
}

// loading is the ordinary loader.
type loading struct{}

// Load runs one go/packages session.
func (loading) Load(cfg load.Config) (*load.Session, error) { return load.Load(cfg) }

// discovering is the ordinary discoverer.
type discovering struct{}

// Discover scans the session's syntax for candidates.
func (discovering) Discover(session *load.Session) ([]discover.Candidate, diag.Set) {
	return discover.Declarations(session)
}

// resolving is the ordinary resolver.
type resolving struct{}

// Resolve follows each candidate to the stack it names.
func (resolving) Resolve(candidates []discover.Candidate) ([]resolve.Declaration, diag.Set) {
	return resolve.Declarations(candidates)
}

// modelling is the ordinary modeller.
type modelling struct{}

// Model builds every declaration's subject through one builder, so that two
// declarations over one type get one model of it.
func (modelling) Model(cfg subject.Config, declarations []resolve.Declaration) ([]request, diag.Set) {
	var diags diag.Set

	builder := subject.New(cfg)
	out := make([]request, 0, len(declarations))

	for _, decl := range declarations {
		// The rendered declaration goes down with the subject so that a refusal
		// can underline the type it refused. The builder is handed a type and
		// never sees the declaration it was written inside, and "subject
		// *Person is a pointer" leaves the reader to find it among four nested
		// layers.
		built, problems := builder.Build(decl.Subject, subject.Site{
			Pos:    decl.Candidate.Pos,
			Layout: model.LayoutOf(decl.Stack, decl.Subject),
		})
		diags.Merge(&problems)

		out = append(out, request{Declaration: decl, Model: built, Diagnostics: problems})
	}

	return out, diags
}
