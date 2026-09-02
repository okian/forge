package resolve

import (
	"go/types"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/model"
)

// Diagnostics this package reports.
var codeLayerArity = diag.Register(1007, "layer takes more than one type argument")

// arityHint says what to write instead of a second type argument.
const arityHint = "every layer takes exactly one type argument, the layer below it; capacities, keys and sort fields are written as //forge: options"

// Declaration is one candidate followed to its end.
//
// It is deliberately less than a [model.Model]: the stack is resolved but its
// layers are not yet identified, and the subject is the type that was written
// rather than a model of its fields. Both are filled in by the stages that can
// do it — the layer registry and the subject model builder — and neither
// belongs to a walk over instantiations.
type Declaration struct {
	// Candidate is the declaration this was resolved from, carrying the name,
	// the package, the form and the position every later stage reports against.
	Candidate discover.Candidate

	// Stack holds the markers the declaration names, outermost first, so that
	// Stack[0] is the layer that determines the public API. It always has at
	// least one entry: a declaration naming no marker is not a request and is
	// never resolved into one.
	//
	// Every entry carries an origin and nothing else. Kinds come from the layer
	// registry, and the default storage a refining layer implies is inserted by
	// the stage that knows the kinds, so nothing here was inferred.
	Stack []model.LayerRef

	// Subject is the innermost type argument, with aliases resolved: the type
	// the stack is specialised to.
	//
	// It is whatever was written there, which is not always a type a subject
	// can be built from. A pointer, a basic type and a type parameter all
	// resolve to exactly themselves, so that the stage which rejects them can
	// say what it rejected rather than reporting that resolution failed.
	Subject types.Type
}

// String returns the declaration as the stack reads, with markers spelled
// unqualified however the author imported them and the subject spelled the way
// it was written: "Persons Collection[Ring[Json[Person]]]".
func (d Declaration) String() string {
	above, _, open := model.OpenStack(d.Stack)
	return strings.TrimSpace(d.Candidate.Name + " " + above + model.TypeString(d.Subject) + strings.Repeat("]", open))
}

// Claims reports whether any layer of the run claims this marker.
//
// A predicate rather than the registry, so that following a declaration does
// not need to know what a layer is: what resolution asks is which of the types
// in a nested instantiation are markers, and the answer is whoever claimed
// them.
type Claims func(model.TypeRef) bool

// Declarations resolves every candidate against the markers the run knows,
// preserving their order, which discovery has already made deterministic. It
// returns the candidates that name a stack, together with the diagnostics for
// those that name a broken one.
//
// What counts as a marker is what a layer claims, not what package it came
// from. Forge's own markers are declared together and claimed by the layers it
// ships; a layer somebody added declares its marker in its own package and
// claims that, and a declaration naming it is followed exactly as one naming
// forge's is. Asking the package instead would make the extension point
// unreachable: the type would not be recognised, the walk would end at it, and
// the declaration would be dropped without a word.
//
// A candidate that names no marker is dropped without comment: a defined type
// over a generic type of the author's own is an ordinary Go declaration, and
// forge has nothing to say about it. That holds however deep the instantiation
// goes — a marker written inside such a type is not a stack either, because the
// outermost type is the one that would have to build it.
func Declarations(candidates []discover.Candidate, claims Claims) ([]Declaration, diag.Set) {
	var (
		found []Declaration
		diags diag.Set
	)

	if claims == nil {
		// Nothing claims anything, so nothing is a stack. Answered rather than
		// panicked over: a caller with no registry has asked a question whose
		// answer is none, and a resolver that crashed on it would be worse than
		// one that agreed.
		return nil, diags
	}

	for _, candidate := range candidates {
		if decl, ok := resolve(candidate, claims, &diags); ok {
			found = append(found, decl)
		}
	}

	return found, diags
}

// resolve follows one candidate, reporting what is wrong with it and returning
// whether it named a stack at all.
func resolve(candidate discover.Candidate, claims Claims, diags *diag.Set) (Declaration, bool) {
	if candidate.Pkg == nil || candidate.Pkg.TypesInfo == nil || candidate.Spec == nil {
		return Declaration{}, false
	}

	// The type of the right-hand side, not of the declared type: for a defined
	// type the declared type's own type is its underlying one, from which the
	// instantiation that wrote it cannot be recovered.
	current := candidate.Pkg.TypesInfo.TypeOf(candidate.Spec.Type)
	if current == nil {
		return Declaration{}, false
	}
	current = types.Unalias(current)

	var stack []model.LayerRef
	for {
		named, ok := current.(*types.Named)
		if !ok || !marker(named, claims) {
			break
		}

		args := named.TypeArgs()
		if args.Len() == 0 {
			// A type from the marker package that is not generic is a type like
			// any other. Nothing was applied to anything, so this is where the
			// stack ends rather than a layer written wrong.
			break
		}
		if args.Len() > 1 {
			diags.Add(arity(candidate, stack, named))
			return Declaration{}, false
		}

		stack = append(stack, model.LayerRef{Origin: origin(named)})
		current = types.Unalias(args.At(0))
	}

	if len(stack) == 0 {
		return Declaration{}, false
	}

	return Declaration{Candidate: candidate, Stack: stack, Subject: current}, true
}

// marker reports whether a named type is one a layer of this run claims.
//
// A type with no package is not one: a marker is a declared type, and the ones
// without a package are the predeclared identifiers, which nothing claims.
func marker(named *types.Named, claims Claims) bool {
	if pkg := named.Obj().Pkg(); pkg == nil {
		return false
	}
	return claims(origin(named))
}

// arity builds the diagnostic for a marker written with more than the one type
// argument a layer takes.
func arity(candidate discover.Candidate, stack []model.LayerRef, named *types.Named) diag.Diagnostic {
	text, span := arityLayout(stack, named)

	return diag.New(codeLayerArity, candidate.Pos,
		"layer %s is written with %d type arguments", named.Obj().Name(), named.TypeArgs().Len()).
		WithStack(text, span.Underline(text)).
		WithHint("%s", arityHint)
}

// arityLayout renders the stack down to the offending marker and reports where
// that marker's name falls in the rendering, so the caret can underline it.
//
// The marker is spelled with its type arguments, unlike every entry above it,
// which puts the count the message reports in the line the caret points at.
// Nothing below it is rendered, because the walk stopped there and inventing
// the rest would be inventing the thing the message says is wrong.
func arityLayout(stack []model.LayerRef, named *types.Named) (string, model.Span) {
	above, _, open := model.OpenStack(stack)
	span := model.Span{Offset: len(above), Width: len(named.Obj().Name())}

	return above + model.TypeString(named) + strings.Repeat("]", open), span
}

// origin returns the marker's identity with any instantiation dropped, which is
// what a layer registry is keyed by.
//
// Called by [marker] to ask the registry, and again once the answer is yes, so
// it runs for a type that has not been accepted. What it needs is a package to
// name, and marker has already checked there is one — the nil guard comes
// first for that reason.
func origin(named *types.Named) model.TypeRef {
	obj := named.Obj()
	return model.TypeRef{Pkg: obj.Pkg().Path(), Name: obj.Name()}
}
