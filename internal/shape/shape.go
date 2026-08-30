package shape

import (
	"strconv"
	"strings"

	"github.com/okian/forge/internal/model"
)

// Cap is one capability a stack may expose.
//
// Capabilities are the vocabulary composition is checked in. A layer says what
// it needs and what it adds; nothing says which layers it may sit on, which is
// what keeps the rules from growing with the square of the catalog.
//
// The width is wider than the ten capabilities need. Narrowing it later is a
// breaking change for anything that converted one, and there is no saving to be
// had: a shape is the same size either way, because the field pads into the
// space its neighbours already leave.
type Cap uint32

const (
	// Sized reports its length without walking itself.
	Sized Cap = 1 << iota

	// Ordered has an order that means something, so it can be walked backwards
	// and asked for the element before another.
	Ordered

	// Indexed reaches an element by position.
	Indexed

	// Keyed reaches an element by a declared key, which is what a layer routing
	// or deduplicating by one needs.
	Keyed

	// Structured has fields to generate from, which every layer reading the
	// subject rather than the container needs.
	Structured

	// Encodable turns into bytes and back.
	Encodable

	// Comparable has a stable identity, so it can be a set member or a map key.
	Comparable

	// Streamable is walked one element at a time, without materialising
	// anything in between.
	Streamable

	// Bounded holds at most a declared number of elements.
	Bounded

	// Concurrent is safe to use from more than one goroutine, which the layers
	// that run goroutines against a stack require of what is beneath them.
	Concurrent
)

// caps lists every capability in declaration order, with the spelling that
// appears in diagnostics and in the output of the explain and list commands.
//
// One ordered table rather than a set and a lookup beside it: the order a set
// renders in is the order declared here, so there is no second thing to keep in
// step and nothing an iteration could decide.
var caps = []struct {
	cap  Cap
	name string
}{
	{Sized, "Sized"},
	{Ordered, "Ordered"},
	{Indexed, "Indexed"},
	{Keyed, "Keyed"},
	{Structured, "Structured"},
	{Encodable, "Encodable"},
	{Comparable, "Comparable"},
	{Streamable, "Streamable"},
	{Bounded, "Bounded"},
	{Concurrent, "Concurrent"},
}

// String returns the capability's name.
func (c Cap) String() string {
	for _, known := range caps {
		if known.cap == c {
			return known.name
		}
	}
	return "cap(" + strconv.Itoa(int(c)) + ")"
}

// CapSet is a set of capabilities.
//
// The zero value is the empty set, which is what a layer that adds nothing
// exposes and what a subject on its own has.
type CapSet uint32

// Set returns the set holding exactly these capabilities.
func Set(members ...Cap) CapSet {
	var out CapSet
	for _, member := range members {
		out |= CapSet(member)
	}
	return out
}

// Has reports whether the set holds every one of these capabilities. It reports
// true for none, since a layer that requires nothing is satisfied by anything.
func (s CapSet) Has(members ...Cap) bool {
	return s&Set(members...) == Set(members...)
}

// With returns the set with these capabilities added, which is how a layer
// reports what it contributes.
func (s CapSet) With(members ...Cap) CapSet { return s | Set(members...) }

// Without returns the set with these capabilities taken away.
//
// Withdrawing is a decorator's to do and is not a hypothetical: a lock that
// exposes iteration is broken whether the caller iterates inside it or outside
// it, so it takes iteration away and offers scoped access instead. A capability
// withdrawn here is one the layers above can no longer be written against.
func (s CapSet) Without(members ...Cap) CapSet { return s &^ Set(members...) }

// All returns the capabilities in the set, in the order they are declared, so
// that anything printed from a set reads the same way twice.
//
// A bit no capability is declared for comes last, on its own. It cannot arise
// from anything in this package, and dropping it would render a set that is not
// empty as though it were — which is the shape of mistake a diagnostic is least
// able to survive, since it turns "needs Streamable, and it is Sized" into
// "needs Streamable, and it is".
func (s CapSet) All() []Cap {
	var out []Cap

	rest := s
	for _, known := range caps {
		if s.Has(known.cap) {
			out = append(out, known.cap)
			rest = rest.Without(known.cap)
		}
	}

	for bit := Cap(1); bit != 0 && rest != 0; bit <<= 1 {
		if rest.Has(bit) {
			out = append(out, bit)
			rest = rest.Without(bit)
		}
	}

	return out
}

