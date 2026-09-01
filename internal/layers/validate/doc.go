// Package validate generates a value's own check of the rules written on its
// fields.
//
// What it writes is Validate() error on the subject, built from the validate
// tags its fields carry. Each rule becomes a condition in the output: a length
// compared against a number, a string compared against a set, a pattern
// compiled once when the package loads. Nothing is discovered at run time,
// which is what makes the result both readable as the thing it does and free
// when everything is in order — a value that satisfies its rules costs the
// comparisons and no memory at all.
//
// # The rules
//
// Seven, and the list is closed. A rule this package does not know is an error
// rather than a rule quietly ignored, because a tag that says something and
// does nothing is worse than no tag: the author believes the value is checked.
//
//	required    the value is there. A pointer, an interface, a slice, a map,
//	            a channel or a function must not be nil; a string, a slice or
//	            a map must not be empty.
//	nonzero     the value is not the zero value of its type.
//	min=n       a number is at least n; a string, slice, map or array holds at
//	            least n of whatever it holds.
//	max=n       the same, the other way.
//	len=n       a string, slice, map or array holds exactly n.
//	oneof=a b c the value is one of the listed. Written with spaces between
//	            them, which is what the ecosystem writes.
//	regexp=…    a string matches the pattern, which is compiled once at package
//	            level rather than per call.
//
// # required and nonzero are different rules
//
// They coincide often enough to look like synonyms and they are not, and the
// difference is the one every validation library blurs.
//
// required asks whether the value is *there*. It is a question with an answer
// for a pointer, which may be nil, and for a string, a slice or a map, which
// may be empty — and no answer at all for an int, which is always there and
// whose zero is a number somebody may well have meant. So required on a number,
// a boolean, an array or a struct is refused, with a diagnostic naming nonzero.
//
// nonzero asks whether the value equals the zero of its type, which is a
// question about equality — so it is refused on a slice, a map or a function,
// which Go will not compare, with a diagnostic naming required.
//
// Between them they cover everything, they overlap on strings and pointers, and
// a tag that names the wrong one is told which one it meant.
//
// # regexp is written last
//
// A pattern holds commas — {2,4} is the ordinary way to write a repetition —
// and a struct tag separates its options with commas. So the rule is that
// regexp= comes last in the tag and everything after the = is the pattern,
// commas included. A rule written after it is an error rather than a rule
// silently swallowed into the pattern.
//
// It is the same shape of rule encoding/json/v2 gives its own format option,
// and for the same reason: a grammar with no escape has to end somewhere.
//
// # The author's own checks
//
// A rule that cannot be written as a tag is written as a method. A subject that
// declares ValidateEmail() error has it called where the Email field's own
// rules are checked, and what it returns is reported under that field's path.
// The method is the author's, so it is called and not generated: forge writes
// the ones the tags describe and leaves the ones only the author can express.
//
// # What a failure says
//
// Every failure carries the path that reaches the field — Address.City, not
// City — the rule that was not met as it was written, and what the rule wanted
// in words. A value with three bad fields reports three, because a form with
// three bad fields is not three round trips.
//
// One failure per field, though. The rules written on a field are checked in
// the order they were written and the first that fails is the one reported: an
// empty address does not match a pattern either, and saying so as well tells
// somebody two things about one mistake, the second of which is about a value
// that is not there. It is also why required is worth writing first — every
// rule after it is then asked only of a value that is there.
//
// A struct inside a struct is checked by its own Validate, and what it reports
// is folded into the enclosing value's failures with its path extended. So the
// method is written once per type and reached from wherever that type is held.
package validate
