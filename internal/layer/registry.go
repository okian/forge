package layer

import (
	"fmt"
	"slices"

	"github.com/okian/forge/internal/model"
)

// Registry holds the layers a run knows about, keyed by the marker each claims.
//
// The zero value is not usable; call [New]. A registry is not safe for
// concurrent registration, which costs nothing: registration happens once,
// before anything is loaded.
type Registry struct {
	// byOrigin is the lookup resolution and composition go through. It is a map
	// because it is only ever looked up; order comes from registered.
	byOrigin map[model.TypeRef]Layer

	// registered holds the layers themselves, and gives the scan for a clashing
	// directive an order, so that which of two clashing registrations is
	// reported does not depend on a map.
	registered []Layer
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{byOrigin: make(map[model.TypeRef]Layer)}
}

// Register adds a layer.
//
// It refuses two layers claiming one marker, and it refuses two markers that
// would answer to one directive. The second is the less obvious rule and the
// more important one: options are addressed by the marker's name alone, so two
// markers named Ring in different packages would leave //forge:ring meaning
// either of them. Rejecting the second registration is the only point at which
// that can be caught, since by the time a directive is read the ambiguity looks
// like an ordinary option.
//
// The one marker two layers may claim is one forge published and did not
// implement. A layer that generates takes it over from the placeholder holding
// it, which is what a staged marker is for: see [supersedes].
func (r *Registry) Register(l Layer) error {
	origin := l.Origin()

	if origin.Name == "" {
		return fmt.Errorf("layer %T claims no marker", l)
	}
	// The contract is that a layer claims a generic and not an instantiation.
	// Reducing it here quietly would leave the registry reporting one form and
	// keyed by another, and would hide a layer that thinks it claims
	// Collection[Person] from the declarations that write Collection[Session].
	if origin.Args != "" {
		return fmt.Errorf("layer %T claims the instantiation %s; a layer claims the generic it was made from", l, origin)
	}
	// The composition rules are written entirely in kinds: which may sit where,
	// how many of each, what an inline declaration may hold. A layer that
	// reports none is invisible to all of them, which is worse than being
	// refused — a container that forgot to say it was one is told there is no
	// container beneath the decorator above it, and the complaint names the
	// wrong layer. The zero value is the natural mistake, so it is the one worth
	// refusing here, where the answer is a line in the layer rather than a
	// diagnostic about somebody else's declaration.
	if kind := l.Kind(); !kind.Valid() {
		return fmt.Errorf("layer %T claims marker %s and reports no kind; a layer says where in a stack it may appear",
			l, origin)
	}
	// Everything after //forge: is one word, and what that word means is
	// decided by looking it up — so a word forge answers itself is a word no
	// layer may have. A layer called Skip would otherwise register without
	// complaint and take the directive that turns a claim off, and the two
	// would be told apart by whichever lookup happened first.
	if directive := (model.LayerRef{Origin: origin}).Directive(); model.Reserved(directive) {
		return fmt.Errorf("layer %T claims marker %s, whose directive //forge:%s is one forge answers itself",
			l, origin, directive)
	}

	if existing, ok := r.byOrigin[origin]; ok {
		if !supersedes(l, existing) {
			return fmt.Errorf("marker %s is claimed by %s and by %s", origin, name(existing), name(l))
		}
		r.replace(origin, l)

		return nil
	}

	directive := directiveFor(origin)
	for _, other := range r.registered {
		if directiveFor(other.Origin()) == directive {
			return fmt.Errorf("markers %s and %s both answer to //forge:%s",
				other.Origin(), origin, directive)
		}
	}

	r.byOrigin[origin] = l
	r.registered = append(r.registered, l)

	return nil
}

// MustRegister adds a layer and panics if it cannot.
//
// It is for the layers forge itself ships, whose registration happens at
// initialisation and cannot be recovered from: a binary with two layers
// claiming one marker generates the wrong code, and finding out at the first
// test is better than finding out in someone's repository.
func (r *Registry) MustRegister(layers ...Layer) {
	for _, l := range layers {
		if err := r.Register(l); err != nil {
			panic("layer: " + err.Error())
		}
	}
}

