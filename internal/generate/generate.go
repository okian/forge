package generate

import (
	"errors"
	"fmt"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/compose"
	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/merge"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/options"
	"github.com/okian/forge/internal/scalars"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/shared/jsonwire"
	"github.com/okian/forge/internal/shared/seq"
)

// What can go wrong between a declaration that resolved and a file that could
// be written.
var (
	// 4006 was "two declarations want one file", which a package writing one
	// file cannot have. It is not reused: a code is permanent, so that anything
	// referring to one — a suppression, a runbook, a search — does not come
	// back with the wrong answer years later.
	codeNoProvider    = diag.Register(4007, "nothing provides a helper a layer requires")
	codeLayerFailed   = diag.Register(4008, "layer could not generate")
	codeNothingGiven  = diag.Register(4014, "a layer handed over nothing where a declaration should be")
	codeImportUnnamed = diag.Register(4015, "a layer named an import path that is empty")
	codeBindsDisagree = diag.Register(4021, "two layers disagree about the name an import binds")
)

// Request is one declaration to generate for.
type Request struct {
	// Model is the declaration, resolved and modelled.
	Model *model.Model

	// Directives are the //forge: comments written above it, which are what the
	// options a layer is handed come from.
	Directives []discover.Directive
}

// Config is everything about a run that is not about one declaration.
type Config struct {
	// Catalog is the layers this build composes with.
	Catalog compose.Catalog

	// Versions are what the generated files record about how they were made:
	// this build of forge, the markers it resolves, and the toolchain that will
	// format and compile the output.
	//
	// All three are inputs to what is written, so all three are inputs to the
	// fingerprint that says whether it is still current — the same declarations
	// formatted by a later gofmt are different bytes.
	Forge     string
	Markers   string
	Toolchain string

	// Generated reports whether a declaration was written by a generator rather
	// than by hand, as [load.Session.Generated] answers it.
	//
	// The collision check needs it and nothing else here does. What a package
	// already declares is what generated code may not redeclare — and a
	// generated file is loaded with the package it belongs to, so a run without
	// this would report the whole of its own last output as a collision with
	// itself.
	//
	// No function means nothing was generated, which is what a caller with no
	// load has and what is true of the run before the first one.
	Generated func(token.Pos) bool

	// Writes is what the package's declarations will put on each subject, by
	// the identity of each, for a caller generating fewer of them than the
	// package holds.
	//
	// [Package] takes this answer from the requests it is given, which is the
	// whole package where a run generates one. A caller generating one
	// declaration to see what it would say — which is what explaining a
	// declaration does — is giving a subset, and a subset answers about its own
	// declarations only: a field whose type a neighbour gives a text codec
	// would be described one way here and generated another.
	//
	// The package and no wider. Answering across a run makes what one package
	// generates depend on which others were named on the command line, and lets
	// a declaration refused in one leave a call to a method that will never
	// exist in another.
	//
	// Nil is a fair answer, and is what a caller generating a whole package
	// passes: what those requests say is unioned in regardless.
	Writes map[string][]string

	// Ours reports whether a declaration is forge's own output in a package
	// this module may rewrite, as [load.Session.Ours] answers it.
	//
	// Narrower than Generated on purpose, and the difference is who may change
	// the file. A generated file in this module is one a later run rewrites, so
	// what it holds says nothing about what the next run will hold — and a
	// layer that read it would answer differently on a clean checkout than on a
	// full one. A generated file in a dependency is committed, released and
	// immutable from here: it is as fixed as anything its author typed, and a
	// layer that ignored it would be ignoring a method that is really there.
	Ours func(token.Pos) bool
}

// File is one file generation would write.
type File struct {
	// Name is what it is called, with no directory: every file of a package
	// sits beside the source it was generated from.
	Name string

	// Content is what goes in it, formatted.
	Content []byte

	// Pos is where the first declaration of the package was written, which is
	// where anything reported about the file points. A package has no position
	// of its own and every declaration in it is equally the reason the file is
	// there.
	Pos token.Position
}

// wide is what a declaration is generated against that it cannot answer for
// itself, because the answer is about the package rather than about it.
//
// Held together rather than passed one by one, because they are one kind of
// thing: each is a question a declaration would answer wrongly on its own, and
// each is answered by looking at every declaration before any of them is
// generated for. A fourth would go here rather than into another parameter.
type wide struct {
	// reads names the subjects that will be given a String, by the identity of
	// each. A field of one is rendered through it, and a declaration asked on
	// its own could only answer that by reading what the last run left behind.
	reads map[string]bool

	// bound is what every file this package gets written will bind, decided
	// across all of them for the reason [willBind] gives: two of the three
	// files a declaration reaches are shared with its neighbours.
	bound []model.Import

	// writes is what the run will put on each subject, which is what lets one
	// declaration be generated knowing what a neighbour will have made of a
	// type it holds as a field. Decided here for the reason [willWrite] gives:
	// asked after the loop it would be an answer about the last run.
	writes map[string][]string
}

// alongside merges what the caller said the package writes with what these
// requests say, so that a caller passing neither, one, or both gets the whole
// package's answer.
//
// Both are unions of [Layer.Writes] over declarations of one package, so
// neither can disagree with the other about a type they both cover — which is
// what makes merging them safe rather than a choice between two answers.
func alongside(held, here map[string][]string) map[string][]string {
	out := make(map[string][]string, len(held)+len(here))

	for _, one := range []map[string][]string{held, here} {
		for key, methods := range one {
			for _, method := range methods {
				if !slices.Contains(out[key], method) {
					out[key] = append(out[key], method)
				}
			}
		}
	}

	return out
}

// Writes returns what a package's declarations will put on each subject, for a
// caller that means to generate fewer of them than the package holds.
//
// [Config.Writes] says why a subset is not enough to answer it.
func Writes(requests []Request, cfg Config) map[string][]string {
	return willWrite(requests, cfg)
}