// Empty reports whether the set holds nothing.
func (s CapSet) Empty() bool { return s == 0 }

// String returns the capabilities in declaration order, separated by commas, or
// a dash for the empty set — which is a thing a table has to print rather than
// leave blank.
func (s CapSet) String() string {
	if s.Empty() {
		return "—"
	}

	names := make([]string, 0, len(caps))
	for _, member := range s.All() {
		names = append(names, member.String())
	}
	return strings.Join(names, ", ")
}

// Method is one method a layer emits on the declared type, as the layers above
// it see it.
//
// The surface is what a decorator wraps and what the merge step checks names
// against, so a method is carried as a name and a rendered signature rather
// than as the syntax that will produce it: neither of those readers has the
// generated file yet.
type Method struct {
	// Name is the method's identifier.
	Name string

	// Signature is the method's parameters and results as they read in source,
	// "(v Person)" or "() iter.Seq[Person]", without the receiver.
	Signature string

	// Owner identifies the layer that emits it, which is what lets two layers
	// wanting one name be told apart — and lets the one that loses the
	// unqualified name be named in the diagnostic rather than merely counted.
	Owner model.TypeRef

	// Doc is the one-line summary the explain command prints beside it.
	Doc string
}

// String returns the method as it reads in a declaration, "All() iter.Seq[T]".
func (m Method) String() string { return m.Name + m.Signature }

// Shape is what one layer exposes to the layer above it.
type Shape struct {
	// Caps holds the capabilities the stack up to and including this layer has.
	Caps CapSet

	// Elem identifies the element type the layers above see. It does not change
	// as element layers are applied: those attach capabilities to the subject
	// rather than replacing it.
	Elem model.TypeRef

	// Surface holds the methods on the declared type emitted by this layer and
	// the ones beneath it, which is what a decorator wraps and what collision
	// detection reads.
	//
	// Methods on the *subject* are not in it. An element layer attaches to the
	// subject rather than to the container, so a decorator told to wrap
	// everything beneath it would generate a method with the wrong receiver for
	// a type it does not own. What an element layer contributes reaches the
	// layers above as a capability instead, which is exactly as much as they
	// need to know.
	Surface []Method
}

// Subject returns the shape a subject offers before any layer is applied.
//
// It is where two capabilities come from that no layer adds. A subject with
// fields is structured, which is what every layer reading the subject rather
// than the container requires, and there is nowhere else for that to be
// established: a stack of layers over nothing is not structured, however deep
// it is.
func Subject(subject *model.Struct) Shape {
	if subject == nil {
		return Shape{}
	}

	out := Shape{Elem: subject.Ref()}
	if len(subject.Fields) > 0 {
		out.Caps = out.Caps.With(Structured)
	}
	return out
}

// WithMethods returns the shape with these methods added to its surface.
//
// It copies rather than appending in place, because a shape travels up a stack
// by value and a caller holding an intermediate one — which is exactly what a
// per-step explain does — would otherwise watch its copy grow methods that
// belong to a layer above it.
func (s Shape) WithMethods(methods ...Method) Shape {
	if len(methods) == 0 {
		return s
	}

	surface := make([]Method, 0, len(s.Surface)+len(methods))
	surface = append(surface, s.Surface...)
	surface = append(surface, methods...)
	s.Surface = surface

	return s
}

// Method returns the method in the surface under this name, and whether the
// shape has one.
func (s Shape) Method(name string) (Method, bool) {
	for _, method := range s.Surface {
		if method.Name == name {
			return method, true
		}
	}
	return Method{}, false
}

// Names returns the names in the surface, in the order they were added.
func (s Shape) Names() []string {
	out := make([]string, len(s.Surface))
	for i, method := range s.Surface {
		out[i] = method.Name
	}
	return out
}
