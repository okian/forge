package words

import "strings"

// Source says what answered a question about a word, which is what forge
// explain reports so that "why is my method called that" is a command rather
// than an issue.
type Source uint8

// Where an inflection came from.
const (
	// FromRule is the regular rules: a sibilant takes es, a consonant and a y
	// take ies, everything else takes s. Where a domain word lands, and where
	// it belongs.
	FromRule Source = iota

	// FromDictionary is the embedded dictionary, which knew the word.
	FromDictionary

	// FromInitialism is the Go initialism set: an initialism takes a small s
	// and never es.
	FromInitialism

	// FromPlural is the word already being plural, so nothing was done to it.
	// Aliases is Aliases, which is what keeps Aliaseses from existing.
	FromPlural
)

// sourceNames say where an answer came from, in the words a report uses.
var sourceNames = [...]string{
	FromRule:       "the regular rules",
	FromDictionary: "the dictionary",
	FromInitialism: "the Go initialism set",
	FromPlural:     "the name already being plural",
}

// String names the source the way a diagnostic or an explanation prints it.
func (s Source) String() string {
	if int(s) >= len(sourceNames) {
		return "unknown"
	}
	return sourceNames[s]
}

// Plural returns the name a collection of these is called.
//
// Only the last word inflects, and everything before it survives exactly as it
// was written: HomeAddress is HomeAddresses, ChildNode is ChildNodes, and
// NodeChild is NodeChildren. That is the whole of what a compound name needs,
// because English puts the head of a noun phrase last.
//
// The dictionary answers where it knows the word — Person is People, Child is
// Children, Datum is Data, Leaf is Leaves, Analysis is Analyses, and Epoch is
// Epochs rather than the Epoches a rule that cannot hear the difference between
// monarch and match would give. The regular rules answer everywhere else, which
// is where a word forge has never heard of lands and is the right answer for
// nearly all of them.
//
// A name that is already plural comes back unchanged. Aliases is Aliases, which
// is what stops Aliaseses from ever being written — and makes that name collide
// with the field it was derived from, which is a collision to report rather
// than a spelling nobody would have chosen.
//
// An initialism takes a small s and never es: ID is IDs, URL is URLs, API is
// APIs. Written down because it needs deciding rather than because it is
// obvious: OS is OSs, not OSes. A bare s is the rule that keeps [Singular] an
// exact inverse of this, and the initialisms that end in a sibilant — OS,
// HTTPS, DNS, TLS — are ones nobody pluralises in the first place.
func Plural(name string) string {
	held, _ := PluralFrom(name)
	return held
}

// PluralFrom returns a name's plural together with what decided it.
func PluralFrom(name string) (string, Source) {
	at, end := last(name)
	if at < 0 {
		return name, FromRule
	}

	held, from := pluralWord(name[at:end])
	if at == 0 && end == len(name) {
		return held, from
	}
	return name[:at] + held + name[end:], from
}

// pluralWord inflects one word.
func pluralWord(w string) (string, Source) {
	if _, is := Initialism(w); is {
		return w + "s", FromInitialism
	}
	if plural(w) {
		return w, FromPlural
	}

	if held, is := dictionary().Plural(w); is {
		return recased(w, held), FromDictionary
	}
	return Regular(w), FromRule
}

// Singular returns the name one of these is called.
//
// The inverse of [Plural] over every word either of them knows: People is
// Person, Boxes is Box, Categories is Category, IDs is ID. A name that is
// already singular is returned as it is, so that a caller may ask without first
// having to know.
//
// Here because a layer that names an element after the collection holding it
// has nowhere else to ask, and because the alternative is the fourth
// hand-written rule in a tree that has already paid for three.
func Singular(name string) string {
	at, end := last(name)
	if at < 0 {
		return name
	}
	return name[:at] + singularWord(name[at:end]) + name[end:]
}

// singularWord reduces one word.
func singularWord(w string) string {
	if stem, cut := strings.CutSuffix(w, "s"); cut {
		if _, is := Initialism(stem); is {
			return stem
		}
	}
	if _, is := Initialism(w); is {
		return w
	}

	if held, is := dictionary().Singular(w); is {
		return recased(w, held)
	}
	if !plural(w) {
		return w
	}
	return reduced(w)
}

