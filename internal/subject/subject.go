package subject

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/tags"
)

// Diagnostics this package reports. The first three are one rule seen from
// three sides — a subject is one concrete named type — reported apart because
// the fix for each is different, and a hint that covers all three helps nobody.
var (
	codeSubjectUnnamed      = diag.Register(2001, "subject is not a named type")
	codeSubjectPointer      = diag.Register(2002, "subject is a pointer")
	codeSubjectOpen         = diag.Register(2003, "subject is not fully instantiated")
	codeFieldTagMalformed   = diag.Register(2004, "struct tag is malformed")
	codeSubjectNotAvailable = diag.Register(2005, "subject type is missing")
	codeTypeUnbounded       = diag.Register(2006, "type nests inside itself without end")
)

// maxDepth bounds how deep the reachable set may nest.
//
// Nesting is bounded in any program the compiler accepts: a type reached twice
// is the same type, and the second time it is already built. An instantiation
// cycle breaks that — every level of Recur[[]T] is a different type, so the
// walk finds no end — and the compiler rejects it, which does not help, because
// forge reads packages that do not compile on purpose. The bound is far past
// anything hand-written and is a diagnostic rather than a hang.
const maxDepth = 512

// Interface is one interface a type may already implement.
//
// A layer whose interface is already implemented has an implementation to
// delegate to, which is usually the author overriding forge on purpose. It is
// not the same as a method name being taken — a type can satisfy an interface
// through an embedded field without declaring anything itself, and a method
// declared on it would legally shadow what it embeds. [model.Struct.Methods] is
// what answers that.
type Interface struct {
	// Ref identifies the interface, and is what appears in
	// [model.Struct.Implements] and [model.Field.Implements].
	Ref model.TypeRef

	// Type is the interface itself, which is what the check is made against. An
	// entry carrying none is skipped, so a caller assembling the list from
	// packages it may not have loaded need not filter it first.
	Type *types.Interface
}

// Config describes the module a subject is being built for.
type Config struct {
	// Fset resolves the positions recorded on structs and fields. Without it
	// no field-level diagnostic can point anywhere.
	Fset *token.FileSet

	// Owned holds the import paths of the packages belonging to the module
	// being generated for. A type declared anywhere else is recorded as
	// external, because no method can be attached to it from here.
	//
	// A set rather than a module path to compare prefixes against. Membership
	// looks like a prefix test and is not one: a module with another nested
	// inside it has packages that share its path and belong to somebody else,
	// and counting one of those as the module's own would have an element layer
	// attach a method to a type it does not own — a compile error in generated
	// code rather than a mislabel. The load answers it exactly, and this takes
	// the answer from there.
	//
	// An empty set means nobody said, and everything is taken to be local. It
	// is what a caller with no load has, and the direction that leaves a
	// diagnostic about a method rather than silence about a subject.
	Owned map[string]bool

	// Interfaces are the interfaces to record on every type that already
	// implements one. An empty list records nothing, which is honest: no
	// interface is claimed rather than every interface denied.
	Interfaces []Interface

	// Docs holds the comment written above each struct field, keyed by the
	// position of the field's name, as [load.Session.FieldDocs] returns it.
	//
	// This stage reads types, and a type carries no comments. An option written
	// above a field would be unreadable without this, and the field is where
	// such an option has to be written — on the declaration it would have to
	// name the field in its value, which is a second grammar with a second way
	// to misspell one.
	//
	// An empty map reads no directives from any field. That is what a caller
	// with no load has, and it costs a diagnostic rather than producing a wrong
	// one: a layer sees a field with no options written, which is the ordinary
	// case anyway.
	Docs map[token.Pos]*ast.CommentGroup
}

// Builder builds subject models, sharing the structs it has already built.
//
// Two declarations specialised to the same subject get the same [model.Struct],
// and so do a subject and another subject that reaches it. That is what lets
// generation emit one codec for a type rather than one per declaration that
// happens to mention it, and it is why the builder is a value with a memory
// rather than a function.
//
// A Builder is not safe for concurrent use.
type Builder struct {
	cfg Config

	// built memoises by identity. It is a map because it is only ever looked
	// up; order comes from discovered, so nothing an iteration decides can
	// reach the output.
	built map[model.TypeRef]*model.Struct

	// discovered lists identities in the order they were first reached, which
	// is field order, depth first. Every ordered result is built from it.
	discovered []model.TypeRef

	// reaches records the structs each struct's fields refer to directly, in
	// the order the fields refer to them and without repeats. The closure and
	// the cycle flag are both read off it once the walk is done.
	reaches map[model.TypeRef][]model.TypeRef

	// faults remembers what was wrong with each subject, so that the second
	// declaration over one is told what the first was told.
	//
	// Memoising the model without memoising its faults would make a malformed
	// tag a property of declaration order: the declaration that reached the
	// subject first would be refused and every later one would be handed the
	// cached model and a clean bill. The fault is in the subject, so every
	// declaration that names it has it.
	faults map[model.TypeRef]diag.Set
}