// widely answers the questions a declaration cannot answer for itself.
func widely(requests []Request, cfg Config, diags *diag.Set) wide {
	return wide{
		reads:  reading(requests),
		bound:  willBind(requests, cfg, diags),
		writes: alongside(cfg.Writes, willWrite(requests, cfg)),
	}
}

// Package generates the file one package's declarations ask for.
//
// One file, holding everything: what each declaration's stack wrote, what the
// declarations share, and what their subjects earned. A package used to get one
// file per declaration and one more for what they had between them, which read
// well in a diff and cost more than it was worth everywhere else — a rename
// left a file behind, two declarations whose names differed only in case wanted
// one file, and a declaration called Windows wanted a file the compiler leaves
// out of the build.
//
// A second file is written only where the language requires one. A spec-form
// declaration's type is written by forge under one constraint and by the
// author's own file under its complement, so the two can never be in scope
// together; the package's whole output then goes under the first constraint and
// [Stubs] stands in for it under the second. Exactly one of the two is in any
// build, so a package still has one generated file however it is compiled.
//
// The path and the name are both needed and are different things. The name goes
// in the package clause; the path is what the helpers a layer required are
// identified by, since a helper is emitted into a package rather than imported
// from one, and two packages of one name hold two of them.
//
// Everything is generated before anything is reported as done, because a
// package is written whole or not at all: a run that composed three
// declarations and then found the fourth wrong would leave a file holding an
// answer to a question nobody can see any more.
func Package(path, name string, requests []Request, cfg Config) ([]File, diag.Set) {
	var diags diag.Set

	var (
		units    []merge.Unit
		required []model.TypeRef
		names    []string
		judged   []judgement
		about    = make(map[string]layer.Unit)
		spec     bool

		// What only the package knows, decided before any declaration is
		// generated for.
		known = widely(requests, cfg, &diags)

		// What each declaration's layers put on its subject, gathered as they
		// go. A subject reached from two declarations earns from its tags once,
		// and what a layer wrote about it anywhere in the package is what that
		// one answer has to be decided against: one declaration redacting and
		// its neighbour not is an ordinary arrangement, and a subject earning a
		// log value from the second would hold the first layer's as well.
		putting = make(map[string][]string, len(requests))
	)

	for _, req := range requests {
		if req.Model == nil {
			continue
		}

		unit, was, problems, wrote := declaration(req, cfg, known)
		diags.Merge(&problems)

		if !problems.Empty() {
			continue
		}

		gathered(putting, wrote)
		gather(about, unit.Provides)
		judged = append(judged, promised(req, was))

		units = append(units, unit)
		required = append(required, unit.Requires...)
		names = append(names, req.Model.Name)
		spec = spec || req.Model.Form == model.FormSpec
	}

	// After every declaration, because what a subject earns is a fact about the
	// package: it is keyed by the subject rather than by whoever asked, and two
	// declarations over one subject would otherwise each contribute a copy.
	gather(about, earned(requests, putting, path, known, cfg, &diags))

	held := merge.Join(units...)

	shared, made, wrote := sharing(path, required, about, requests, cfg, &diags)
	if wrote {
		held = merge.Join(held, shared)
	}
	answered(judged, made, &diags)

	if held.Empty() {
		return nil, diags
	}

	where := policing{at: at(requests)}
	checked(held, where, &diags)

	var sum emit.Digest
	FingerprintPackage(&sum, requests, name, cfg)

	out, err := written(name, where.at, held, names, spec, cfg, &sum)
	if err != nil {
		diags.AddError(err)
		return nil, diags
	}

	return out, diags
}

// claims is what one declaration promised, kept until the run can answer for
// the skips written on it.
func promised(req Request, was claimable) judgement {
	return judgement{
		declared: req.Model.Name,
		subject:  model.TypeIdentity(req.Model.Subject.Type()),
		skipped:  skips(req.Directives),
		made:     was,
	}
}

// gathered records what one declaration's layers put on its subjects, against
// each subject rather than against the declaration that asked.
func gathered(into, wrote map[string][]string) {
	for on, names := range wrote {
		into[on] = append(into[on], names...)
	}
}

// checked holds a package's whole output to the rules a file has to keep.
//
// The whole file at once, which is the only granularity that answers now there
// is one of them. Two methods of one name on one type meet here whether two
// declarations wrote them or a declaration and a subject's own section did; two
// package-level declarations of one name meet here, which a fold of a package
// and a type into one identifier makes possible however carefully the fold is
// written; and the imports have to agree, which needs every layer of every
// declaration at once.
//
// Reported rather than resolved, because renaming one of two things forge chose
// the names for would leave a caller unable to guess either — and the case is
// rare enough that being told to rename a type is a better answer than a scheme
// nobody can predict.
func checked(held merge.Unit, of policing, diags *diag.Set) {
	claimed(held.Sections, of, diags)
	redeclared(held.Sections, of.at, diags)
	bound(held.Imports, of, diags)
}

// written renders what a package's output comes to: the file itself, and the
// one standing in for it under the tag where the package holds anything the tag
// excludes.
//
// Both carry the same fingerprint, because both are a function of the same
// inputs. A run that would rewrite one rewrites the other, which is what keeps
// a package from holding two files disagreeing about which declarations they
// were written from.
func written(
	name string, at token.Position, held merge.Unit, declared []string,
	spec bool, cfg Config, sum *emit.Digest,
) ([]File, error) {
	content, err := render(name, tagged(spec), at, held, cfg, sum)
	if err != nil {
		return nil, err
	}

	out := make([]File, 0, len(Names()))
	out = append(out, File{Name: Name(), Content: content, Pos: at})
	if !spec {
		return out, nil
	}

	// Everything the file holds, because everything the file holds is what the
	// tag excludes. What is stood in for used to be one declaration's methods
	// and is now the package's whole output, helpers included — a build with
	// the tag set has to find every name the other build has.
	standing := stubs(declared, held)
	if len(standing) == 0 {
		return out, nil
	}

	if content, err = renderStubs(name, at, standing, held.Imports, cfg, sum); err != nil {
		return nil, err
	}
	return append(out, File{Name: Stubs(), Content: content, Pos: at}), nil
}

