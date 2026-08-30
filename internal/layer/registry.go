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
	if existing, ok := r.byOrigin[origin]; ok {
		return fmt.Errorf("marker %s is claimed by %s and by %s", origin, name(existing), name(l))
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

// name spells a layer for an error message, by the marker it claims.
func name(l Layer) string { return l.Origin().String() }

// directiveFor returns the //forge: name a marker answers to.
func directiveFor(origin model.TypeRef) string {
	return model.LayerRef{Origin: origin}.Directive()
}
