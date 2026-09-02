// Package model holds the subjects a codec is written for.
//
// One struct per question the codec has to answer, rather than one struct with
// everything in it: a failure names the shape it is about, and a shape that
// cannot be generated for at all can be left out of the comparison without
// taking the rest with it.
package model

import (
	"encoding/json/jsontext"
	"errors"
	"strconv"

	"codecfixture/other"
)

// Scalars holds one field of every kind that becomes a single JSON token.
type Scalars struct {
	Bool    bool
	String  string
	Int     int
	Int8    int8
	Int16   int16
	Int32   int32
	Int64   int64
	Uint    uint
	Uint16  uint16
	Uint32  uint32
	Uint64  uint64
	Float32 float32
	Float64 float64
}

// Celsius is a named type over a basic one, which is written as what it is
// underneath and read back as itself.
type Celsius float64

// Named holds fields whose types are defined rather than predeclared.
type Named struct {
	Temp  Celsius
	Label Label
	Count Counter
}

// Label and Counter are named over a string and an integer.
type (
	Label   string
	Counter uint32
)

// Tagged holds the tag grammar: a renamed member, a hidden one, and the two
// ways of leaving one out.
type Tagged struct {
	Renamed string `json:"renamed_here"`
	Hidden  string `json:"-"`
	Zero    int    `json:"zero,omitzero"`
	Empty   []int  `json:"empty,omitempty"`
	Plain   string
}

// Composites holds the shapes that nest.
type Composites struct {
	Strings []string
	Numbers []int
	Nested  [][]string
	Fixed   [3]int
	Lookup  map[string]int
	Deep    map[string][]string
	Pointer *string
	Bytes   []byte
}

// Address is a struct reached from another struct, which gets a codec of its
// own and is called into rather than inlined.
type Address struct {
	City string
	Post string
}

// Nested holds a struct, a pointer to one, and a slice of them.
type Nested struct {
	Home    Address
	Work    *Address
	Visited []Address
}

// Embedded promotes another struct's members into its own object, which is
// what Go embedding means to JSON.
type Embedded struct {
	Address
	Name string
}

// Renamed embeds a struct under a name, which asks for a nested object rather
// than promotion.
type Renamed struct {
	Address `json:"address"`
	Name    string
}

// Behind embeds through a pointer, whose members are written only when it is
// there and which is allocated when one arrives.
type Behind struct {
	*Address
	Name string
}

// Cyclic reaches itself through a pointer, which terminates at run time and has
// to terminate while the codec is being written too.
type Cyclic struct {
	Name string
	Next *Cyclic
}

// Reflective holds a field forge cannot see through, with the boundary marked.
//
// What is written above the field is the whole of what makes this generate at
// all: without it the field is a refusal, and with it the field is handed to the
// reflective encoder and nothing else about the struct changes.
type Reflective struct {
	Name string

	//forge:json fallback=stdlib
	Extra any
}

// Stamp carries a codec of its own, written by hand.
//
// What forge does with one is call it. A hand-written codec is the author
// overriding what would otherwise be generated, and a generated second one would
// both redeclare the methods and disagree with the first about the wire.
type Stamp struct {
	Seconds int64
}

// MarshalJSONTo writes the stamp as the number of seconds.
func (s Stamp) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.Int(s.Seconds))
}

// UnmarshalJSONFrom reads a number of seconds back.
func (s *Stamp) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	token, err := dec.ReadToken()
	if err != nil {
		return err
	}

	held, err := token.Int()
	if err != nil {
		return err
	}

	s.Seconds = held
	return nil
}

// Stamped holds a field whose type brought its own codec.
type Stamped struct {
	At   Stamp
	Also *Stamp
	Name string
}

// Weight carries a codec whose halves are both declared on the pointer, which
// is what somebody writes when the two are written together.
//
// It is the case that decides whether generated code can call the method at
// all: an encoder's receiver is a value, so a field of it is not addressable,
// and a pointer method cannot be called on something with no address.
type Weight struct {
	Grams int64
}

// MarshalJSONTo writes the weight in grams.
func (w *Weight) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.Int(w.Grams))
}

// UnmarshalJSONFrom reads a weight in grams.
func (w *Weight) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	token, err := dec.ReadToken()
	if err != nil {
		return err
	}

	held, err := token.Int()
	if err != nil {
		return err
	}

	w.Grams = held
	return nil
}