// answered reports the skips that turned nothing off.
//
// Last, because a skip turns off a claim about a declaration or about its
// subject and those are decided in two places. Neither of them knows enough to
// say a directive turned nothing off — only the run does, and only once both
// have run.
//
// Against each declaration's own subject rather than against every subject the
// package has. What a skip written on a declaration can turn off is what
// [turned] hands to that subject's synthesis, so the set it is judged against
// has to be the set it acts on: judged against the wider one, a skip naming a
// claim some other subject earned would be accepted for doing nothing.
func answered(judged []judgement, shared map[string]claimable, diags *diag.Set) {
	for _, one := range judged {
		unclaimed(one.made.with(shared[one.subject]), one, diags)
	}
}

// judgement is one declaration's claims, kept until the run can answer for the
// skips written on it.
//
// What it holds rather than the request it came from, because the request is
// only reachable here through a path that already established these: a name to
// report against, the directives to answer for, and which subject's claims
// count as the same declaration's.
type judgement struct {
	declared string
	subject  string
	skipped  []discover.Directive
	made     claimable
}

// sharing works out what a package's declarations have between them, and
// reports whether there was anything at all.
//
// Two sources with one destination. The helpers this build knows how to write
// and the work the layers gave the subjects are the same kind of thing — one
// copy for the package, however many declarations wanted it — and differ only
// in who wrote them.
//
// A unit rather than a file, because a package is one file and this is part of
// it. What it still owns is the check against what the author declared: a
// subject's companion type — a builder, a patch — is named against nothing any
// declaration was checked for, so this is the only place a name of the author's
// it collides with can be found.
func sharing(path string, required []model.TypeRef, about map[string]layer.Unit,
	requests []Request, cfg Config, diags *diag.Set,
) (merge.Unit, map[string]claimable, bool) {
	built, problems := helpers(path, required, requests)
	diags.Merge(&problems)

	made := make(map[string]claimable)

	// Joined rather than flattened into one unit, because the helpers were
	// parsed under file sets of their own: a comment is found by its position,
	// and a position read against somebody else's file set lands a sentence in
	// the middle of a stranger's function.
	held := merge.Join(merge.Units(contributed(about)...), built)
	if held.Empty() {
		return merge.Unit{}, made, false
	}

	// The subjects earn claims here for the same reason the declarations earn
	// them in their own sections: what is written about a subject is written
	// here, so this is where a reader looks and where the compiler can be made
	// to check. Reported into the run's own set, since a package is written
	// whole and a claim that cannot be built is not a reason to write the rest.
	for _, one := range subjects(requests) {
		claims, imports, was, wrote := synthesise(held, synthesis{
			// What the file already binds is passed rather than left empty: a
			// spelling that ignored it would be a spelling for some other
			// file, writing the element type under forge's own idea of its
			// package's name in a file where a layer may have bound that path
			// to something else.
			declared: model.Spell(one.Type(), path, held.Imports).Text,

			// The subject stands in for its own element, which is not a
			// mistake and is not meaningful either: an element is what a
			// container holds, and a subject holds nothing. It is read only to
			// build the walk's signature, and a subject has no walk — so what
			// is written here is discarded, and writing the subject is what
			// keeps the field from being the one thing at hand that is wrong.
			elem:    model.Spell(one.Type(), path, held.Imports),
			pkg:     path,
			at:      one.Pos,
			skipped: turned(one, requests),
		}, diags)

		made[model.TypeIdentity(one.Type())] = was

		if wrote {
			held.Sections = append(held.Sections, claims)
			held.Imports = merged(held.Imports, imports)
		}
	}

	// Against what the author declared, which every declaration's own sections
	// were checked against and these were not. Named against nothing, because
	// no one declaration owns them: the exemption a declaration gets for its
	// own type has nothing to apply to here.
	taken(held.Sections, policing{held: holds(into(requests), cfg.Generated), at: at(requests)}, diags)

	return held, made, true
}

// into returns the package the declarations are generated into, which is the one
// they all share.
//
// From a declaration rather than from a parameter, because a package's own
// declarations are what a name is checked against and only a declaration
// carries them. A run with nothing in it has nothing to check either.
func into(requests []Request) *packages.Package {
	for _, req := range requests {
		if req.Model != nil {
			return req.Model.Pkg
		}
	}
	return nil
}

// reading returns the subjects of this package that will be given a String, by
// the identity of each.
//
// Computed for the package rather than per declaration, because it is a
// question about the run: a field is rendered through its type's String, and
// whether that type has one depends on whether anything in this run is over it.
// A declaration asked on its own could answer only by reading what the last run
// left on disk, which is how a package comes to build from a committed tree and
// fail from a clean checkout.
func reading(requests []Request) map[string]bool {
	out := make(map[string]bool, len(requests))

	for _, one := range subjects(requests) {
		if scalars.Earns(one) {
			out[model.TypeIdentity(one.Type())] = true
		}
	}

	return out
}

// turned returns the skips written on the declarations over a subject.
//
// A subject has no directives of its own — it is somebody's struct, and forge
// reads directives above declarations — so the only place an author can say
// they do not want one of its claims is on a declaration that caused it. Any of
// them is enough, because what a skip turns off is a claim rather than a
// method: not making one costs nothing that two declarations could disagree
// about.
func turned(subject *model.Struct, requests []Request) []discover.Directive {
	var out []discover.Directive

	held := model.TypeIdentity(subject.Type())

	for _, req := range requests {
		if req.Model == nil || req.Model.Subject == nil {
			continue
		}
		if model.TypeIdentity(req.Model.Subject.Type()) != held {
			continue
		}
		out = append(out, skips(req.Directives)...)
	}

	return out
}