// New returns a builder for one module.
func New(cfg Config) *Builder {
	return &Builder{
		cfg:     cfg,
		built:   make(map[model.TypeRef]*model.Struct),
		reaches: make(map[model.TypeRef][]model.TypeRef),
		faults:  make(map[model.TypeRef]diag.Set),
	}
}

// Build returns the model of the type a stack is specialised to, and the
// diagnostics for a type no subject can be built from.
//
// It returns nil only when the subject itself is one no model can be built
// from. A tag that will not parse, and a type that nests without end, are
// reported alongside a model rather than instead of one: the rest of it is
// still true, and a layer that needs none of the part that failed should still
// run.
func (b *Builder) Build(subject types.Type, at Site) (*model.Struct, diag.Set) {
	run := &run{at: at}

	named, ok := b.named(subject, run)
	if !ok {
		return nil, run.diags
	}

	ref := model.RefOf(named)
	if built, seen := b.built[ref]; seen {
		// Built already, so the walk would find nothing to report this time
		// round. What was wrong with it is still wrong, and every declaration
		// over it has it — so the same answer is given again rather than the
		// second author being handed the cached model and a clean bill.
		//
		// Verbatim, positions included. What is wrong here is wrong in a field
		// or in a type reached from one, and those are in one place whoever
		// named them; moving them to the asking declaration would point every
		// author at a line that holds nothing but the name of the type.
		return built, b.faults[ref]
	}

	// Everything discovered from here is new, and everything discovered before
	// it was linked when it was discovered. A struct's reachable set is fixed
	// the moment it is built — the walk builds the whole subgraph beneath it
	// before returning — so relinking what is already linked would recompute
	// the same answer at the cost of the whole graph, on every declaration.
	fresh := len(b.discovered)

	root := b.structFor(named, 0, run)
	b.link(fresh)

	b.faults[ref] = run.diags

	return root, run.diags
}

// Site is where a subject was named.
//
// The position is the declaration's, not the subject's own: a subject is very
// often somebody else's type in somebody else's file, and the line the author
// of this declaration can edit is the one that named it.
//
// The rendering is separate because the stage that refuses a subject cannot
// produce it. A stack is resolved before a model is built, so the picture is
// known by then — but the builder is handed a type and never sees the stack it
// was written inside, and a diagnostic that only says which type was refused
// leaves the reader to find it among four nested layers.
type Site struct {
	// Pos is where the declaration was written.
	Pos token.Position

	// Layout is the declaration rendered, with the subject's place inside it.
	// Its zero value is a site that carries a position and no picture, which is
	// what a caller with nothing to draw gets.
	//
	// The rendering rather than a caret line already drawn: drawing it is one
	// call, and a caller that had to make it could make it wrong — under the
	// layer beside the subject, or measured in the wrong units — with nothing
	// to compare against.
	Layout model.Layout
}

// At returns a site with a position and no rendering, for a caller that has
// nothing to draw.
func At(pos token.Position) Site { return Site{Pos: pos} }

// run is the state of one Build: where to report, and what has been reported.
type run struct {
	// at is where the declaration being built for was written, and how it
	// reads.
	at Site

	// diags collects what was wrong.
	diags diag.Set
}

// refuse records a diagnostic about the subject itself, drawn beneath the
// declaration that named it.
//
// Only the subject's own refusals go through here. A malformed tag is reported
// against the field that carries it, and underlining the subject for it would
// point at the one part of the declaration that is right.
func (r *run) refuse(d diag.Diagnostic) {
	r.diags.Add(d.WithStack(r.at.Layout.Text, r.at.Layout.Subject.Underline(r.at.Layout.Text)))
}

