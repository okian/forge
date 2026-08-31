package generate

import (
	"errors"
	"fmt"
	"go/token"
	"slices"
	"strconv"

	"github.com/okian/forge/internal/compose"
	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/load"
	"github.com/okian/forge/internal/merge"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/options"
	"github.com/okian/forge/internal/shape"
	"github.com/okian/forge/internal/shared/seq"
)

// What can go wrong between a declaration that resolved and a file that could
// be written.
var (
	codeFileCollision = diag.Register(4006, "two declarations want one file")
	codeNoProvider    = diag.Register(4007, "nothing provides a helper a layer requires")
	codeLayerFailed   = diag.Register(4008, "layer could not generate")
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
}

// File is one file generation would write.
type File struct {
	// Name is what it is called, with no directory: every file of a package
	// sits beside the source it was generated from.
	Name string

	// Content is what goes in it, formatted.
	Content []byte

	// Decl names the declaration it was generated for, and Pos where that
	// declaration was written. A file with neither is one several declarations
	// share.
	Decl string
	Pos  token.Position
}

// Package generates the files one package's declarations ask for.
//
// The path and the name are both needed and are different things. The name goes
// in the package clause of every file written; the path is what the helpers a
// layer required are identified by, since a helper is emitted into a package
// rather than imported from one, and two packages of one name hold two of them.
//
// Everything is generated before anything is reported as done, because a
// package is written whole or not at all: a run that wrote three files and then
// found the fourth declaration wrong would leave a package holding an answer to
// a question nobody can see any more.
func Package(path, name string, requests []Request, cfg Config) ([]File, diag.Set) {
	var diags diag.Set

	var (
		out      []File
		required []model.TypeRef
		standing []emit.Section
		imported []emit.Import
		taken    = make(map[string]string, len(requests)+len(Reserved()))
	)

	// Spoken for before anything is generated, so that a declaration named
	// after one of them collides rather than overwrites.
	for _, held := range Reserved() {
		taken[held] = ""
	}

	for _, req := range requests {
		if req.Model == nil {
			continue
		}

		file, unit, ok := one(req, name, cfg, taken, &diags)
		if !ok {
			continue
		}

		out = append(out, file)
		required = append(required, unit.Requires...)

		// Only a declaration whose output the tag excludes needs standing in
		// for. An inline declaration's file carries no constraint, so it is in
		// every build already, and a second copy of its methods would collide
		// with the first.
		if req.Model.Form == model.FormSpec {
			standing = append(standing, stubs(req.Model, unit)...)
			imported = append(imported, unit.Imports...)
		}
	}

	if len(standing) > 0 {
		var sum emit.Digest
		FingerprintStubs(&sum, requests, name, cfg)

		content, err := renderStubs(name, standing, imported, cfg, &sum)
		if err != nil {
			diags.AddError(err)
		} else {
			out = append(out, File{Name: Stubs(), Content: content})
		}
	}

	held, problems := helpers(path, required, requests)
	diags.Merge(&problems)

	if !held.Empty() {
		var sum emit.Digest
		FingerprintShared(&sum, required, name, cfg)

		content, err := render(nil, name, held, cfg, &sum)
		if err != nil {
			diags.AddError(err)
		} else {
			out = append(out, File{Name: Shared(), Content: content})
		}
	}

	return out, diags
}

// one generates the file a single declaration asks for, and hands back what it
// composed to so that the package can see what several declarations share.
//
// The names already taken are read and written, because two declarations
// wanting one file is a thing only the package can see. Anything reported is
// added to the run's own set rather than returned, so that a declaration
// refused here does not need a second path out.
func one(req Request, name string, cfg Config, taken map[string]string, diags *diag.Set) (File, merge.Unit, bool) {
	into := Named(req.Model.Name)
	if first, twice := taken[into]; twice {
		diags.Add(collision(req.Model, into, first))
		return File{}, merge.Unit{}, false
	}
	taken[into] = req.Model.Name

	// Taken before anything composes, because composing fills in what a
	// declaration meant and did not say — and a fingerprint of the declaration
	// as forge understood it, rather than as it was written, would be one this
	// run could produce and the next could not.
	var sum emit.Digest
	Fingerprint(&sum, req, name, cfg)

	unit, problems := declaration(req, cfg)
	diags.Merge(&problems)

	if !problems.Empty() {
		return File{}, merge.Unit{}, false
	}

	content, err := render(req.Model, name, unit, cfg, &sum)
	if err != nil {
		diags.AddError(err)
		return File{}, merge.Unit{}, false
	}

	return File{Name: into, Content: content, Decl: req.Model.Name, Pos: req.Model.Pos}, unit, true
}

// collision reports a declaration whose file another declaration is already
// being written to, or which the package keeps for itself.
//
// The two are the same failure and read differently. A declaration colliding
// with another names the other, and the fix is to rename either. A declaration
// colliding with a file the package writes for itself has nothing to be told
// about except the name, since what it collided with is not something anybody
// wrote — and it is the one the author is least likely to guess, because the
// name looks ordinary and the file it wants has never been mentioned.
func collision(held *model.Model, into, first string) diag.Diagnostic {
	if first == "" {
		return diag.New(codeFileCollision, held.Pos,
			"%s would be written to %s, which the package writes for itself", held.Name, into).
			WithHint("%s", "a package writes one file for what its declarations share and one for "+
				"what a build with the tag needs; rename the declaration")
	}

	return diag.New(codeFileCollision, held.Pos,
		"%s and %s are both written to %s", first, held.Name, into).
		WithHint("%s", "a file is named after the declaration in lower case, so two names that "+
			"differ only in case, or only by what is added to a name the build reads, "+
			"want one file; rename one of them")
}