// subjects returns the distinct subjects a package's declarations are over, in
// the order they were first named.
//
// Distinct, because what is written about a subject is written once however
// many declarations are over it, and a claim about it belongs beside that. In
// the order they were named rather than sorted, so that the claims read in the
// order the file's declarations do.
func subjects(requests []Request) []*model.Struct {
	var (
		out  []*model.Struct
		seen = make(map[string]bool, len(requests))
	)

	for _, req := range requests {
		if req.Model == nil || req.Model.Subject == nil {
			continue
		}

		held := model.TypeIdentity(req.Model.Subject.Type())
		if seen[held] {
			continue
		}

		seen[held] = true
		out = append(out, req.Model.Subject)
	}

	return out
}

// gather keeps what the layers contributed to something other than the
// declaration, once per thing it is about.
//
// Two declarations over one subject each get the same contribution from their
// element layers, and a package holding it twice does not compile. The key is
// what says the two are the same, so the first one under a key is the one the
// package keeps and the rest are that same answer arriving again.
func gather(into map[string]layer.Unit, held map[string]layer.Unit) {
	for what, one := range held {
		if _, seen := into[what]; !seen {
			into[what] = one
		}
	}
}

// contributed returns what the layers gave the package, in the order the keys
// sort.
//
// Sorted, because a map is walked in whatever order it feels like and this ends
// up as declarations in a file: two runs over one package would otherwise write
// the same helpers in different orders and every one of them would look like a
// change.
func contributed(about map[string]layer.Unit) []layer.Unit {
	out := make([]layer.Unit, 0, len(about))
	for _, what := range slices.Sorted(maps.Keys(about)) {
		out = append(out, about[what])
	}
	return out
}

// declaration generates for one declaration: what its options mean, what its
// stack composes to, and what each of its layers contributes.
func declaration(req Request, cfg Config, known wide) (merge.Unit, claimable, diag.Set, map[string][]string) {
	var diags diag.Set

	held := req.Model

	set, problems := options.Read(options.Declaration{
		Pos:        held.Pos,
		Directives: req.Directives,
		Stack:      held.Stack,
		Subject:    held.Subject,
	}, cfg.Catalog.Registry)
	diags.Merge(&problems)

	// Attached before composing rather than after, because a layer is asked
	// what it exposes while the stack is being composed and a layer whose
	// methods are named after its options cannot answer without them. A set
	// that came back with problems still reaches the layer, and is meant to:
	// the run is already going to be refused below, and a layer is written to
	// describe what it was given rather than to check it a second time.
	held.Options = set

	composed, problems := compose.Compose(compose.Declaration{
		Stack:   held.Stack,
		Subject: held.Subject,
		Pos:     held.Pos,
		Model:   held,
	}, cfg.Catalog)
	diags.Merge(&problems)

	if !diags.Empty() {
		return merge.Unit{}, claimable{}, diags, nil
	}

	// The stack composition arrived at, which is what the layers were asked
	// against and what the declaration means. It is not always what was
	// written: a refining layer over no storage has one filled in.
	held.Stack = composed.Stack()

	units := contributions(held, composed, cfg, known, &diags)

	if !diags.Empty() {
		return merge.Unit{}, claimable{}, diags, nil
	}

	// What this declaration's layers put on its subject, handed back rather
	// than acted on: what a subject earns from its own tags is decided against
	// every declaration of the package at once, and this is one of them.
	wrote := putOn(units)

	already := holds(held.Pkg, cfg.Generated)

	out := policed(merge.Units(units...), policing{
		held:     already,
		exposed:  composed.Exposed,
		at:       held.Pos,
		declared: held.Name,
	}, &diags)

	if !diags.Empty() {
		return merge.Unit{}, claimable{}, diags, nil
	}

	out, was := claiming(out, req, already, &diags)

	if !diags.Empty() {
		return merge.Unit{}, claimable{}, diags, nil
	}
	return out, was, diags, wrote
}

// claiming adds the assertions a declaration's output earns to it.
//
// Run after the collision policy rather than before it: what a declaration
// claims is what its methods add up to, and which methods those are is not
// settled until an override has taken the place of what would have been
// generated.
func claiming(out merge.Unit, req Request, already declared, diags *diag.Set) (merge.Unit, claimable) {
	held := req.Model

	claims, imports, was, made := synthesise(out, synthesis{
		declared: held.Name,
		elem:     model.Spell(held.Subject.Type(), held.Pkg.PkgPath, out.Imports),
		pkg:      held.Pkg.PkgPath,
		at:       held.Pos,
		held:     already,
		skipped:  skips(req.Directives),
	}, diags)

	if made {
		out.Sections = append(out.Sections, claims)
		out.Imports = merged(out.Imports, imports)
	}

	return out, was
}

// skips returns the directives asking for an interface not to be claimed.
func skips(directives []discover.Directive) []discover.Directive {
	var out []discover.Directive
	for _, one := range directives {
		if strings.EqualFold(one.Layer, model.SkipDirective) {
			out = append(out, one)
		}
	}
	return out
}

// earned returns what the package's subjects get from their own shape and tags.
//
// No layer is asked for these and none could be: what they answer is a tag an
// author wrote and a shape the subject has, neither of which is any layer's
// business. Gathered in with the layers' contributions rather than kept beside
// them, so that everything downstream — the collision policy, what a claim
// reads, what the file holds — sees one set of declarations.
//
// Once per subject, and for the package rather than for a declaration. What is
// written here goes on the subject and is keyed by it, so two declarations over
// one subject asking separately would ask the same question twice — and would
// answer it differently, because what a layer already wrote about that subject
// is something only the whole package knows. One declaration redacting and its
// neighbour not is an ordinary arrangement, and asking per declaration would
// have the neighbour earn a log value beside the one the first had written.
func earned(
	requests []Request, putting map[string][]string,
	path string, known wide, cfg Config, diags *diag.Set,
) map[string]layer.Unit {
	out := make(map[string]layer.Unit)

	for _, one := range subjects(requests) {
		written, err := scalars.For(scalars.Asked{
			Subject:   one,
			Local:     path,
			Bound:     known.bound,
			At:        one.Pos,
			Earning:   known.reads,
			Generated: cfg.Generated,
			Written:   append(slices.Clone(putting[one.Ref().Name]), one.Methods...),
		}, diags)
		if err != nil {
			diags.AddError(err)
			continue
		}

		gather(out, written)
	}

	return out
}

