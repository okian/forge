package jsoncodec

import (
	"testing"

	"github.com/okian/forge/internal/model"
)

// A boolean option is read the way it was validated.
//
// Options validation accepts every spelling [strconv.ParseBool] does — 1, t,
// T, TRUE, 0, f, F and FALSE as well as the two words anybody writes — so a
// layer that compared against the word would read omitzero=1 as off and leave a
// member in a document that asked for it out. Silently: the declaration passes
// validation and the codec generates.
//
// Every spelling rather than a representative few, because what is being
// checked is agreement with a list somebody else wrote. A test over "true" and
// "false" passes against a comparison and against a parse alike, which is to
// say it checks nothing here.
func TestEverySpellingOfABooleanOption(t *testing.T) {
	on := []string{"1", "t", "T", "TRUE", "true", "True"}
	off := []string{"0", "f", "F", "FALSE", "false", "False"}

	for _, held := range on {
		if !omitting(carrying(held)) {
			t.Errorf("%s=%q reads as off", optionOmitZero, held)
		}
	}

	for _, held := range off {
		if omitting(carrying(held)) {
			t.Errorf("%s=%q reads as on", optionOmitZero, held)
		}
	}

	// Unwritten is off, which is what the schema declares as the default.
	if omitting(model.Options{}) {
		t.Errorf("%s reads as on when it was not written", optionOmitZero)
	}

	// And a value nothing can parse reads as off rather than as something. It
	// is unreachable — validation refuses one before this layer is asked
	// anything — and the direction is what matters: a member left in is a
	// document that says more than it was asked to, which is recoverable, and
	// one left out is a document missing a field nobody knows about.
	if omitting(carrying("perhaps")) {
		t.Errorf("%s=perhaps reads as on", optionOmitZero)
	}
}

// carrying builds the option set a declaration carrying omitzero produces.
func carrying(value string) model.Options {
	return model.Options{
		Layer:   markerName,
		Entries: []model.Option{{Key: optionOmitZero, Value: value}},
	}
}
