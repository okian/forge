package compose

import (
	"fmt"
	"go/token"
	"slices"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// What can be wrong with a stack.
//
// The numbers follow the rules they enforce, so that a code and the rule it
// came from are the same number and a reader of one finds the other. The
// exception is at the top of the range, for what is wrong with a stack without
// any rule having been broken: a marker nothing claims is not a layer, and
// there is no rule about how it composes because there is nothing to compose.
var (
	codeUnclaimed  = diag.Register(1900, "no layer claims this marker")
	codeMisbehaved = diag.Register(1901, "layer failed while being composed")
	codeRefused    = diag.Register(1006, "layer cannot sit on the stack beneath it")
)

// reportHint says what an author can do about a layer that misbehaved, which is
// nothing except say so.
const reportHint = "this is a fault in the layer rather than in the declaration; report it with the declaration that produced it"

// asked and exposed put the two questions a layer is asked while composing, and
// survive one that answers with a panic.
//
// A layer is the part of this a third party writes, and a run that ends in a
// stack trace tells an author their generator is broken in a form only its
// authors can read — where a diagnostic names the layer that did it and the
// declaration they were working on. The one that panics is the one forge did
// not write, which is exactly why this is here.
//
// What panicked is reported along with what it said, since a nil dereference
// and a deliberate refusal read alike as text and are the first thing whoever
// receives the report has to tell apart. The stack is not: a diagnostic is one
// line, and there is nowhere else for it to go until a verb has somewhere to
// put detail.
func asked(one layer.Layer, below shape.Shape) (refuses, panicked error) {
	defer func() {
		if caught := recover(); caught != nil {
			refuses, panicked = nil, fmt.Errorf("%T: %v", caught, caught)
		}
	}()

	return one.Accepts(below), nil
}

func exposed(one layer.Layer, ctx *layer.Context, below shape.Shape) (above shape.Shape, panicked error) {
	defer func() {
		if caught := recover(); caught != nil {
			above, panicked = below, fmt.Errorf("%T: %v", caught, caught)
		}
	}()

	return one.Shape(ctx, below), nil
}

// Catalog is what composing needs to know about the layers a build ships.
//
// Two things rather than one, because the second cannot be derived from the
// first: a registry says what claims each marker, and nothing in it says which
// of them a refining layer gets when it was written over none. That is a fact
// about forge's own catalog, and this package is written to hold for a layer
// from outside it — so it is told rather than knowing.
type Catalog struct {
	// Registry holds the layers this build can compose.
	Registry *layer.Registry

	// DefaultStorage is the marker a refining layer written over no storage is
	// composed as though it had named. It may be the zero value, and a
	// refining layer will then be refused by the rule it was written to
	// satisfy rather than silently composed over nothing.
	DefaultStorage model.TypeRef
}

// Declaration is what composing a stack needs to know about it.
type Declaration struct {
	// Stack is the layers the declaration names, outermost first, as
	// resolution found them.
	Stack []model.LayerRef

	// Subject is the model of the type the stack is specialised to.
	Subject *model.Struct

	// Pos is where the declaration was written, which is where every
	// diagnostic about it points.
	Pos token.Position

	// Model is the whole declaration, which is what a layer is handed when it
	// is asked what it exposes.
	//
	// The three fields above are the ones composition itself reads, and they
	// are here rather than taken from the model because composition works
	// without one: a subject that could not be modelled still has a stack, and
	// the rules about that stack are still worth reporting. What needs the
	// model is the layer, and a layer whose surface depends on the declaration
	// reports less of it when there is none.
	Model *model.Model
}

// Step is one entry of a composed stack and what it is handed.
type Step struct {
	// Layer is the stack entry, with Implicit set for one this package filled
	// in rather than the author writing.
	Layer model.LayerRef

	// Below is the shape the layers beneath this one expose, which is what the
	// layer was asked to accept and what it is given to generate against.
	Below shape.Shape
}

// Composed is a stack that holds together.
type Composed struct {
	// Steps are the layers innermost first, which is the order they are
	// generated in: each one is handed what the ones before it built up.
	//
	// Innermost first rather than as written, because that is the only order in
	// which the answers exist. A layer's shape is decided by the shape beneath
	// it, so the stack has to be walked from the subject outward however it was
	// spelled.
	Steps []Step

	// Exposed is the shape the whole stack offers, which is what the outermost
	// layer left.
	Exposed shape.Shape
}

// Stack returns the composed layers outermost first, which is how a
// declaration reads and how everything that renders one wants them.
func (c Composed) Stack() []model.LayerRef {
	out := make([]model.LayerRef, len(c.Steps))
	for i, step := range c.Steps {
		out[len(c.Steps)-1-i] = step.Layer
	}
	return out
}

// Compose works out what each layer of a declaration is handed, filling in what
// the declaration means and does not say, and reports what does not hold
// together.
//
// A layer that refuses the stack beneath it is a declaration that will not
// generate, and the caller reports that — but the steps below the refusal were
// composed correctly, so they come back with it and a report about them is
// still worth having.
//
// A stack whose shape is wrong comes back empty instead. Nothing about it was
// worked out: the shape is what decides what each layer is handed, and half a
// walk over an arrangement that is not a stack describes something the author
// did not write.
func Compose(decl Declaration, cat Catalog) (Composed, diag.Set) {
	var diags diag.Set

	if cat.Registry == nil {
		cat.Registry = layer.New()
	}

	stack, layers, ok := claimed(decl, cat.Registry, &diags)
	if !ok {
		return Composed{}, diags
	}

	stack, layers = defaulted(stack, layers, cat)

	// Drawn against the stack as composed rather than as written, since the two
	// are not the same once a storage layer has been filled in: an entry that
	// was inserted shifts every entry beneath it, and a caret drawn against the
	// other rendering marks the wrong layer or none at all. An inferred entry
	// renders no text and gets a span of no width, so the two renderings read
	// alike and only the offsets differ.
	layout := model.LayoutOf(stack, decl.Subject.Type())

	// The shape of the stack before anything is asked of the layers in it. What
	// a layer needs of the one beneath it has an answer either way, and over a
	// stack that is arranged wrongly the answer is a consequence: a stack with
	// two storage layers has one of them refusing what it was handed, which is
	// true and is a second complaint about one mistake.
	if broken := validate(stack, layers, decl, layout); !broken.Empty() {
		diags.Merge(&broken)
		return Composed{}, diags
	}

	out := Composed{Steps: make([]Step, 0, len(stack))}
	below := shape.Subject(decl.Subject)

	// Innermost first: what a layer exposes depends on what is beneath it, so
	// the walk runs from the subject outward whatever order the stack is in.
	for i := len(stack) - 1; i >= 0; i-- {
		out.Steps = append(out.Steps, Step{Layer: stack[i], Below: below})

		accepts, refused := asked(layers[i], below)
		if refused != nil {
			diags.Add(diag.New(codeMisbehaved, decl.Pos,
				"the %s layer failed while composing: %v", stack[i].Origin.Name, refused).
				WithStack(layout.Text, layout.Underline(i)).
				WithHint("%s", reportHint))
			return out, diags
		}
		if accepts != nil {
			diags.Add(diag.New(codeRefused, decl.Pos, "%s", accepts).
				WithStack(layout.Text, layout.Underline(i)).
				WithHint("%s", "put a layer beneath it that provides what it needs, or drop it"))
			return out, diags
		}

		above, refused := exposed(layers[i], layer.ContextFor(decl.Model, stack[i]), below)
		if refused != nil {
			diags.Add(diag.New(codeMisbehaved, decl.Pos,
				"the %s layer failed while composing: %v", stack[i].Origin.Name, refused).
				WithStack(layout.Text, layout.Underline(i)).
				WithHint("%s", reportHint))
			return out, diags
		}
		below = above
	}

	out.Exposed = below
	return out, diags
}

// claimed looks up every marker in the stack, reporting the ones nothing
// claims, and fills in what kind each of them is.
//
// The kinds come from here because this is where the layers are. Resolution
// produces origins and nothing else — a walk over instantiations has no
// business knowing what a layer means — so a stack arrives with every kind
// unset, and the rules below are written entirely in terms of kinds.
//
// All of them are looked up before any of them is checked, because a marker
// nothing claims is not a layer and there is nothing to ask it: a walk that
// reported the first and stopped would send an author back for each of the
// others in turn.
func claimed(decl Declaration, registry *layer.Registry, diags *diag.Set) ([]model.LayerRef, []layer.Layer, bool) {
	stack := slices.Clone(decl.Stack)
	found := make([]layer.Layer, len(stack))

	// Against the stack as written, which is what it still is at this point:
	// nothing has been filled in, so the two renderings agree.
	layout := model.LayoutOf(stack, decl.Subject.Type())

	ok := true
	for i, ref := range stack {
		one, claims := registry.Lookup(ref.Origin)
		if !claims {
			diags.Add(diag.New(codeUnclaimed, decl.Pos,
				"nothing in this build claims the marker %s", ref.Origin.Name).
				WithStack(layout.Text, layout.Underline(i)).
				WithHint("%s", "check the spelling, or the version of forge this was generated with"))
			ok = false
			continue
		}
		stack[i].Kind = one.Kind()
		found[i] = one
	}

	return stack, found, ok
}

// defaulted fills in the storage a refining layer means and does not say.
//
// A refining layer adds a surface over a representation, and one written with
// no representation beneath it is over the ordinary one — Collection[Person] is
// Collection[Slice[Person]], which is what makes an inline declaration's
// underlying type a real slice rather than a special case.
//
// The entry goes above the element layers, which sit around the subject: a
// storage layer holds elements, and what an element layer attached to the
// subject is still the subject. It is marked as inferred, so that nothing draws
// a caret under a layer nobody wrote.
func defaulted(stack []model.LayerRef, layers []layer.Layer, cat Catalog) ([]model.LayerRef, []layer.Layer) {
	refining, storage := false, false
	for _, ref := range stack {
		switch ref.Kind {
		case model.KindRefining:
			refining = true
		case model.KindStorage:
			storage = true
		default:
		}
	}

	if !refining || storage {
		return stack, layers
	}

	found, claims := cat.Registry.Lookup(cat.DefaultStorage)
	if !claims {
		// A build that named no default storage, or named one nothing claims,
		// cannot fill one in. The refining layer then refuses the shape beneath
		// it and says what it needed, which is a better report than one about
		// forge's own catalog.
		return stack, layers
	}

	at := len(stack)
	for at > 0 && stack[at-1].Kind == model.KindElement {
		at--
	}

	stack = slices.Insert(stack, at,
		model.LayerRef{Origin: cat.DefaultStorage, Kind: found.Kind(), Implicit: true})
	layers = slices.Insert(layers, at, found)

	return stack, layers
}