// putOn returns the methods the layers wrote, by the type each was written on.
//
// What it answers is whether a layer has already done what a tag would
// otherwise earn — a log value, a text codec, a rendering — so that the two are
// not written into one package twice. [scalars.Asked.Written] says why the
// layer's is the one that stays.
//
// Read off the declarations rather than taken from what a layer says about
// itself, because there is nothing a layer says about itself that would answer
// it: a surface describes the declared type, and what an element layer writes
// goes on the subject. The declarations are what will be in the file, which is
// the thing the question is really about.
//
// Keyed by the type rather than gathered under the declaration's own subject,
// because an element layer writes for everything the subject reaches: a stack
// over a type that merely holds a secret writes the method on the secret, and a
// key naming only the subject would not have it.
//
// By the receiver's bare name, which is what a method declaration carries. Two
// types of one name from two packages would be conflated by that, and cannot
// be: [methodOf] rejects a qualified receiver, so every key here is a type
// declared in the package being generated into — and a package cannot hold two
// of one name.
func putOn(units []layer.Unit) map[string][]string {
	out := make(map[string][]string)

	for _, unit := range units {
		for _, held := range unit.Provides {
			for _, decl := range held.Decls {
				on, name, is := methodOf(decl)
				if is && !slices.Contains(out[on], name) {
					out[on] = append(out[on], name)
				}
			}
		}
	}

	for _, held := range out {
		slices.Sort(held)
	}
	return out
}

// merged adds the imports an assertion needs to the ones the declarations
// already asked for, without repeating one.
func merged(held, adding []emit.Import) []emit.Import {
	for _, one := range adding {
		if !slices.Contains(held, one) {
			held = append(held, one)
		}
	}
	return held
}

// willWrite returns the methods this run will put on each of the
// subjects it generates for, keyed by the subject's type identity.
//
// One declaration's layers decide how another declaration's output reads a type
// it holds as a field. A codec writing a field of a closed set has to write the
// member's name rather than the number behind it, and whether the name can be
// written is settled by the declaration over the closed set — a neighbour,
// generated in the same run, that the codec's own declaration knows nothing
// about. So it is answered here, where every declaration is in view, and handed
// down as [layer.Context.Writes].
//
// Before anything is generated, which is the whole point. The methods are in
// the package by the end of the run, so a layer could find them by looking —
// but only on a run that was not the first. A generated file is part of the
// package and is loaded with it, so a codec that read the package would write
// the number into an empty checkout and the name on the next run, from one
// unchanged declaration, and the file would rewrite itself on alternate builds.
// Asking the layers instead gives the same answer however many times the run
// has been made.
//
// One package, which is as wide as this goes. The two declarations it answers
// for are usually in one package — a closed set is declared beside the type it
// closes over, since nothing else may declare methods on that type, and the
// struct holding one of its members is usually declared alongside — and a
// package is the unit everything else here is decided in.
//
// Wider was tried and is worse. Answering across the run makes what one package
// generates depend on which others were named on the command line, so the same
// tree generates two ways; it lets a declaration refused in one package leave a
// call to a method that will never exist in another, which nothing then
// reports; and it puts the whole effect in the file a package shares, which is
// the one file the freshness check does not compare. A field whose type is
// given a text codec in another package is written as the form underneath it,
// the same way on every run and from every invocation.
//
// Keyed by type identity rather than by the declaration, because that is the
// question: a field has a type, and what a neighbour did to it is a fact about
// the type. Two declarations over one subject contribute to one entry, which is
// right — what either of them writes is in the package, and a reader of the
// subject cannot tell which put it there and has no reason to care.
//
// Gathered from the whole stack rather than from its element layers, since
// which kinds put methods on a subject is a layer's business and not this
// function's. A layer that writes about the container answers nothing here.
func willWrite(requests []Request, cfg Config) map[string][]string {
	out := make(map[string][]string)

	for _, req := range requests {
		if req.Model == nil || req.Model.Subject == nil {
			continue
		}

		held := model.TypeIdentity(req.Model.Subject.Type())

		// The stack as it was written, and the storage a declaration naming no
		// container has filled in beneath it. Composing is what fills that in
		// and this runs before composing, so the default is claimed here the
		// way [willBind] claims it: a storage layer that put a method on the
		// subject would otherwise be the one layer nothing asked.
		for _, ref := range append(refs(req.Model.Stack), cfg.Catalog.DefaultStorage) {
			found, claims := cfg.Catalog.Registry.Lookup(ref)
			if !claims {
				continue
			}

			for _, method := range found.Writes() {
				if !slices.Contains(out[held], method) {
					out[held] = append(out[held], method)
				}
			}
		}
	}

	return out
}

// refs returns the markers a stack names, which is what a registry is asked by.
func refs(stack []model.LayerRef) []model.TypeRef {
	out := make([]model.TypeRef, len(stack))
	for i, one := range stack {
		out[i] = one.Origin
	}
	return out
}