// Weighed holds one by value.
type Weighed struct {
	Mass Weight
	Name string
}

// Colour is a named scalar carrying a text codec and nothing else.
//
// What forge does with one is write the text, because that is what the standard
// library does and because the text is what its author said the value means.
// The number underneath is an implementation detail no document can explain.
type Colour int

// The colours, whose names are the whole of what a document carries.
const (
	Red Colour = iota
	Green
)

// MarshalText writes the colour's name.
func (c Colour) MarshalText() ([]byte, error) {
	switch c {
	case Red:
		return []byte("red"), nil
	case Green:
		return []byte("green"), nil
	default:
		return nil, errors.New("no such colour")
	}
}

// UnmarshalText reads a colour's name back, and refuses one nobody declared.
func (c *Colour) UnmarshalText(text []byte) error {
	switch string(text) {
	case "red":
		*c = Red
	case "green":
		*c = Green
	default:
		return errors.New("no such colour: " + string(text))
	}
	return nil
}

// Coloured holds one by value and one behind a pointer.
type Coloured struct {
	Shade  Colour
	Accent *Colour
	Name   string
}

// Appending is a named scalar whose only writing half is the appender.
//
// encoding.TextAppender is the newer of the two and a type may carry it alone.
// Nothing here has a buffer to append into, so the half is taken for the text
// it produces rather than for what it was added to save.
type Appending int

// AppendText writes the value onto the end of a buffer.
func (a Appending) AppendText(b []byte) ([]byte, error) {
	return append(b, "held"...), nil
}

// UnmarshalText reads it back.
func (a *Appending) UnmarshalText(text []byte) error {
	if string(text) != "held" {
		return errors.New("no such value")
	}
	*a = 1
	return nil
}

// Appended holds one.
type Appended struct {
	Held Appending
}

// Older is a named scalar carrying the codec that came before this one, beside
// a text codec.
//
// The standard library asks for the older pair first, so a value of this type
// goes onto the wire as a number wherever it is encoded reflectively. This
// layer reads neither pair and writes neither, so taking the text codec would
// make forge the one reader that disagreed — it is left to the reflective
// encoder instead, which reaches the same method everything else does.
type Older int

// MarshalJSON writes the value as a number, which is what the older codec is.
func (o Older) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Itoa(int(o))), nil
}

// UnmarshalJSON reads a number back.
func (o *Older) UnmarshalJSON(b []byte) error {
	held, err := strconv.Atoi(string(b))
	if err != nil {
		return err
	}
	*o = Older(held)
	return nil
}

// MarshalText writes the value as a name, which nothing asks it for.
func (o Older) MarshalText() ([]byte, error) { return []byte("older"), nil }

// UnmarshalText reads that name back.
func (o *Older) UnmarshalText(text []byte) error {
	if string(text) != "older" {
		return errors.New("no such value")
	}
	*o = 1
	return nil
}

// Aged holds one, which goes out as a number from either side.
type Aged struct {
	Held Older
}

// Odd is a named scalar with two methods named as a text codec's halves are and
// shaped like nothing.
//
// A name says what a package holds a method under and nothing about how it may
// be called. Reading only the name would have this written through a call with
// the wrong number of results, in a file its author cannot edit — a package
// that does not build, from a run that reported nothing wrong. It is written as
// the number it is.
type Odd int

// MarshalText returns one value where a text codec's writer returns two.
func (o Odd) MarshalText() string { return "odd" }

// UnmarshalText takes a string where a text codec's reader takes bytes.
func (o *Odd) UnmarshalText(text string) error {
	_ = text
	return nil
}

// Oddly holds one, and is written as the number underneath it.
type Oddly struct {
	Held Odd
}

// Vary is a named scalar whose text codec takes its bytes one at a time.
//
// A variadic parameter is a slice to the function and one value to every
// caller, so a half taking ...byte reads as taking []byte and is called with a
// single byte. Neither half satisfies the interface its name belongs to, and
// the value is written as the number it is.
//
// Both halves rather than one, because a type with one good half and one bad
// one is a type the standard library writes through the good half and cannot
// read back — and a fixture cannot be held against a library disagreeing with
// itself.
type Vary int

// AppendText takes its buffer one byte at a time, which no caller of a text
// codec does.
func (v Vary) AppendText(b ...byte) ([]byte, error) { return append(b, "vary"...), nil }

