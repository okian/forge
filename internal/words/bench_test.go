package words_test

import (
	"testing"

	"github.com/okian/forge/internal/words"
)

// What a name costs to derive, which is worth a budget because it is paid once
// per generated method and because the dictionary is the thing most likely to
// make it worse.
//
// One allocation each is the target and is what the budget says. The lookups
// fold as they compare rather than lower-casing the word first, and a hit
// answers with a slice of the asset rather than a copy of it — so what is left
// is the one string the answer itself is.
func BenchmarkPluralFromTheDictionary(b *testing.B) {
	warm(b)

	for b.Loop() {
		sink = words.Plural("Person")
	}
}

func BenchmarkPluralFromTheRules(b *testing.B) {
	warm(b)

	for b.Loop() {
		sink = words.Plural("Widget")
	}
}

// A compound name, where only the last word inflects and the rest is joined
// back on: the second allocation is that join.
func BenchmarkPluralOfACompoundName(b *testing.B) {
	warm(b)

	for b.Loop() {
		sink = words.Plural("HomeAddress")
	}
}

// Spelling a name out of its parts, which every generated identifier goes
// through.
func BenchmarkSpellAMethodName(b *testing.B) {
	for b.Loop() {
		sink = words.Spell(words.KindMethod, true, "sorted", "by", "userId")
	}
}

// warm decodes the dictionary before anything is measured.
//
// It is decoded once per process and only if something asks, so a run that did
// not warm it would divide that one decode by however many iterations it did
// and report a number about the run length rather than about the code.
func warm(b *testing.B) {
	b.Helper()

	if _, is := words.English().Plural("person"); !is {
		b.Fatal("the embedded dictionary does not know person")
	}
}

// sink keeps the compiler from deciding a benchmark's work is unobserved.
var sink string