// Lookup returns the layer claiming a marker, and whether one does. The
// reference is reduced to its origin first, so an instantiation finds the layer
// its generic was registered under.
func (r *Registry) Lookup(marker model.TypeRef) (Layer, bool) {
	l, ok := r.byOrigin[marker.Origin()]
	return l, ok
}

// Kind returns the kind the registered layer reports for a marker, and
// [model.KindInvalid] for a marker no layer claims — which is a diagnostic
// somewhere else rather than a failure here.
func (r *Registry) Kind(marker model.TypeRef) model.Kind {
	if l, ok := r.Lookup(marker); ok {
		return l.Kind()
	}
	return model.KindInvalid
}

// Resolve fills in the kind of every entry in a stack, setting
// [model.KindInvalid] for an entry no layer claims.
//
// Resolution produces a stack of origins and nothing else, because a walk over
// instantiations has no business knowing what a layer means. This is where the
// two meet. The registry is the authority, so a kind already written into an
// entry is replaced rather than kept: the alternative is a stack whose kinds
// depend on how many times it has been resolved.
func (r *Registry) Resolve(stack []model.LayerRef) []model.LayerRef {
	out := slices.Clone(stack)
	for i := range out {
		out[i].Kind = r.Kind(out[i].Origin)
	}
	return out
}

// All returns every registered layer, ordered by the marker it claims, so that
// anything printed from a registry reads the same way twice.
func (r *Registry) All() []Layer {
	out := slices.Clone(r.registered)
	slices.SortFunc(out, func(a, b Layer) int {
		return a.Origin().Compare(b.Origin())
	})
	return out
}

// Len returns the number of registered layers.
func (r *Registry) Len() int { return len(r.registered) }

// supersedes reports whether a layer may take a marker over from the one
// holding it.
//
// One case, and it is the case a staged marker exists for. Forge publishes a
// marker before its generator is written so that a declaration naming it
// type-checks and is answered with what is missing rather than with silence —
// and a marker nobody may ever claim would make that publication a permanent
// obstacle instead: the name is taken, the directive is taken, and the layer
// the roadmap promised cannot be written by anybody but forge. So a layer that
// generates takes over from a placeholder that does not.
//
// Only for the same marker. Two markers of one name in two packages both answer
// to one directive whatever either of them has implemented, and that ambiguity
// is refused where it is found: replacing one with the other would leave every
// declaration over the abandoned marker resolving to nothing.
//
// Never between two layers that generate. Which of those an author meant is not
// a question the order of two registration calls should answer, so it is
// refused and the binary's own main is where it gets decided.
func supersedes(taking, held Layer) bool {
	return StageOf(held) != StageReady && StageOf(taking) == StageReady
}

// replace swaps in the layer now claiming a marker, where the one it takes over
// from stood.
//
// A takeover leaves the catalog the size it was, which is the property worth
// keeping: a marker is claimed once, and a registry that grew by one every time
// a placeholder was superseded would report a count nothing in it corresponds
// to and would walk the same marker twice in the scan for a clashing directive.
func (r *Registry) replace(origin model.TypeRef, l Layer) {
	at := slices.IndexFunc(r.registered, func(one Layer) bool { return one.Origin() == origin })
	if at < 0 {
		// Unreachable, and not survivable. The two are written together and
		// cannot come apart: byOrigin is only ever filled beside registered,
		// and a marker in one and absent from the other means this file has
		// been changed wrongly or a layer's Origin is not a constant.
		//
		// Appending instead would leave the catalog holding two entries for one
		// marker, which is precisely the state Len and the scan for a clashing
		// directive are written against — so a defensive path here would break
		// the invariant it is defending.
		panic("layer: " + origin.String() + " is claimed in the index and absent from the catalog")
	}

	r.byOrigin[origin] = l
	r.registered[at] = l
}

// name spells a layer for an error message, by the marker it claims.
func name(l Layer) string { return fmt.Sprintf("%T", l) }

// directiveFor returns the //forge: name a marker answers to.
func directiveFor(origin model.TypeRef) string {
	return model.LayerRef{Origin: origin}.Directive()
}