// willBind returns what the files this package's declarations write into will
// bind: the union of what every layer any of them names says it imports, sorted
// by path.
//
// Worked out before anything generates, because it is the one thing a layer
// needs and cannot work out for itself. What makes it necessary at all is
// [layer.Layer.Binds]; what is decided here is how wide the answer is.
//
// The package, and it is not a choice. A stack writes into three files, and the
// declaration's own is the only one it has to itself: what an element layer
// writes goes into the file a package's subjects share, and a spec
// declaration's stand-ins go into another that is assembled out of each
// declaration's own imports. That last one is what settles it — the stand-in
// file's spelling *is* the declaration file's spelling, carried over
// unchanged — so a narrower answer per declaration would be two declarations
// writing one foreign package two ways into a file built from both. One answer
// for the package is the only consistent width available.
//
// What that costs is worth saying plainly, because somebody will meet it: what
// a declaration generates is a function of its neighbours. Adding a codec
// anywhere in a package reserves json, io and half a dozen other ordinary
// names, and a neighbouring declaration whose subject comes from a package
// called io regenerates as io2.Thing — a diff in a file whose own declaration
// nobody touched.
//
// The storage a refining layer gets when none is written is counted whether or
// not anything asked for it. Working out which packages will be given one means
// writing composition's rule a second time, and two walks written to the same
// rule stay in step until the day one of them is edited. A marker nothing in
// the catalog claims is skipped rather than refused: what is wrong with it is
// reported by the stage whose business it is, and a second complaint from here
// would name the same declaration twice.
func willBind(requests []Request, cfg Config, diags *diag.Set) []model.Import {
	var out []model.Import

	// Who reserved each path, and which disagreements have already been
	// reported. A package naming one layer from three declarations reaches the
	// same conflict three times, and three copies of one complaint is worse
	// than one: what is wrong is a fact about two layers, not about any of the
	// declarations that happen to name them.
	from := make(map[string]string)
	said := make(map[string]bool)

	where := at(requests)

	claim := func(ref model.TypeRef) {
		found, claims := cfg.Catalog.Registry.Lookup(ref)
		if !claims {
			return
		}

		for _, one := range found.Binds() {
			if one.Path == "" {
				continue
			}

			held, taken := from[one.Path]
			if !taken {
				from[one.Path] = ref.Name
				out = append(out, one)
				continue
			}
			if slices.Contains(out, one) || said[one.Path] {
				continue
			}
			said[one.Path] = true

			// One path, one name. A disagreement is reported rather than
			// settled here: whichever name were kept, the other output would
			// name a package under one the file does not bind it to, and
			// choosing silently would make what is generated depend on which
			// declaration the author happened to write first.
			//
			// The first name is kept and the run goes on, which is not a
			// judgement that it was the right one. Nothing is written — a
			// package with a diagnostic against it is not written at all — and
			// carrying on is what lets the run report the rest of what is wrong
			// rather than a cascade of spellings computed against a set with a
			// path missing from it.
			diags.Add(diag.New(codeBindsDisagree, where, "%s", disagreement(held, ref.Name, one)).
				WithHint("%s", bindsFault))
		}
	}

	claim(cfg.Catalog.DefaultStorage)
	for _, req := range requests {
		if req.Model == nil {
			continue
		}
		for _, ref := range req.Model.Stack {
			claim(ref.Origin)
		}
	}

	// Sorted, so that what a layer spells against is the same list however the
	// declarations were walked — and so that a name a spelling has to number is
	// numbered the same way in every file a run writes. Paths are unique by
	// now, so there is nothing left for a stable sort to keep in order.
	slices.SortFunc(out, func(a, b model.Import) int { return strings.Compare(a.Path, b.Path) })

	return out
}

// disagreement says which two layers cannot agree about a path, or which one
// cannot agree with itself.
//
// Two wordings, because they are two faults and only one of them is about a
// pair. A layer that names one path twice under two names is a bug its author
// can see and fix without knowing what else is in the stack, and reporting it
// as a disagreement with itself would read as forge being unable to count.
func disagreement(held, second string, one model.Import) string {
	if held == second {
		return fmt.Sprintf("the %s layer names %s twice, and not under one name", held, one.Path)
	}
	return fmt.Sprintf("the %s and %s layers disagree about what %s binds", held, second, one.Path)
}

// bindsFault says what an author can do about two layers that will not agree,
// which is nothing except say so.
const bindsFault = "one path binds one name, so forge cannot spell against both; " +
	"this is a fault in the layers rather than in the declaration, and reporting it " +
	"is better than choosing between them — a file built on either would name a package " +
	"under a name it does not bind"

// contributions asks every layer of a composed stack what it writes.
//
// Outermost first in the file, innermost first in the walk: a layer is
// generated against what is beneath it, and read above what is above it.
func contributions(
	held *model.Model, composed compose.Composed, cfg Config, known wide, diags *diag.Set,
) []layer.Unit {
	units := make([]layer.Unit, 0, len(composed.Steps))

	for i := len(composed.Steps) - 1; i >= 0; i-- {
		step := composed.Steps[i]

		found, claims := cfg.Catalog.Registry.Lookup(step.Layer.Origin)
		if !claims {
			continue
		}

		ctx := layer.ContextFor(held, step.Layer).
			Generating(composed.Exposed).
			Declaring(step.Declared).
			Holding(step.Holds).
			Binding(known.bound).
			Writing(known.writes, cfg.Ours)

		unit, err := generated(found, ctx, step.Below)
		if err != nil {
			diags.Add(refusal(err, held, step.Layer))
			continue
		}

		if wrong := misgiven(unit, step.Layer.Origin.Name, held.Pos); len(wrong) > 0 {
			for _, one := range wrong {
				diags.Add(one)
			}
			continue
		}

		units = append(units, unit)
	}

	return units
}

// misgiven reports what is wrong with what one layer handed over, before the
// merge folds it in with everybody else's.
//
// Here rather than after the merge, because the answer names the layer — and
// after the merge there is no layer to name. Both are things the merge would
// otherwise pass over in silence: a gap where a declaration should be is
// printed as nothing at all, and an import with no path is dropped, leaving a
// file that names a package it does not import and a diagnostic pointing at the
// generated line rather than at whoever wrote it.
func misgiven(unit layer.Unit, named string, at token.Position) []diag.Diagnostic {
	var out []diag.Diagnostic

	gaps := 0
	for _, decl := range unit.Decls {
		if decl == nil {
			gaps++
		}
	}
	if gaps > 0 {
		out = append(out, diag.New(codeNothingGiven, at,
			"the %s layer handed over %d declarations that are not there", named, gaps).
			WithHint("%s", reportFault))
	}

	for _, one := range unit.Imports {
		if one.Path == "" {
			out = append(out, diag.New(codeImportUnnamed, at,
				"the %s layer asked for an import bound to %s with no path", named, one.Name).
				WithHint("%s", reportFault))
		}
	}

	return out
}