// named narrows a resolved subject to the one shape a model can be built from,
// reporting what it was instead.
func (b *Builder) named(subject types.Type, run *run) (*types.Named, bool) {
	if subject == nil {
		run.refuse(diag.New(codeSubjectNotAvailable, run.at.Pos, "the declaration names no subject").
			WithHint("%s", "report this declaration; resolution accepted it and left nothing for the subject"))
		return nil, false
	}

	spelled := model.TypeString(subject)

	switch typ := types.Unalias(subject).(type) {
	case *types.Named:
		return b.instantiated(typ, spelled, run)

	case *types.Pointer:
		run.refuse(diag.New(codeSubjectPointer, run.at.Pos, "subject %s is a pointer", spelled).
			WithHint("%s", "write the value type; a pointer's nil and its aliasing would have to be answered for by every layer above it"))

	case *types.TypeParam:
		run.refuse(diag.New(codeSubjectOpen, run.at.Pos, "subject %s is a type parameter", spelled).
			WithHint("%s", openHint))

	case *types.Basic:
		run.refuse(diag.New(codeSubjectUnnamed, run.at.Pos, "subject %s is a predeclared type", spelled).
			WithHint("declare a type for it, as in \"type Celsius %s\", so that generated methods have something to attach to", spelled))

	default:
		run.refuse(diag.New(codeSubjectUnnamed, run.at.Pos, "subject %s is not a named type", spelled).
			WithHint("%s", "declare a type for it, so that generated methods have something to attach to"))
	}

	return nil, false
}

// openHint says what to write instead of a subject that is still generic.
const openHint = "a subject has to be one concrete type before its fields exist to generate from; declare one instantiation per subject"

// tagHint says where the grammar a malformed tag broke comes from.
const tagHint = `a struct tag is a space-separated list of key:"value" pairs, and the json key follows the standard library's grammar exactly`

// instantiated refuses a named type that is still generic.
//
// A generic declaration and an instantiation of one by its own parameters are
// both types whose fields are type parameters, which is to say fields with no
// shape yet. Nothing can be generated from them, and accepting them would
// produce a model whose every field classified as nothing in particular.
func (b *Builder) instantiated(named *types.Named, spelled string, run *run) (*types.Named, bool) {
	args := named.TypeArgs()

	if named.TypeParams().Len() > 0 && args.Len() == 0 {
		run.refuse(diag.New(codeSubjectOpen, run.at.Pos, "subject %s is a generic type", spelled).
			WithHint("%s", openHint))
		return nil, false
	}

	for arg := range args.Types() {
		if holdsTypeParam(arg) {
			run.refuse(diag.New(codeSubjectOpen, run.at.Pos,
				"subject %s is instantiated with a type parameter", spelled).
				WithHint("%s", openHint))
			return nil, false
		}
	}

	return named, true
}

// holdsTypeParam reports whether a type argument still has a type parameter
// somewhere inside it.
//
// Being one is not the only way to carry one. Pair[string, V] is refused by
// anybody looking, and Pair[string, []V] and Pair[string, Wrapping[V]] are the
// same declaration wearing a coat: each still ends in a field whose type is a
// parameter, which is a field with no shape to generate from.
//
// The walk descends only into what a type is made of and into the arguments of
// the types it names, never into their fields, so it always ends: the argument
// list of an argument is strictly smaller than the argument, and a struct
// written in place cannot name itself.
func holdsTypeParam(t types.Type) bool {
	switch typ := types.Unalias(t).(type) {
	case *types.TypeParam:
		return true

	case *types.Named:
		for arg := range typ.TypeArgs().Types() {
			if holdsTypeParam(arg) {
				return true
			}
		}

	case *types.Pointer:
		return holdsTypeParam(typ.Elem())

	case *types.Slice:
		return holdsTypeParam(typ.Elem())

	case *types.Array:
		return holdsTypeParam(typ.Elem())

	case *types.Chan:
		return holdsTypeParam(typ.Elem())

	case *types.Map:
		return holdsTypeParam(typ.Key()) || holdsTypeParam(typ.Elem())

	case *types.Struct:
		for field := range typ.Fields() {
			if holdsTypeParam(field.Type()) {
				return true
			}
		}
	}

	return false
}

