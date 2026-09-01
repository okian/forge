// Package builder generates a way to make a value one field at a time.
//
// What it writes is a companion type — a PersonBuilder for a Person — with one
// method per field and a Build that hands back the value. Each setter answers
// with the builder, so a caller writes the fields in whatever order suits them
// and the compiler still checks every one:
//
//	held, err := NewPersonBuilder().Name("Ada").Email("ada@example.com").Build()
//
// It exists for the struct literal that is hard to read and easy to get wrong.
// A positional literal breaks silently when two fields of one type are
// reordered; a keyed literal is fine until half the fields are optional and
// nobody can tell which half; and a constructor with nine parameters is a
// function whose call site nobody can check by eye. A builder is none of those,
// at the cost of a value that is not usable until Build says it is.
//
// # What Build enforces, and what it does not
//
// A field tagged validate:"required" or validate:"nonzero" is one the author
// has said a value must carry, so Build refuses to hand back a value whose
// setter was never called. It reports every one that was missing rather than
// the first, because a caller filling in a form wants the whole list.
//
// What it does not do is check the value. A caller who calls Age(0) has set
// Age, and Build hands the value back; whether nought is an age is what
// validate:"nonzero" says and what the check generated from it enforces. The
// two questions are different — was anything said, and was what was said any
// good — and a builder that answered the second would be a check written twice,
// in a place where a rule added to the tag would not reach it.
//
// The two rules are read rather than a rule of this layer's own. A separate
// forge:"required" would be a second way to say one thing, which is a second
// place for it to drift; validate's tags are already what every validation
// library in Go reads, and an author who marks a field required means it
// whichever of the two of them is looking.
//
// # What a builder can set
//
// The exported fields, and those alone. A builder is how a value is made from
// outside the package that declares it, and an unexported field is not part of
// that by definition — so one keeps whatever the zero value gives it, and the
// generated type says so. A field a builder cannot set and a tag saying it must
// be given is a contradiction, and is reported rather than resolved: the ways
// out are to export the field or to drop the rule, and neither is forge's to
// choose.
//
// # Failures
//
// Build reports through the same types a generated check reports through, so a
// caller who handles one handles the other and a package with both layers holds
// one vocabulary rather than two that mean the same and compare unequal. A
// field that was never given reads exactly as a field that failed the rule
// would: "Name: required wants a value".
package builder