// declaration generates for one declaration: what its options mean, what its
// stack composes to, and what each of its layers contributes.
func declaration(req Request, cfg Config) (merge.Unit, diag.Set) {
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
		return merge.Unit{}, diags
	}

	// The stack composition arrived at, which is what the layers were asked
	// against and what the declaration means. It is not always what was
	// written: a refining layer over no storage has one filled in.
	held.Stack = composed.Stack()

	units := make([]layer.Unit, 0, len(composed.Steps))

	// Outermost first in the file, innermost first in the walk: a layer is
	// generated against what is beneath it, and read above what is above it.
	for i := len(composed.Steps) - 1; i >= 0; i-- {
		step := composed.Steps[i]

		found, claims := cfg.Catalog.Registry.Lookup(step.Layer.Origin)
		if !claims {
			continue
		}

		unit, err := generated(found, layer.ContextFor(held, step.Layer), step.Below)
		if err != nil {
			diags.Add(refusal(err, held, step.Layer))
			continue
		}

		units = append(units, unit)
	}

	if !diags.Empty() {
		return merge.Unit{}, diags
	}
	return merge.Units(units...), diags
}

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
// One helper, because there is one: the shared view every query surface hands
// its results to. A second would want a registry, and a registry with one entry
// is a lookup written twice.
func provided(ref model.TypeRef, pkg string) (layer.Unit, error) {
	if ref == seq.Ref(pkg) {
		return seq.Unit(token.Position{})
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
func render(held *model.Model, pkg string, unit merge.Unit, cfg Config, sum *emit.Digest) ([]byte, error) {
	file := emit.File{
		Package:  pkg,
		Imports:  unit.Imports,
		Sections: unit.Sections,
		Header: emit.Header{
			Forge:   cfg.Forge,
			Markers: cfg.Markers,
			Inputs:  sum.String(),
		},
	}
	if held != nil {
		file.Decl, file.Pos = held.Name, held.Pos
		file.Build = tagged(held.Form)
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
func renderStubs(pkg string, sections []emit.Section, imports []emit.Import, cfg Config, sum *emit.Digest) ([]byte, error) {
	file := emit.File{
		Package:  pkg,
		Build:    load.SpecTag,
		Imports:  reaching(sections, imports),
		Sections: sections,
		Header: emit.Header{
			Forge:   cfg.Forge,
			Markers: cfg.Markers,
			Inputs:  sum.String(),
		},
	}

	return file.Render()
}

// FingerprintStubs records what the file standing in for a package's output is
// made from.
//
// Exported for the reason the other two are: generating and checking have to
// assemble the same inputs or every check reports staleness for ever.
//
// What the file holds is one declaration's output for every spec declaration in
// the package, so what it is a function of is those declarations — each one
// exactly as its own file is fingerprinted, since the same inputs decide the
// signatures here and the bodies there. Inline declarations are left out
// because nothing of theirs is written here.
func FingerprintStubs(sum *emit.Digest, requests []Request, pkg string, cfg Config) {
	versions(sum, cfg)
	sum.AddString("package name", pkg)

	for _, req := range requests {
		if req.Model == nil || req.Model.Form != model.FormSpec {
			continue
		}
		Fingerprint(sum, req, pkg, cfg)
	}
}

// tagged returns the build constraint a declaration's file carries.
//
// A spec declaration and the file generated for it are two declarations of one
// name, and exactly one of them may be in scope. The spec is written under a
// tag and this is written under its complement, so the ordinary build sees the
// real type and a build with the tag sees the one the author wrote — which is
// what keeps the spec type-checked, and a rename of the subject a compile error
// rather than a stale comment.
//
// An inline declaration carries none. The author's own file holds the type and
// carries no tag, so the methods have to be readable wherever that file is —
// which is everywhere. A constraint of any kind would take them out of some
// build the author already had.
func tagged(form model.Form) string {
	if form == model.FormSpec {
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

// FingerprintShared records what the file a package's declarations share is
// made from, which is the helpers they asked for and nothing else.
//
// It is the same seam as the one above and is exported for the same reason,
// with a caveat worth stating: knowing which helpers were asked for means
// generating every declaration in the package, so this is not the cheap check
// the per-declaration fingerprint is. What it buys is that the shared file's
// header cannot disagree with the file, and that a package which merely gained
// a declaration does not rewrite it.
func FingerprintShared(sum *emit.Digest, required []model.TypeRef, pkg string, cfg Config) {
	versions(sum, cfg)
	sum.AddString("package name", pkg)

	// Sorted and without repeats, because what the file holds is the set of
	// helpers and not how many declarations asked for each. A package that
	// gained a second declaration requiring the same helper writes the same
	// bytes, and a fingerprint that counted would say otherwise.
	held := slices.Clone(required)
	slices.SortFunc(held, model.TypeRef.Compare)

	for _, ref := range slices.Compact(held) {
		sum.AddString("helper", ref.String())
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