// structFor returns the model of one named type, building it the first time it
// is reached and returning the same value every time after.
//
// The model is recorded before its fields are, which is what lets a type that
// reaches itself find the partly built model rather than recurse into it
// forever.
func (b *Builder) structFor(named *types.Named, depth int, run *run) *model.Struct {
	ref := model.RefOf(named)
	if existing, ok := b.built[ref]; ok {
		return existing
	}

	out := &model.Struct{
		Named:        named,
		Implements:   b.implements(named),
		Methods:      methodNames(named),
		External:     b.external(named),
		Instantiated: named.TypeArgs().Len() > 0,
		Pos:          b.position(named.Obj().Pos()),
	}
	b.built[ref] = out
	b.discovered = append(b.discovered, ref)

	if depth >= maxDepth {
		run.diags.Add(diag.New(codeTypeUnbounded, run.at.Pos,
			"type %s nests inside itself without end", model.TypeString(named)).
			WithHint("%s", "this is an instantiation cycle, which the compiler rejects; the package declaring it does not build"))
		return out
	}

	structure, ok := named.Underlying().(*types.Struct)
	if !ok {
		// A named type over something other than a struct — an integer with a
		// constant block, a slice with methods — is a subject like any other.
		// It simply has no fields, and nothing reachable through them.
		return out
	}

	out.Fields = make([]model.Field, 0, structure.NumFields())
	for i := range structure.NumFields() {
		out.Fields = append(out.Fields, b.field(ref, structure.Field(i), structure.Tag(i), out.External, depth, run))
	}

	return out
}

// methodNames returns the names of the methods declared on a type, sorted.
//
// Declared on it, not promoted into it: a method a type embeds is not one
// generated code would collide with, because a method declared on the type
// legally shadows it.
func methodNames(named *types.Named) []string {
	if named.NumMethods() == 0 {
		return nil
	}

	names := make([]string, 0, named.NumMethods())
	for method := range named.Methods() {
		names = append(names, method.Name())
	}
	slices.Sort(names)

	return names
}

// field builds one field's model and records the structs it reaches.
//
// external says whether the struct the field belongs to is one declared outside
// this module, which changes two things. Its unexported fields cannot be read
// by generated code at all, so nothing they reach could be generated for and
// the walk stops at them — which is why time.Time contributes a name and
// nothing else, rather than three structs of another module's internals. And a
// tag in a dependency is not the author's to fix, so what is wrong with one is
// not reported at a file they cannot edit.
func (b *Builder) field(owner model.TypeRef, v *types.Var, tag string, external bool, depth int, run *run) model.Field {
	out := model.Field{
		Name:     v.Name(),
		Type:     classify(v.Type()),
		Embedded: v.Embedded(),
		Exported: v.Exported(),
		Pos:      b.position(v.Pos()),
		External: b.externalType(v.Type()),
	}

	parsed, problems := tags.Parse(tag)
	out.Tags = parsed
	if !external {
		for _, problem := range problems {
			run.diags.Add(diag.New(codeFieldTagMalformed, out.Pos, "field %s has a malformed tag: %s", v.Name(), problem).
				WithHint("%s", tagHint))
		}
	}

	// The field's own type is asked, not the name underneath it: a *Stringy is
	// a fmt.Stringer, and reporting nothing for a field written as one would be
	// a plain wrong answer.
	out.Implements = b.implements(v.Type())

	out.Directives = model.Directives(b.cfg.Fset, b.cfg.Docs[v.Pos()])

	if !external || v.Exported() {
		// A struct that reaches itself records the edge like any other. That
		// edge is the whole of what makes it cyclic, and dropping it here — on
		// the grounds that a struct is not in its own closure — would answer
		// the wrong question. Keeping it out of the closure happens later,
		// where the closure is built.
		for _, reached := range b.walk(v.Type(), nil, depth, run) {
			if !slices.Contains(b.reaches[owner], reached) {
				b.reaches[owner] = append(b.reaches[owner], reached)
			}
		}
	}

	return out
}

// walk builds the model of every named struct a field's type reaches directly,
// and returns their identities in the order they were reached.
//
// Directly means through everything a field's type can be written out of —
// pointers, slices, arrays, maps, channels and a struct written in place — and
// stops at a named struct, which is followed rather than looked through. A
// named struct is where a method can attach and so is where the reachable set
// has its members.
//
// through holds the named types being looked through on the way here. A name
// over something that is not a struct is looked through rather than followed,
// and "type Names []Names" is a legal declaration that would otherwise be
// looked through forever.
func (b *Builder) walk(t types.Type, through []model.TypeRef, depth int, run *run) []model.TypeRef {
	switch typ := types.Unalias(t).(type) {
	case *types.Named:
		// A named type is followed for its own sake when it is a struct, and
		// looked through when it is not: a named slice of structs still reaches
		// those structs.
		if _, ok := typ.Underlying().(*types.Struct); ok {
			b.structFor(typ, depth+1, run)
			return []model.TypeRef{model.RefOf(typ)}
		}

		ref := model.RefOf(typ)
		if slices.Contains(through, ref) {
			return nil
		}
		return b.walk(typ.Underlying(), append(through, ref), depth, run)

	case *types.Pointer:
		return b.walk(typ.Elem(), through, depth, run)

	case *types.Slice:
		return b.walk(typ.Elem(), through, depth, run)

	case *types.Array:
		return b.walk(typ.Elem(), through, depth, run)

	case *types.Chan:
		return b.walk(typ.Elem(), through, depth, run)

	case *types.Map:
		return append(b.walk(typ.Key(), through, depth, run), b.walk(typ.Elem(), through, depth, run)...)

	case *types.Struct:
		// A struct written in place has no name to attach a method to, so it is
		// not a member of the reachable set — but the named types inside it
		// are, and nothing else would find them.
		var reached []model.TypeRef
		for field := range typ.Fields() {
			reached = append(reached, b.walk(field.Type(), through, depth, run)...)
		}
		return reached
	}

	// Everything else — a basic type, an interface, a function — reaches no
	// type that generated code could attach to.
	return nil
}