// UnmarshalText takes its bytes the same way, so neither half is one.
func (v *Vary) UnmarshalText(text ...byte) error {
	_ = text
	return nil
}

// Varied holds one.
type Varied struct {
	Held Vary
}

// Bytes and Err are aliases for the two types a text codec is spelled with.
//
// An alias is the same type. A codec written against these satisfies the same
// interfaces as one written against what they stand for, and the compiler and
// the standard library both say so — which is why what reads a signature here
// has to see through them.
type (
	Bytes = []byte
	Err   = error
)

// Aliased is a named scalar whose text codec is spelled through those aliases.
type Aliased int

// MarshalText writes it, spelled through the aliases.
func (a Aliased) MarshalText() (Bytes, Err) { return Bytes("aliased"), nil }

// UnmarshalText reads it back, spelled the same way.
func (a *Aliased) UnmarshalText(text Bytes) Err {
	if string(text) != "aliased" {
		return errors.New("no such value")
	}
	*a = 1
	return nil
}

// Naming what it holds, which is a text codec however it is spelled.
type Aliasing struct {
	Held Aliased
}

// Naming holds the field names that tell one naming style from another.
//
// An initialism at either end and one in the middle, because those are where
// the word boundaries are argued over: a run of capitals is one word, and the
// last capital of a run belongs to the next word when a lower-case letter
// follows it.
type Naming struct {
	UserID     int
	JSONValue  string
	HTTPServer string
	ID         int
	Name       string
}

// Hollow renders as an empty object whenever its own members are left out,
// which is the only way a struct can be empty in the JSON sense.
type Hollow struct {
	Text string `json:"text,omitempty"`
	List []int  `json:"list,omitempty"`
}

// Omitting holds a member of every shape that can be left out, asked for both
// ways.
//
// The two ways are defined against different things — omitzero against the Go
// zero value and omitempty against the JSON one — so a number that is zero is
// left out by the first and written by the second. Every shape is here because
// the condition is written per shape: a string compares against the empty
// string, a slice against nil, and a struct against its own zero value only
// when everything in it can be compared at all.
type Omitting struct {
	Text   string         `json:"text,omitempty"`
	Count  int            `json:"count,omitempty"`
	Flag   bool           `json:"flag,omitempty"`
	List   []int          `json:"list,omitempty"`
	Lookup map[string]int `json:"lookup,omitempty"`
	Ptr    *string        `json:"ptr,omitempty"`
	Blob   []byte         `json:"blob,omitempty"`
	Inner  Address        `json:"inner,omitempty"`
	Hollow Hollow         `json:"hollow,omitempty"`

	ZeroText   string     `json:"ztext,omitzero"`
	ZeroFlag   bool       `json:"zflag,omitzero"`
	ZeroFloat  float64    `json:"zfloat,omitzero"`
	ZeroStruct Address    `json:"zstruct,omitzero"`
	ZeroArray  [2]int     `json:"zarray,omitzero"`
	ZeroList   []int      `json:"zlist,omitzero"`
	ZeroPtr    *Address   `json:"zptr,omitzero"`
	ZeroHolder Composites `json:"zholder,omitzero"`
	ZeroFix    [4]byte    `json:"zfix,omitzero"`

	// An array whose elements cannot be compared, and a struct reached through
	// an embedded pointer: the two shapes whose zero test is built out of parts
	// rather than written as one comparison.
	ZeroSlices  [2][]int `json:"zslices,omitzero"`
	ZeroBehind  Behind   `json:"zbehind,omitzero"`
	EmptyBehind Behind   `json:"ebehind,omitempty"`
}

// Pointed holds a pointer to every shape whose read binds a temporary.
//
// A pointer to a number is the shape that decides whether the temporaries a
// decoder binds can shadow the value it is reading into: the target is a local
// there rather than a field selector, so a temporary of the same name compiles
// and assigns the value to itself.
type Pointed struct {
	Big   *int64
	Num   *int
	Small *int8
	Count *uint32
	Ratio *float32
	Warm  *Celsius
	Text  *string
	Flag  *bool
}

// Elsewhere holds types from another package, which a codec names and a file
// therefore has to bind.
type Elsewhere struct {
	Temp   other.Celsius
	Name   other.Label
	Many   []other.Celsius
	Lookup map[string]other.Label
	Ptr    *other.Celsius
	Where  other.Place
	Places []other.Place
}