// IsPlural reports whether a name is already the plural of something, which is
// what keeps a projection over a field called Aliases from being Aliaseses.
func IsPlural(name string) bool {
	at, end := last(name)
	if at < 0 {
		return false
	}

	return plural(name[at:end])
}

// plural reports whether one word is already plural.
//
// The dictionary first, because it is the only thing that can know People and
// Children and Data are plurals of anything. Then the vocabulary, which is what
// tells a singular noun that happens to end in s from a plural: Alias, Status
// and Address are words rather than plurals, and nothing about their spelling
// says so.
//
// The endings answer last, for a word the dictionary has never heard of. A
// domain word ending in s is taken to be plural unless it ends in ss, us or is,
// which are the endings a singular noun has — so Widgets is plural, Cujus is
// not, and neither of them had to be in a dictionary for that to come out.
func plural(w string) bool {
	if stem, cut := strings.CutSuffix(w, "s"); cut {
		if _, is := Initialism(stem); is {
			return true
		}
	}
	if _, is := Initialism(w); is {
		return false
	}

	held := dictionary()

	switch {
	case has(held.Singular(w)):
		return true
	case has(held.Plural(w)), held.Known(w):
		return false
	default:
		return looksPlural(w)
	}
}

// has reports a lookup that found something, so that a switch can read as a
// list of questions rather than as a pile of assignments.
func has(_ string, found bool) bool { return found }

// looksPlural reads a word's ending, which is all there is to go on for a word
// no dictionary carries.
func looksPlural(w string) bool {
	if len(w) < 3 || !endsIn(w, "s") {
		return false
	}
	return !endsIn(w, "ss", "us", "is")
}

// Regular returns the plural the regular rules give a word, without consulting
// the dictionary.
//
// Exported for two callers and no others. The converter under gen drops every
// dictionary pair the rules already get right, and would otherwise be applying
// its own copy of them — two copies of one rule, and the asset silently wrong
// the day they part. And forge explain says of a derived name whether the
// dictionary answered or the rules did, which it cannot do without being able
// to ask what the rules alone would have said.
func Regular(word string) string {
	switch {
	case word == "":
		return word

	// A sibilant cannot take a bare s: Address becomes Addresses and Box
	// becomes Boxes, where Addresss and Boxs are not words.
	case endsIn(word, "s", "x", "z", "ch", "sh"):
		return word + "es"

	// A y after a consonant is a vowel, and becomes ies: Category becomes
	// Categories. A y after a vowel is not, and takes a bare s: Day becomes
	// Days. Case-blind like the rule above it, since CATEGORY ends the same way
	// Category does.
	case endsIn(word, "y") && len(word) > 1 && !isVowel(word[len(word)-2]):
		return word[:len(word)-1] + "ies"

	default:
		return word + "s"
	}
}

// reduced undoes [Regular], which is the answer for every plural the dictionary
// did not have to carry.
func reduced(w string) string {
	switch {
	case endsIn(w, "ies") && len(w) > 4 && !isVowel(w[len(w)-4]):
		return w[:len(w)-3] + matchingCase(w[len(w)-3], 'y')

	case endsIn(w, "es") && endsIn(w[:len(w)-2], "s", "x", "z", "ch", "sh"):
		return w[:len(w)-2]

	case endsIn(w, "s"):
		return w[:len(w)-1]

	default:
		return w
	}
}

// matchingCase writes a letter in the case of the one it replaces, so that
// CATEGORIES reduces to CATEGORY rather than to CATEGORy.
func matchingCase(like byte, letter byte) string {
	if like >= 'A' && like <= 'Z' {
		return string(letter - 'a' + 'A')
	}
	return string(letter)
}

// endsIn reports whether a word ends in any of these, ignoring the case of the
// word's own letters — a field is named Address or ADDRESS or address and ends
// the same way in each.
//
// Folded as it compares rather than by lower-casing the word first, because
// every rule in this file asks it and a copy of the word per question is a
// copy per name forge writes.
func endsIn(word string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if len(word) >= len(suffix) && strings.EqualFold(word[len(word)-len(suffix):], suffix) {
			return true
		}
	}
	return false
}
