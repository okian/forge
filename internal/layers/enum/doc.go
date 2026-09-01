// Package enum generates the API of a closed set.
//
// A named scalar with constants declared against it is already an enumeration
// in everything but what it can do. What it cannot do is say its own name,
// parse one back, list what it holds, say whether a value is a member at all,
// or refuse one that is not — and each of those is written by hand, the same
// way, in every package that has one:
//
//	type Status int
//
//	const (
//		StatusUnknown Status = iota
//		StatusActive
//	)
//
//	held, err := ParseStatus("active")   // and Status.String, Status.Valid,
//	                                     // ValuesStatus, the text codec
//
// # The constants are the declaration
//
// Nothing is written on the type saying what its members are. They are the
// exported constants the package declares of that type, found by walking its
// scope — the package's rather than the file's, because a large set is usually
// written away from the type it belongs to, and a walk over one file would find
// half of it and say nothing about the rest.
//
// Unexported ones are not members. A run counted by iota usually ends in a
// sentinel nobody outside the package is meant to hold — a count, an end
// marker — and a set that offered one would be offering the one value whose
// whole purpose is not being one.
//
// Reported in declaration order, which is what a reader of the constant block
// expects and is not the order the names sort in: alphabetically, StatusActive
// comes before StatusUnknown, and a list in that order reads as somebody's
// mistake. Order across the files of a package is taken from the file name and
// the offset in it rather than from a raw position, which is only ordered
// within one file — a package's files are parsed in parallel, so which of them
// gets the lower base is decided by which parse finished first.
//
// # What a member is called
//
// The constant's own name with the type's name taken off the front, and what is
// left lower-cased a word at a time: StatusActive is "active" for a Status, and
// StatusOK is "ok" rather than "oK". A word rather than a letter because an
// exported Go name often opens with an initialism, and it is the same rule a
// generated codec names a field by — a package holding both writes one kind of
// name rather than two that differ by which layer wrote them.
//
// A constant whose name does not begin with the type's is spelled in full,
// because there is nothing to take off and cutting somewhere else would name a
// member after a rule rather than after what its author wrote.
//
// A named string is not renamed at all. Its constants carry their text already,
// and a set whose members are "pass" and "fail" is a set an author wrote the
// spelling of; deriving one from the constant's name would give two answers
// about one member and put the wrong one on the wire.
//
// # Two of one thing
//
// A set can hold two of one thing in two ways, and they are not the same.
//
// Two names for one value is an alias, which is what a package has while a name
// is being changed. Both parse, because a reader that took only the new one
// would break every caller the moment it was added; the first declared is what
// renders and what the list holds, because a value that rendered as its alias
// would rename itself as soon as the alias appeared — and because a switch
// cannot hold one value twice.
//
// Two names that are the same is something else. For a named string it is two
// constants written with one text, which is the same alias seen from the other
// side and is treated as one. For a named number it is two constants whose
// names trim to one word, which is refused: they are different values that
// would render alike and parse to whichever was written first, so one of them
// would go out and never come back.
//
// # A value nobody declared
//
// Not every value of the type is a member — the type is the whole range of
// whatever it is a name for — so Valid is how a caller asks. There is no zero
// to compare against: for a set counted from iota the zero is an ordinary
// member, and for a named string it is a member of nothing.
//
// String renders one as the type and the value it holds, the way a stringer
// does. That is right for a log, where the alternative is a plausible-looking
// member that was never declared. It is wrong for a wire, so the text codec
// refuses instead: writing it would produce a document nothing can read back,
// and the zero of a named string is exactly such a value.
//
// Reading one back is an error rather than a zero value, for the same reason
// from the other end. The zero of a set counted from iota is an ordinary
// member, so decoding an unknown name into it would turn a typo in a document
// into a value the receiver treats as meant.
//
// # No JSON codec
//
// None is written and none is wanted. encoding/json reaches for a text codec
// where a type has one, so a member goes over the wire under the name it is
// known by rather than as the number behind it — and a JSON codec of this
// layer's own would be a second answer to a question already answered.
//
// # What is not a closed set
//
// Three things, each refused rather than half written.
//
// A subject with no constants at all, because what would be written is a Parse
// that accepts nothing, a Values that returns nothing and a String that always
// fails — a type made harder to use in exchange for nothing.
//
// A subject that is not a named number or a named string, because a struct has
// no constants to find and asking for the API of a closed set over one is
// asking for something that cannot be built rather than something that happens
// to be empty.
//
// And a subject another package declares. Every part of this API belongs to the
// type — the methods are on it and the two functions are named after it — and
// Go lets a method be declared only in the package that declares its type, so
// there is nowhere for any of it to go.
package enum
