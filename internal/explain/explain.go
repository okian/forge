package explain

import (
	"fmt"

	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// pending is what a step reports for work no layer in this build does yet.
//
// Named rather than left blank: a column with nothing in it reads as a layer
// that contributes nothing, which is a different claim and a wrong one.
const pending = "pending"

// Step is one entry in a resolution, in the order resolution reaches it.
type Step struct {
	// Number counts from one at the subject, which is where resolution starts
	// and where a reader following the nesting inward arrives last.
	Number int

	// Name is the subject's type name, or the layer's marker name.
	Name string

	// Kind is what the step contributes. The subject's is [model.KindSubject].
	Kind model.Kind

	// Effect is the one line saying what this step does to the stack. For a
	// layer it is the layer's own summary of itself, so that a layer written
	// outside forge explains itself in its own words rather than in a sentence
	// forge invented for it.
	Effect string

	// Adds names the capabilities this step contributes that the step below it
	// did not have, and Masks those it withdraws. A decorator may do both.
	Adds  []string
	Masks []string

	// Shape names every capability the stack exposes after this step, which is
	// what the step above it is offered and has to accept.
	Shape []string

	// Methods names what this step adds to the declared type's surface, and
	// Withdraws what it takes away. A decorator that cannot uphold a method
	// over what it wraps removes it, which is as much a part of the surface as
	// what it adds.
	Methods   []string
	Withdraws []string

	// Pending records a layer whose generator is not written in this build, so
	// that a step emitting nothing can be told from one whose methods are
	// merely not written yet. Without it the two are one empty list, and only
	// one of them is worth waiting for.
	//
	// Staged narrows that: the marker is declared for a layer forge has not
	// committed to at all, rather than one whose turn has not come.
	Pending bool
	Staged  bool
}

// Resolution is a declaration and the walk that resolves it.
type Resolution struct {
	// Name is the declared type's own name.
	Name string

	// Declaration is the stack as the author wrote it, subject innermost.
	Declaration string

	// Package is the import path the declaration lives in.
	Package string

	// Position is where it was written, as file:line:col.
	Position string

	// Form records whether it was written inline or in a spec file.
	Form model.Form

	// Steps holds the resolution, subject first.
	Steps []Step
}

// Of walks a declaration and reports what each step of it contributes.
//
// The walk runs subject outward, which is the direction resolution runs and the
// reverse of the direction the declaration reads. Every layer's shape is
// decided by the shape beneath it, so there is no other order in which the
// answers exist.
//
// A subject that could not be modelled still resolves: the stack above it is
// what the author wrote and is worth reading, and the step that would have
// described the subject says so rather than the whole answer disappearing. That
// is the case somebody explaining a declaration is most likely to be in.
func Of(decl Declaration, registry *layer.Registry) Resolution {
	if registry == nil {
		// A walk with no catalog can still report the stack that was written,
		// which is more than half of what was asked and is what a caller
		// holding no registry has.
		registry = layer.New()
	}

	out := Resolution{
		Name:        decl.Name,
		Declaration: decl.Layout.Text,
		Package:     decl.Package,
		Position:    decl.Position,
		Form:        decl.Form,
	}

	below := shape.Shape{}
	if decl.Subject != nil {
		below = shape.Subject(decl.Subject)
	}

	out.Steps = append(out.Steps, subjectStep(decl, below))

	// What each layer declares onto, worked out before the walk because it can
	// only be worked out in the other direction: an enclosing decorator moves
	// what is beneath it onto a type of its own, and a layer names what it
	// emits after that. A description that skipped this would report method
	// names the run will not write.
	names := layer.Declaring(decl.Name, claiming(decl.Stack, registry))

	// Outermost first is how the stack is stored, because that is how it was
	// written; the walk needs the other end first.
	for i := len(decl.Stack) - 1; i >= 0; i-- {
		step, above := layerStep(len(out.Steps)+1, decl.Stack[i], decl, below, registry, names[i])
		out.Steps = append(out.Steps, step)
		below = above
	}

	return out
}

// claiming returns the layer claiming each entry of a stack, with nothing where
// nothing claims one.
//
// A hole rather than a shorter list, because what the names are worked out from
// is positional: a marker nothing claims still occupies a place in the stack,
// and dropping it would move every layer beneath it up one.
func claiming(stack []model.LayerRef, registry *layer.Registry) []layer.Layer {
	out := make([]layer.Layer, len(stack))
	for i, ref := range stack {
		if found, claims := registry.Lookup(ref.Origin); claims {
			out[i] = found
		}
	}
	return out
}

// subjectStep describes the type the stack is specialised to.
func subjectStep(decl Declaration, exposed shape.Shape) Step {
	step := Step{
		Number: 1,
		Name:   decl.SubjectName,
		Kind:   model.KindSubject,
		Adds:   names(exposed.Caps),
		Shape:  names(exposed.Caps),
	}

	if decl.Subject == nil {
		step.Effect = "no model: the subject was refused"
		return step
	}

	step.Effect = fmt.Sprintf("struct model: %s, %s",
		count(len(decl.Subject.Fields), "field"), count(tagged(decl.Subject), "tag"))
	return step
}

// layerStep describes one layer and the shape it leaves for the layer above it.
func layerStep(
	number int, ref model.LayerRef, decl Declaration, below shape.Shape,
	registry *layer.Registry, declared string,
) (Step, shape.Shape) {
	step := Step{Number: number, Name: ref.Origin.Name, Kind: model.KindInvalid, Shape: names(below.Caps)}

	found, ok := registry.Lookup(ref.Origin)
	if !ok {
		// A marker nothing claims is not a layer, and reporting it as one with
		// no effect would be a stack that explains cleanly and cannot generate.
		step.Effect = "no layer in this build claims this marker"
		return step, below
	}

	step.Kind = found.Kind()
	step.Effect = summary(found)
	step.Pending, step.Staged = unwritten(found)

	// What a layer would be given decides whether it is given anything. A layer
	// that cannot sit on the shape beneath it is never asked what it exposes, so
	// describing it as though it had been would put a row in the table saying it
	// adds a capability beside a diagnostic saying it is not there — and a reader
	// would have to know which of the two to believe.
	if asked, refuses := accepts(found, below); !asked || refuses != nil {
		// The layer's own words, unless the layer is notional. A marker declared
		// so that a declaration naming it type-checks has one thing worth
		// reporting and it is not that its inputs were wrong. A layer whose
		// generator is merely unwritten is a different case: its composition
		// rules are in place, so the refusal is a fact it implemented and is
		// the one the diagnostic beside it points at.
		if !step.Staged {
			step.Effect = refusal(refuses)
		}
		return step, below
	}

	above, ok := shaped(found, layer.ContextFor(decl.Model, ref).Declaring(declared), below)
	if !ok {
		// A layer that cannot say what it exposes has said something about
		// itself worth printing, and the layers above it are described against
		// the shape it was given rather than against nothing.
		step.Effect = "the layer could not say what it exposes"
		return step, below
	}

	step.Adds = names(above.Caps.Without(below.Caps.All()...))
	step.Masks = names(below.Caps.Without(above.Caps.All()...))
	step.Shape = names(above.Caps)

	step.Methods, step.Withdraws = surface(above, below)

	return step, above
}

// shaped asks a layer what it exposes, surviving one that cannot answer.
//
// A report that fails with a stack trace is worse than one that says a layer
// misbehaved: the reader learns nothing about the other four layers, and the
// one thing they do learn is in a form only forge's authors can read. Layers
// are the part of this a third party writes, so the one that panics is the one
// forge did not.
func shaped(l layer.Layer, ctx *layer.Context, below shape.Shape) (above shape.Shape, ok bool) {
	defer func() {
		if recover() != nil {
			above, ok = below, false
		}
	}()

	return l.Shape(ctx, below), true
}

// accepts asks a layer whether it can sit on the shape beneath it, reporting
// whether it answered at all and what it said.
//
// The same guard [shaped] has, for the same reason: a layer is the part of this
// a third party writes, and a report that ended in a stack trace would tell a
// reader nothing about the other four layers.
func accepts(l layer.Layer, below shape.Shape) (asked bool, refuses error) {
	defer func() {
		if recover() != nil {
			asked, refuses = false, nil
		}
	}()

	return true, l.Accepts(below)
}

// refusal is what a step says about itself when it cannot sit where it was
// written.
//
// The layer's own words where it gave them. It knows what it needed and what it
// was offered and wrote a sentence saying so; a sentence invented here would be
// a worse one that also had to be kept in step.
func refusal(refuses error) string {
	if refuses == nil {
		return "the layer could not say whether it can sit here"
	}
	return refuses.Error()
}

// summary is what a layer says it is for, or what forge says about a layer that
// says nothing.
func summary(l layer.Layer) string {
	described, ok := l.(layer.Described)
	if !ok {
		return pending
	}

	doc := described.Doc()
	if doc == "" {
		doc = pending
	}
	if described.Stage() == layer.StageStaged {
		return doc + " (not in this release)"
	}
	return doc
}

// unwritten reports whether a layer's generator is absent from this build, and
// whether the marker is one forge has not committed to at all.
//
// A layer that says nothing about itself is taken to be written, since a layer
// from outside forge has no answer to a question about forge's roadmap and
// answering it on their behalf would be forge inventing one.
func unwritten(l layer.Layer) (pending, staged bool) {
	described, ok := l.(layer.Described)
	if !ok {
		return false, false
	}

	stage := described.Stage()
	return stage != layer.StageReady, stage == layer.StageStaged
}

// surface names what a layer puts on the declared type's methods and what it
// takes away.
//
// By name and by owner, not by counting. A count would report a decorator that
// wrapped every method beneath it as a layer that did nothing, since the surface
// is the same length; names alone would do the same, since a wrapper and what it
// wraps share one. What tells them apart is who owns the method afterwards — a
// wrapped method belongs to the layer that wrapped it — and a step that emits
// the wrapper is a step that emits a method, which is what a reader is being
// shown.
func surface(above, below shape.Shape) (added, withdrawn []string) {
	held := make(map[string]shape.Method, len(below.Surface))
	for _, method := range below.Surface {
		held[method.Name] = method
	}

	now := make(map[string]bool, len(above.Surface))
	for _, method := range above.Surface {
		now[method.Name] = true

		was, there := held[method.Name]
		if !there || was.Owner != method.Owner {
			added = append(added, method.Name)
		}
	}

	for _, method := range below.Surface {
		if !now[method.Name] {
			withdrawn = append(withdrawn, method.Name)
		}
	}
	return added, withdrawn
}

// names renders a capability set as the words a reader knows them by.
func names(caps shape.CapSet) []string {
	all := caps.All()
	if len(all) == 0 {
		return nil
	}

	out := make([]string, len(all))
	for i, c := range all {
		out[i] = c.String()
	}
	return out
}

// tagged counts the fields carrying at least one tag, which is the number that
// says whether a codec has anything to work from.
func tagged(s *model.Struct) int {
	found := 0
	for _, field := range s.Fields {
		if len(field.Tags) > 0 {
			found++
		}
	}
	return found
}

// count renders a number with its noun, pluralised.
func count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
