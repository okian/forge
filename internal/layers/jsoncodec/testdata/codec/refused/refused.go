// Package refused holds the subjects a codec cannot be written for.
//
// Every one of them is valid Go that compiles: what is wrong with them is not
// that they are malformed but that a static codec cannot be written for them,
// which is a different thing and has to be reported differently.
package refused

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"
	"time"
)

// Opaque holds a field whose type is decided at run time, which is the case a
// codec written when the code is written cannot see through.
type Opaque struct {
	Name     string
	Anything any
}

// Interfaced holds a named interface rather than the unnamed one, which is the
// same problem wearing a name.
type Interfaced struct {
	Reader interface{ Read([]byte) (int, error) }
}

// Foreign holds a struct from another module, whose unexported fields generated
// code cannot read — so a codec written member by member would write an empty
// object rather than the text it holds. A foreign type carrying its own codec,
// like time.Time, is called through it instead and is not this refusal.
type Foreign struct {
	Text strings.Builder
}

// Channelled holds something JSON has no form for at all.
type Channelled struct {
	Updates chan int
}

// Keyed is a map a JSON object member cannot be named by.
type Keyed struct {
	Lookup map[int]string
}

// Formatted asks for a format this Go release withdrew support for, which
// would otherwise be silently ignored and produce timestamps nobody asked for.
type Formatted struct {
	When string `json:"when,format:RFC3339"`
}

// Insensitive asks for a name to be matched loosely, which generated code
// matches exactly.
type Insensitive struct {
	Name string `json:"name,case:ignore"`
}

// Colliding gives two fields one name on the wire, which is a struct with no
// unambiguous JSON representation.
type Colliding struct {
	First  string `json:"same"`
	Second string `json:"same"`
}

// Misspelled writes an option this layer does not take on a field, which must
// not quietly turn the reflective boundary on or off.
type Misspelled struct {
	//forge:json fallbck=stdlib
	Anything any
}

// Misvalued writes the option this layer does take, with a value it does not.
type Misvalued struct {
	//forge:json fallback=reflect
	Anything any
}

// Celsius is a named type over a basic one, which has members to promote only
// in the sense that it has none.
type Celsius float64

// EmbedsScalar embeds something that is not a struct, so there is nothing to
// promote into the enclosing object.
type EmbedsScalar struct {
	Celsius
	Name string
}

// Bag carries a codec of its own and holds a slice, so it can be neither
// compared nor looked into from here.
type Bag struct {
	Items []int
}

// MarshalJSONTo writes the bag's items.
func (b Bag) MarshalJSONTo(enc *jsontext.Encoder) error {
	return json.MarshalEncode(enc, b.Items)
}

// UnmarshalJSONFrom reads them back.
func (b *Bag) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	return json.UnmarshalDecode(dec, &b.Items)
}

// EmbedsCodec embeds a type that writes an object of its own, whose members are
// therefore not available to be promoted into this one.
type EmbedsCodec struct {
	Bag
	Name string
}

// CannotOmit asks for a member to be left out when it holds its zero value,
// where no test for that can be written: the type brought its own codec, so its
// parts are not this layer's to look at, and it cannot be compared.
type CannotOmit struct {
	Held Bag `json:"held,omitzero"`
}

// Quoted asks for a number to be written inside a JSON string, which this
// layer neither generates nor may quietly ignore: a reader expecting "5" and
// given 5 refuses the document.
type Quoted struct {
	N int64 `json:"n,string"`
}

// Halved declares one half of a codec. A generated pair would redeclare the
// half that is there, in a file its author cannot edit.
type Halved struct {
	Name string
}

// MarshalJSONTo writes the name, and nothing reads it back.
func (h Halved) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(h.Name))
}

// Holds one, so that the refusal is about a field of a subject rather than
// about a subject nothing names.
type Holds struct {
	One Halved
}

// Named is a map key that writes itself, so it cannot also be a member name.
type Named struct {
	Lookup map[Bag2]string
}

// Bag2 is a key type with a codec of its own.
type Bag2 string

// MarshalJSONTo writes the key as an object, which no member name can be.
func (b Bag2) MarshalJSONTo(enc *jsontext.Encoder) error {
	return json.MarshalEncode(enc, string(b))
}

// UnmarshalJSONFrom reads one back.
func (b *Bag2) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	return json.UnmarshalDecode(dec, (*string)(b))
}

// Hidden holds an unexported field of a type nothing can compare, which is not
// written to the wire and is still part of what the struct is.
type Hidden struct {
	Shown  string
	hidden []int
}

// Held asks for a member to be omitted when it is zero, where the member is a
// struct whose zero value neither a comparison nor its written members can
// answer for: the comparison is illegal and the members are not the whole of it.
type Held struct {
	One Hidden `json:"one,omitzero"`
}

// Loop is a pointer to itself, which Go allows and which reaches itself without
// a struct in between.
type Loop *Loop

// Looped asks for one to be omitted when it is empty, which is the same
// question Chain asks and reaches by a different route.
type Looped struct {
	L Loop `json:"l,omitempty"`
}

// Labels is a slice of itself, which reaches itself the same way Loop does and
// through a different composite.
type Labels []Labels

// Labelled holds one, with nothing asked of it beyond being written — so the
// refusal is about the type rather than about an option on the field.
type Labelled struct {
	All Labels
}

// MisspelledOption writes omitempty in a spelling the standard library
// refuses. Ignoring it would write a member the author asked to leave out,
// which is the one kind of wrong a round trip through this same codec cannot
// see: both halves would agree the member belongs there.
type MisspelledOption struct {
	Tags []string `json:"tags,omitEmpty"`
}

// RepeatedOption writes one option twice. The standard library refuses it, and
// a tag that says a thing twice may mean either of two things, so a codec that
// took the first would be choosing on the author's behalf.
type RepeatedOption struct {
	Tags []string `json:"tags,omitempty,omitempty"`
}

// ContradictoryCase asks for a name to be matched both loosely and exactly.
// That is the repeat that reads as a contradiction rather than as a
// duplication, and it is the one the standard library names separately.
type ContradictoryCase struct {
	Name string `json:"name,case:ignore,case:strict"`
}

// Inner is a struct with members to promote, so that what is wrong with the
// two fixtures below is the tag rather than the type.
type Inner struct {
	A int `json:"a"`
}

// NamedEmbed gives a name to a field whose members are promoted, which is a
// name nothing will ever be written under: promotion is what embed means and a
// promoted member carries its own name.
type NamedEmbed struct {
	Inner `json:"wrapper,embed"`
}

// DecoratedEmbed asks for a promoted field to be omitted when it is zero.
// There is no member to omit — the members are the enclosing struct's — so the
// option describes something that cannot happen.
type DecoratedEmbed struct {
	Inner `json:",embed,omitzero"`
}

// TaggedUnexported tags a field generated code cannot read. The tag asks for a
// member the codec will never write, and an author who wrote it is describing
// a wire format they will not get.
type TaggedUnexported struct {
	Exported   int `json:"exported"`
	unexported int `json:"unexported"`
}

// Timed holds a duration with no format asked for. The standard library
// refuses to choose between a count of nanoseconds and a string like "1h30m",
// and a codec that chose quietly would put one of them on a wire the other end
// reads the other way.
type Timed struct {
	For time.Duration `json:"for"`
}

// NoMembers has fields, and none of them can be written. A codec for it would
// be a function that writes {} — which round-trips through itself and so is
// invisible to any test that only reads back what it wrote.
type NoMembers struct {
	hidden  int
	private string
}
