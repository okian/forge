package main

import (
	"errors"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/resolve"
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
}

// stages returns the pipeline wired to the real ones.
func stages() pipeline {
	return pipeline{loading: loading{}, discovering: discovering{}, resolving: resolving{}}
}

// resolved is everything the shared path found.
type resolved struct {
	// Session is the load the declarations were found in. Later stages need it
	// for the file set their positions resolve against and for the packages
	// they will emit into.
	Session *load.Session

	// Declarations are the requests, in the order discovery found them, which
	// is by package, then file, then position.
	Declarations []resolve.Declaration

	// Diagnostics holds everything wrong that the walk could still walk past:
	// a package that does not build, a directive attached to nothing, a stack
	// that does not resolve. They are collected rather than returned one at a
	// time, because an author who has made three mistakes should learn about
	// three mistakes.
	Diagnostics diag.Set
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
	env.progress("found %d declarations", len(candidates))

	declarations, problems := p.resolving.Resolve(candidates)
	found.Diagnostics.Merge(&problems)
	found.Declarations = declarations
	env.progress("resolved %d stacks", len(declarations))

	return found, nil
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
