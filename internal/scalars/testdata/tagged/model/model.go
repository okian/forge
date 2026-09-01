// Package model holds one subject per signal these emitters answer to.
//
// Each is the smallest thing that carries its signal, because what is under
// test is the reading of the signal rather than the shape of a realistic type.
package model

import "fmt"

// Labelled carries display tags, one with a name and one without.
type Labelled struct {
	Name string `display:""`
	Age  int    `display:"age"`
	Note string
}

// Quiet carries no tags at all, and is what the emitters have to leave alone.
type Quiet struct {
	Name string
	Age  int
}

// Secret carries a redact tag, which is what asks for a log value.
type Secret struct {
	User  string
	Token string `redact:""`
	Tries int
}

// Wrapped is a struct around one string with the tag that asks for its text
// form, which is what a codec is written for.
type Wrapped struct {
	ID string `display:""`
}

// Bare is the same struct without the tag, and gets nothing: a codec written
// unasked would change how the type encodes as JSON.
type Bare struct {
	ID string
}

// Named is the same again with a labelled tag, which asks for a rendering for a
// person rather than for a round trip — so it reads and does not encode.
type Named struct {
	ID string `display:"id"`
}

// Counted is the same over a number, so that the parsing half is exercised as
// well as the appending one.
type Counted struct {
	N int32 `display:""`
}

// Pair holds two fields, so it is not a wrapper and earns no text codec.
type Pair struct {
	Low  int
	High int
}

// Wide wraps something that is not a scalar, which is the other way not to be a
// wrapper.
type Wide struct {
	Names []string
}

// Everything carries every signal at once, which is where two emitters could
// collide over one subject if they were going to.
type Everything struct {
	Name  string `display:""`
	Token string `redact:""`
}

// Quoted carries a label with characters a Go string literal has to escape.
type Quoted struct {
	Path string `display:"the \"path\""`
}

// Stamp is a named type that says how it reads, which is what a display tag on
// a field of this type has to reach for.
type Stamp struct {
	Text string
}

// String is Stamp's own, and is what a rendering of a field holding one calls.
func (s Stamp) String() string { return s.Text }

// Timed carries a display tag on a field whose type is not a scalar.
type Timed struct {
	At Stamp `display:"at"`
}

// Flagged, Measured and Ported wrap the other kinds a text codec is written
// over, so that each conversion is exercised rather than described.
type Flagged struct {
	On bool `display:""`
}

// Measured wraps a float, whose text form carries a precision decision.
type Measured struct {
	Value float64 `display:""`
}

// Ported wraps an unsigned integer, which strconv reads through a different
// pair of calls than a signed one.
type Ported struct {
	Port uint16 `display:""`
}

// Held carries a redact tag beside a field that is not a scalar, which slog has
// to be handed as itself.
type Held struct {
	Names []string
	Token string `redact:""`
}

// Unrenderable carries a display tag on a field nothing here can write down.
//
// A slice says nothing about how it reads and has no String of its own, so the
// only ways to render it are to guess a format or to reach for reflection —
// and this refuses rather than doing either.
type Unrenderable struct {
	Names []string `display:"names"`
}

// Optioned carries a display tag with an option nothing reads.
type Optioned struct {
	Name string `display:"name,omitempty"`
}

// Skipped carries a display tag that excludes the field, which is the
// conventional meaning of a dash and is not a mistake.
type Skipped struct {
	Name   string `display:""`
	Hidden string `display:"-"`
}

// Maybe carries a display tag on a pointer, which may be nil when it is read.
type Maybe struct {
	At *Stamp `display:"at"`
}

// Collides has a field called String, which is a name no method can take.
type Collides struct {
	String string
	Age    int `display:"age"`
}

// Spoken carries a display tag on an interface, which can hold nothing at all.
type Spoken struct {
	By fmt.Stringer `display:"by"`
}

// Earning is a subject whose own String this run writes, and Reaching displays
// a field of that type — so whether the second can be written depends on what
// the first is about to earn rather than on what a previous run left behind.
type Earning struct {
	Name string `display:""`
}

// Reaching displays a field whose String has not been written yet.
type Reaching struct {
	Held Earning `display:"held"`
}

// Pointing displays a field whose type has a field called String, so no String
// will ever be written for it however many display tags it carries.
type Pointing struct {
	At Collides `display:"at"`
}