// link fills in the closure and the cycle flag of every struct built so far.
//
// Both are the same question asked twice, and both are answered here rather
// than during the walk, because during the walk the answer is not known yet: a
// struct's reachable set is not complete until the last field of the last type
// it reaches has been read.
func (b *Builder) link(fresh int) {
	for _, from := range b.discovered[fresh:] {
		reachable := b.reachable(from)

		out := b.built[from]
		out.Cyclic = slices.Contains(reachable, from)
		for _, ref := range reachable {
			if ref != from {
				out.Closure = append(out.Closure, b.built[ref])
			}
		}
	}
}

// reachable returns every identity reachable from one, in the order a
// depth-first walk over the fields finds them, and including the starting
// identity when it reaches itself.
//
// Order comes from the slice and membership from the sets beside it. A linear
// scan would read the same and turn the whole pass quadratic in a graph that is
// only ever going to grow.
func (b *Builder) reachable(from model.TypeRef) []model.TypeRef {
	var (
		found   []model.TypeRef
		isFound = make(map[model.TypeRef]bool)
		seen    = map[model.TypeRef]bool{from: true}
		visit   func(model.TypeRef)
	)

	visit = func(ref model.TypeRef) {
		for _, next := range b.reaches[ref] {
			if !isFound[next] {
				isFound[next] = true
				found = append(found, next)
			}
			if seen[next] {
				continue
			}
			seen[next] = true
			visit(next)
		}
	}

	visit(from)

	return found
}

// implements returns the interfaces a type already satisfies, sorted so that
// two runs record them in the same order.
//
// A pointer to the type is asked as well, because a method with a pointer
// receiver is not in the value's method set and is still an implementation to
// delegate to.
//
// What this answers is whether an implementation exists, not whether generating
// one would collide. Those come apart in both directions: an author who wrote
// half a codec satisfies nothing and would still collide, and a type that
// satisfies an interface through an embedded field has no method of its own,
// which a method declared on it would legally shadow. [model.Struct.Methods]
// answers the collision question.
func (b *Builder) implements(t types.Type) []model.TypeRef {
	var found []model.TypeRef

	for _, iface := range b.cfg.Interfaces {
		if iface.Type == nil {
			continue
		}
		if types.Implements(t, iface.Type) || types.Implements(types.NewPointer(t), iface.Type) {
			found = append(found, iface.Ref)
		}
	}

	slices.SortFunc(found, model.TypeRef.Compare)

	return found
}

// external reports whether a type is declared outside the module being
// generated for, and so is a type no method may be attached to from here.
func (b *Builder) external(named *types.Named) bool {
	pkg := named.Obj().Pkg()
	if pkg == nil {
		// A predeclared type — error, or any of the builtin names. Nothing can
		// be attached to those either, from here or from anywhere.
		return true
	}
	if len(b.cfg.Owned) == 0 {
		return false
	}
	return !b.cfg.Owned[pkg.Path()]
}

// externalType answers the same question for the type of a field, which may be
// written as a pointer to the named type rather than as the named type.
//
// A composite answers no, and means it: nothing can be attached to a slice or a
// map wherever its elements come from, and what those elements are is recorded
// by the closure rather than by a flag on the field.
func (b *Builder) externalType(t types.Type) bool {
	if pointer, ok := types.Unalias(t).(*types.Pointer); ok {
		t = pointer.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	return ok && b.external(named)
}

// position resolves a declaration's position, which is the zero value when the
// builder was given no file set to resolve it against.
func (b *Builder) position(pos token.Pos) token.Position {
	if b.cfg.Fset == nil {
		return token.Position{}
	}
	return b.cfg.Fset.Position(pos)
}