// Scalared reaches another package through named scalars alone.
//
// It has no field whose type is a struct from there, so nothing else in its
// codec spells that package — which is what makes it the case that says whether
// the imports were gathered from everything the body names or only from the
// type the codec is about.
type Scalared struct {
	Temp other.Celsius
	Many []other.Celsius
	Ptr  *other.Label
}

// Measure says for itself when it is empty, which the standard library asks
// before comparing anything.
type Measure struct {
	Amount int
}

// IsZero reports whether the measure is one nobody set.
//
// Which is not the same as its being the zero value: this type spells unset as
// -1 as well as 0, so a comparison against the zero value and this method
// disagree about exactly one value — which is the whole reason a type declares
// the method rather than leaving it to be compared.
func (m Measure) IsZero() bool { return m.Amount == 0 || m.Amount == -1 }

// Measured holds one, asked to be omitted when it says it is zero.
type Measured struct {
	M    Measure `json:"m,omitzero"`
	Name string  `json:"name"`
}

// Deep is embedded into Shadow, which declares a field of the same name.
type Deep struct {
	Shared string
	Extra  string
}

// Shadow's own field wins the name, and the members it did not win keep their
// own places in the object rather than moving to where the loser was.
type Shadow struct {
	Deep
	Shared string
	Only   string
}

// Borrowed reaches another package only through members that name no package
// when they are written: one has a codec of its own and is written by calling
// it, and one is bytes and goes to the reflective encoder.
//
// It is the case that says whether the imports gathered for a codec are
// narrowed to what its body names. Gathering alone would bind a package the
// file never mentions, which does not compile either.
type Borrowed struct {
	At Ticks
	B  Blob
}

// Ticks and Blob are the other package's, named here so that the fixture reads
// as the ordinary case rather than as a package qualifier in every field.
type (
	Ticks = other.Ticks
	Blob  = other.Blob
)

// Marked holds members asked to be omitted at their zero value, where that
// value is not something a composite literal can spell.
//
// One is written by a codec of its own and is a number underneath; one is an
// interface handed to the reflective encoder. Both are comparable, and neither
// is comparable against a T{} — which is what a rule that asked only whether a
// type could be compared would write for them.
type Marked struct {
	At Ticks `json:"at,omitzero"`

	//forge:json fallback=stdlib
	Extra any `json:"extra,omitzero"`
}

// Nilable holds a pointer to a type that writes itself, omitted when it is nil.
//
// It is what the refusal for the same field under omitempty recommends instead,
// and it is here so that the recommendation is one the suite has run.
type Nilable struct {
	At   *Stamp `json:"at,omitzero"`
	Name string `json:"name"`
}

// Buffered holds members whose emptiness only their own output can settle.
//
// One is written by a codec somebody else wrote, one is a pointer to such a
// type, and one is handed to the reflective encoder. None of the three can be
// asked in advance what it will produce, so each is produced and then looked at
// — which is what the standard library does with all of them.
type Buffered struct {
	One Stamp  `json:"one,omitempty"`
	Ptr *Stamp `json:"ptr,omitempty"`

	//forge:json fallback=stdlib
	Extra any    `json:"extra,omitempty"`
	Name  string `json:"name,omitempty"`
}

// Chain reaches itself and is left out when it renders empty.
//
// Whether such a value is empty depends on how far the chain runs, which no
// condition written against the type can say — so it is written and looked at,
// and the recursion ends where the chain does.
type Chain struct {
	Name string `json:"name,omitempty"`
	Next *Chain `json:"next,omitempty"`
}

// Settled embeds two structs that each declare Shared, and declares Shared
// itself. The tie between the embedded two is broken by the shallower field
// rather than being a name with no answer.
type Settled struct {
	Left
	Right
	Shared string
}

// Left and Right each claim the name the enclosing struct settles.
type (
	Left  struct{ Shared string }
	Right struct{ Shared string }
)

// Hollowed holds a pointer to something that renders as an empty object, which
// is empty in the JSON sense however present the pointer is.
type Hollowed struct {
	Ptr  *Hollow `json:"ptr,omitempty"`
	Text *string `json:"text,omitempty"`
	Name string  `json:"name,omitempty"`
}