// reportFault says what an author can do about a layer that misbehaved, which
// is nothing except say so.
const reportFault = "this is a fault in the layer rather than in the declaration; " +
	"report it with the declaration that produced it"

// generated asks a layer for its unit, surviving one that answers with a panic.
//
// A layer is the part of this a third party writes, and this is the path that
// writes files: a run that ended in a stack trace would tell an author their
// generator is broken in a form only its authors can read, and would do it
// after some of their package had already been rewritten. What a panic becomes
// is an error like any other, named after the layer that produced it.
func generated(one layer.Layer, ctx *layer.Context, below shape.Shape) (unit layer.Unit, err error) {
	defer func() {
		if caught := recover(); caught != nil {
			unit, err = layer.Unit{}, fmt.Errorf("%T: %v", caught, caught)
		}
	}()

	return one.Generate(ctx, below)
}

// refusal turns what a layer said into a diagnostic about the declaration.
//
// A layer that reported one is reported as it wrote it: it had the declaration
// and knew what was wrong. One that returned an ordinary error had something go
// wrong that it has no vocabulary for, and what an author can do about that is
// say so — so it is given a code, a position, and the name of the layer that
// produced it.
func refusal(err error, held *model.Model, ref model.LayerRef) diag.Diagnostic {
	if reported, ok := diag.From(err); ok {
		return reported
	}

	return diag.New(codeLayerFailed, held.Pos,
		"the %s layer could not generate for %s: %v", ref.Origin.Name, held.Name, err).
		WithHint("%s", "this is a fault in forge rather than in the declaration; report it with the declaration")
}

// helpers gathers what the declarations of a package required and did not
// declare, so that one copy is emitted however many asked.
func helpers(pkg string, required []model.TypeRef, requests []Request) (merge.Unit, diag.Set) {
	var diags diag.Set

	// Cloned before it is sorted, because the slice is the caller's and
	// compacting rewrites its tail: what was left behind was a zero reference
	// nobody asked for, hashed into the fingerprint of the file this builds.
	held := slices.Clone(required)
	slices.SortFunc(held, model.TypeRef.Compare)

	units := make([]layer.Unit, 0, len(held))
	for _, ref := range slices.Compact(held) {
		unit, err := provided(ref, pkg)
		if err != nil {
			diags.Add(diag.New(codeNoProvider, at(requests),
				"a layer requires %s and nothing in this build provides it: %v", ref, err).
				WithHint("%s", "this is a fault in forge rather than in the declaration; report it with the declaration"))
			continue
		}
		units = append(units, unit)
	}

	return merge.Units(units...), diags
}

// provided returns the declarations of a helper a layer required.
//
// Two helpers, and a switch rather than a registry: the shared view every
// query surface hands its results to, and the JSON wire runtime every
// generated codec writes bytes through. A registry would be worth having at
// the point where a helper arrives from outside this repository, and until
// then it is a lookup written twice.
func provided(ref model.TypeRef, pkg string) (layer.Unit, error) {
	switch ref {
	case seq.Ref(pkg):
		return seq.Unit(token.Position{})
	case jsonwire.Ref(pkg):
		return jsonwire.Unit(token.Position{})
	}
	return layer.Unit{}, errNoProvider
}

// at is where a diagnostic about a package rather than a declaration points,
// which is the first declaration in it — a package has no position of its own,
// and a report with none points at the working directory.
func at(requests []Request) token.Position {
	for _, req := range requests {
		if req.Model != nil {
			return req.Model.Pos
		}
	}
	return token.Position{}
}

// render writes a unit as the file it goes in.
//
// The header records what the file was made from, so that asking whether it is
// current costs a read rather than a regeneration.
func render(pkg, build string, at token.Position, unit merge.Unit, cfg Config, sum *emit.Digest) ([]byte, error) {
	file := emit.File{
		Package:  pkg,
		Build:    build,
		Pos:      at,
		Imports:  unit.Imports,
		Sections: unit.Sections,
		Header: emit.Header{
			Forge:     cfg.Forge,
			Markers:   cfg.Markers,
			Toolchain: cfg.Toolchain,
			Inputs:    sum.String(),
		},
	}

	// A unit's assertions are not written. Nothing generates one yet, and where
	// they go is a question about the whole of a package's output rather than
	// about one file — the stage that decides which interfaces a declaration
	// claims is the one that owns emitting the claims.
	return file.Render()
}

// renderStubs writes the file standing in for a package's output under the tag.
//
// It carries the tag itself, which is the complement of what every file it
// stands in for carries, so the two are never in scope together.
//
// No declaration is named on it, though every declaration in it belongs to one.
// What is reported about a file points at the declaration to edit, and this
// file has as many of those as the package has spec declarations; the shared
// file is nameless for the same reason and this is the same kind of file.
func renderStubs(pkg string, at token.Position, sections []emit.Section, imports []emit.Import, cfg Config, sum *emit.Digest) ([]byte, error) {
	file := emit.File{
		Package:  pkg,
		Build:    load.SpecTag,
		Pos:      at,
		Imports:  reaching(sections, imports),
		Sections: sections,
		Header: emit.Header{
			Forge:     cfg.Forge,
			Markers:   cfg.Markers,
			Toolchain: cfg.Toolchain,
			Inputs:    sum.String(),
		},
	}

	out, err := file.Render()
	if err != nil {
		return nil, err
	}
	return spaced(out), nil
}

// tagged returns the build constraint a package's generated file carries.
//
// A spec declaration and the file generated for it are two declarations of one
// name, and exactly one of them may be in scope. The spec is written under a
// tag and this is written under its complement, so the ordinary build sees the
// real type and a build with the tag sees the one the author wrote — which is
// what keeps the spec type-checked, and a rename of the subject a compile error
// rather than a stale comment.
//
// A package with no spec declaration carries none. The author's own files hold
// every type and carry no tag, so the methods have to be readable wherever
// those files are — which is everywhere. A constraint of any kind would take
// them out of some build the author already had, and buy nothing: there is no
// second declaration of anything to keep them apart from.
func tagged(spec bool) string {
	if spec {
		return "!" + load.SpecTag
	}
	return ""
}

