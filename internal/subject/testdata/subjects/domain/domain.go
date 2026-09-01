// Package domain holds the subjects a model has to be built from.
package domain

import (
	"time"

	"subjectsfixture/other"
)

// Person is the ordinary case: exported fields, tags, a struct field, a field
// whose type is declared elsewhere, and one field nothing outside this package
// can read.
type Person struct {
	Name    string  `json:"name" db:"full_name"`
	Age     int     `json:"age,omitzero" validate:"required,min=0"`
	Address Address `json:"address"`
	Since   time.Time
	secret  string
}

// Address is reached from Person and needs a model of its own.
type Address struct {
	City string `json:"city"`
	Unit *Unit  `json:"unit,omitzero"`
}

// Unit is reached from Address, so it is two steps from Person.
type Unit struct {
	Number int `json:"number"`
}

// Contact embeds a struct and a pointer to one, so both spellings of promotion
// are covered.
type Contact struct {
	Person
	*Address
	Preferred bool
}

// Node reaches itself through a pointer, which is how every tree and every
// linked list is written.
type Node struct {
	Value    string
	Parent   *Node
	Children []*Node
}

// Ring reaches Spoke, which reaches Ring, so neither is cyclic on its own and
// both are cyclic together.
type Ring struct {
	Spokes []Spoke
}

// Spoke closes the cycle Ring opens.
type Spoke struct {
	Hub *Ring
}

// Composite carries one field of every shape the classifier distinguishes, so
// that the classification is exercised on a type an author could really write.
type Composite struct {
	Basic    string
	Named    Unit
	Pointer  *Unit
	Slice    []Unit
	Array    [3]Unit
	Map      map[string]Unit
	Struct   struct{ Inner Unit }
	Iface    error
	Anything any
	Chan     chan Unit
	Func     func(Unit) error
}

// Pair is a generic type, instantiated below.
type Pair[K comparable, V any] struct {
	Key   K
	Value V
}

// Wrapping is a generic type whose argument can hide a type parameter one level
// down, where a check that only looks at the argument itself would miss it.
type Wrapping[T any] struct {
	Held T
}

// Keyed holds an instantiation of a generic, which Go cannot attach a method
// to and which is still a type the model has to describe.
type Keyed struct {
	Entry Pair[string, Unit]
}

// External holds a type declared outside this module, which no method may be
// attached to from here.
type External struct {
	When  time.Time
	Where other.Place
}

// Celsius is a named type over something that is not a struct. It is a subject
// like any other; it simply has no fields.
type Celsius float64

// Tagged carries a struct tag that will not parse, which is the author's
// mistake and has to be reported at the field rather than swallowed.
type Tagged struct {
	Broken string `json:"a,"`
}

// Named has a String method, so a type built from it records whichever
// interface asks for one.
type Named struct {
	Label string
}

// String returns the label.
func (n Named) String() string { return n.Label }

// Pointered has a String method on its pointer, which is not in the value's
// method set and is still a method the author wrote.
type Pointered struct {
	Label string
}

// String returns the label.
func (p *Pointered) String() string { return p.Label }

// Holder reaches Named and Pointered without being either.
type Holder struct {
	One Named
	Two Pointered
}

// Shapes holds one field per way a type can be written around a name, so that
// what is recorded about the field's own type can be told apart from what is
// recorded about the type inside it.
type Shapes struct {
	Value   Named
	Ptr     *Named
	Slice   []Named
	Foreign *other.Place
}

// Hidden holds one struct behind an exported field and another behind an
// unexported one, which generated code outside this module could not read.
type Hidden struct {
	Shown  Unit
	hidden Address
}

// Labels is a name over something that is not a struct, and that something
// mentions the name again. Looking through it rather than following it is
// correct and has to stop somewhere.
type Labels []Labels

// Registry reaches nothing at all through a field that could be followed
// forever.
type Registry struct {
	All  Labels
	Unit Unit
}

// Åéîõü is named outside ASCII, which Go allows and which is where a span
// measured in bytes and a caret drawn in characters part company.
type Åéîõü struct {
	Name string
}

// Annotated carries forge directives above its fields, which is where a
// field-scoped option is written. What they say is nothing to do with this
// stage; that they arrive attached to the right field is.
type Annotated struct {
	// Plain has nothing written above it beyond this sentence.
	Plain string

	// Blob is documented, and carries an option.
	//
	//forge:json fallback=stdlib
	Blob any

	//forge:json fallback=stdlib
	//forge:validate rule=nonzero
	Two string

	// Paired share a comment, so both fields carry what it says.
	//
	//forge:json fallback=stdlib
	First, Second string

	//forge:json fallback=stdlib
	Unit

	// Embedded through a pointer and from another package, which are the two
	// ways an embedded field's name is not where the field begins.
	//
	//forge:json fallback=stdlib
	*Address

	//forge:json fallback=stdlib
	other.Place
}
