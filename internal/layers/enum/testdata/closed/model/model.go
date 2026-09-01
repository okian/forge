// Package model holds the closed sets the enumeration layer is asked about.
//
// One per thing that can be true of a named scalar with constants declared
// beside it, so that a test naming a subject is naming a shape rather than a
// block somebody has to read the whole file to understand.
package model

// Status is the ordinary case: a named integer with a run of constants declared
// against it, counted by iota from a zero that means nothing was set.
type Status int

// The statuses. Declaration order is what Values reports and what a reader of
// the constant block expects, which is not the order the names sort in.
const (
	StatusUnknown Status = iota
	StatusActive
	StatusSuspended
	StatusClosed
)

// Grade is a named string, whose constants carry values rather than positions.
//
// The other half of what a closed set can be: an integer's constants are told
// apart by a number nobody writes down, and a string's are the text itself.
type Grade string

// The grades, out of iota's way entirely.
const (
	GradePass Grade = "pass"
	GradeFail Grade = "fail"
)

// Aliased declares two names for one value, which is an ordinary thing to write
// while a name is being changed.
type Aliased int

// The values, one of which has two names.
const (
	AliasedFirst  Aliased = 1
	AliasedSecond Aliased = 2

	// AliasedOne is the older spelling of AliasedFirst, kept so that callers
	// have somewhere to move from.
	AliasedOne Aliased = 1
)

// Coded is a set whose members open with initialisms, which is what most sets
// of statuses look like.
//
// Lowering one letter of one would give "oK" and "hTTPError" — names nobody
// would write and no reader would recognise.
type Coded int

// The codes.
const (
	CodedOK Coded = iota
	CodedHTTPError
	CodedID

	// codedCount is the sentinel a run like this usually ends in. It is
	// unexported because nothing outside the package is meant to hold one, and
	// a set that offered it as a member would be offering the one value whose
	// whole purpose is not being one.
	codedCount
)

// Renamed holds two constants written with one text, which for a named string
// is two members of one name rather than two names for one member.
type Renamed string

// The two spellings, one of them older.
const (
	RenamedPass    Renamed = "pass"
	RenamedSuccess Renamed = "pass"
)

// Permitted is unsigned, so a value above what a signed number holds would come
// out negative if it were rendered through the signed function.
type Permitted uint8

// The permissions.
const (
	PermittedNone Permitted = 0
	PermittedAll  Permitted = 255
)

// Clashing has two constants whose names come to one word, which is a typo
// rather than an alias: they hold different values, so one of them would render
// as the other's name and never parse back.
type Clashing int

// The two, differing only in how the second word is capitalised.
const (
	ClashingOK Clashing = iota
	ClashingOk
)

// Loose has a constant whose name does not begin with the type's, which keeps
// all of it because there is nothing to take off.
type Loose int

// The members, one named after the type and one not.
const (
	LooseFirst Loose = iota
	Wandering
)

// Owned declares its own Valid, which is the author answering the question this
// layer would otherwise answer for them.
type Owned int

// The members.
const (
	OwnedOne Owned = iota
	OwnedTwo
)

// Valid is the author's own, and is the one the text codec calls.
func (v Owned) Valid() bool { return v == OwnedOne }

// Empty is a named scalar with no constants declared against it at all, which
// is not a closed set and cannot be given the API of one.
type Empty int

// Elsewhere is a named scalar whose constants are declared in another file of
// the same package, which is where a large set usually is.
type Elsewhere int

// Structured is not a scalar at all. Asking for an enumeration over it is
// asking for a set of constants a struct cannot have.
type Structured struct {
	Name string
}