// Fingerprint records everything one declaration's output depends on.
//
// Exported, because generating and checking must agree about it exactly. If the
// two assembled the input set even slightly differently, every check would
// report staleness and nothing anybody did would make it stop — so there is one
// collector and both verbs reach it here.
//
// The package clause is the one the file will carry rather than the one the
// model happens to hold, since it is the clause that is written; a caller with
// only a model would otherwise fingerprint one thing and render another.
//
// What goes in is what the output is a function of. The declaration and how it
// was written; the package it lands in, which is written into every file; every
// field of the subject in the order they are declared, with its type, its tags,
// and what its type already implements; the options; and the three versions,
// because the layers that write the file are compiled into forge, the markers
// decide what resolves, and the same declarations formatted by a later gofmt
// are different bytes.
//
// What is deliberately not in it is the output. A fingerprint of what was
// written answers "is this file what forge would write", which can only be
// answered by writing it — and the whole point is not to.
//
// The set is wider than what any one layer reads, deliberately, and the cost is
// visible: a subject that gains a method no layer looks at rewrites the header
// of every generated file that reaches it, with the body unchanged. The
// alternative is asking each layer what it reads, which under-approximates the
// first time a layer forgets — and an input left out is a file that is stale
// and reports itself current, which is the one failure nothing downstream can
// recover from.
func Fingerprint(sum *emit.Digest, req Request, pkg string, cfg Config) {
	versions(sum, cfg)

	held := req.Model
	if held == nil {
		return
	}

	sum.AddString("declaration", held.Name)
	sum.AddString("form", held.Form.String())
	sum.AddString("stack", held.Layout().Text)

	sum.AddString("package name", pkg)
	if held.Pkg != nil {
		sum.AddString("package path", held.Pkg.PkgPath)
	}

	for i, one := range req.Directives {
		sum.AddString("directive "+strconv.Itoa(i), one.Text)
	}

	fields(sum, held.Subject)
}

// FingerprintPackage records everything a package's generated file is made
// from, which is every declaration in it.
//
// Exported for the reason [Fingerprint] is: generating and checking must agree
// about it exactly, or every check reports staleness and nothing anybody does
// makes it stop.
//
// Each declaration goes in under its own name, as one input holding its whole
// fingerprint. Folding them in flat would let two declarations swap subjects
// and hash alike — a digest sorts what it is given, so the same field recorded
// against two declarations is the same pair of inputs whichever way round they
// were. It also makes the file's own line the thing a reader compares: one
// declaration changing changes one input.
//
// The helpers the declarations asked for are not recorded, and do not need to
// be. What a package requires is a function of its declarations and of the
// version of forge that read them, and both of those are here already — so a
// fingerprint that named the helpers would be recording the same fact twice,
// and would cost the check verb the whole of a generation run to work out.
// Which is exactly what used to keep the shared file from being checked at all.
func FingerprintPackage(sum *emit.Digest, requests []Request, pkg string, cfg Config) {
	versions(sum, cfg)
	sum.AddString("package name", pkg)

	for _, req := range requests {
		if req.Model == nil {
			continue
		}

		var one emit.Digest
		Fingerprint(&one, req, pkg, cfg)
		sum.AddString("declaration "+req.Model.Name, one.String())
	}
}

// versions records what wrote a file, which every generated file depends on
// however little else it does.
func versions(sum *emit.Digest, cfg Config) {
	sum.AddString("forge", cfg.Forge)
	sum.AddString("markers", cfg.Markers)
	sum.AddString("toolchain", cfg.Toolchain)
}

// fields records what the layers read out of a subject: every field in the
// order it is declared, its type, its tags, and what it already implements —
// and the same of everything reachable from it.
//
// In order, and that is the whole of why the position is in the name. A digest
// sorts its inputs before hashing, so two fields recorded under their own names
// hash alike however they are arranged — and generated output follows the
// declaration order, so a struct whose fields were merely rearranged writes a
// different file and would otherwise report itself current. Rearranging fields
// is not exotic: it is what every tool that reports struct padding asks for.
//
// The closure as well as the subject, because a layer that walks it generates
// from what it finds — a codec for a nested struct changes when that struct
// does, and a declaration whose own source did not move would report itself
// current.
func fields(sum *emit.Digest, held *model.Struct) {
	if held == nil {
		return
	}

	record := func(of *model.Struct) {
		at := of.Ref().String()

		// What the type already carries decides what a layer generates rather
		// than delegates to, and what it may not declare a second time.
		for _, iface := range of.Implements {
			sum.AddString(at+" implements", iface.String())
		}
		for _, method := range of.Methods {
			sum.AddString(at+" declares", method)
		}
		sum.AddString(at+" is", placed(of))

		for i, field := range of.Fields {
			name := at + "." + strconv.Itoa(i)

			sum.AddString(name, field.Name)
			sum.AddString(name+" type", field.Type.String())
			sum.AddString(name+" embedded", strconv.FormatBool(field.Embedded))
			sum.AddString(name+" exported", strconv.FormatBool(field.Exported))

			for j, tag := range field.Tags {
				sum.AddString(name+" tag "+strconv.Itoa(j), tag.Key+":"+tag.String())
			}
			for _, iface := range field.Implements {
				sum.AddString(name+" implements", iface.String())
			}
		}
	}

	record(held)
	for _, reached := range held.Closure {
		record(reached)
	}
}

// placed says where a struct stands with respect to the package being written,
// which decides whether a layer attaches a method to it or emits a function
// beside it.
func placed(of *model.Struct) string {
	switch {
	case of.External:
		return "external"
	case of.Instantiated:
		return "instantiated"
	default:
		return "local"
	}
}

// errNoProvider is what asking for a helper nothing in this build provides
// comes back as.
var errNoProvider = errors.New("no helper of that name is provided")
