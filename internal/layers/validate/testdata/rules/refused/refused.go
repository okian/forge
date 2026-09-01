// Package refused holds the tags that cannot become a check.
//
// One subject per refusal, named for what is wrong with it, so that a failure
// names the question rather than a line in a struct of forty fields.
package refused

// Present asks whether a number is there, which it always is.
type Present struct {
	Count int `validate:"required"`
}

// Compared asks whether a slice equals its zero, which the language will not
// ask.
type Compared struct {
	Items []string `validate:"nonzero"`
}

// Matched asks a number to match a pattern.
type Matched struct {
	Count int `validate:"regexp=^1$"`
}

// Measured asks a number for its exact length.
type Measured struct {
	Count int `validate:"len=3"`
}

// Listed asks a slice to be one of a set.
type Listed struct {
	Items []string `validate:"oneof=a b"`
}

// Invented names a rule nobody wrote.
type Invented struct {
	Name string `validate:"whatever"`
}

// Overfed gives a rule that takes no value one anyway.
type Overfed struct {
	Name string `validate:"required=yes"`
}

// Starved gives a rule that needs a number none.
type Starved struct {
	Name string `validate:"min="`
}

// Worded gives a rule that needs a number a word.
type Worded struct {
	Name string `validate:"min=some"`
}

// Fractional asks a whole number to be at least a fraction.
type Fractional struct {
	Count int `validate:"min=1.5"`
}

// Negative asks for a length shorter than nothing.
type Negative struct {
	Name string `validate:"len=-1"`
}

// Partial asks for a fractional length.
type Partial struct {
	Name string `validate:"len=1.5"`
}

// Mistyped lists words where the field holds numbers.
type Mistyped struct {
	Count int `validate:"oneof=a b"`
}

// Broken gives a pattern that does not compile.
type Broken struct {
	Name string `validate:"regexp=^[a-"`
}

// Empty gives a pattern with nothing in it.
type Empty struct {
	Name string `validate:"regexp="`
}

// Trailing writes a rule after the pattern, which takes the rest of the tag.
type Trailing struct {
	Name string `validate:"regexp=^a$,min=2"`
}

// Doubled leaves an empty rule between two commas.
type Doubled struct {
	Name string `validate:"required,,min=2"`
}
