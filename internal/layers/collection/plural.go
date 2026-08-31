package collection

import "strings"

// plural names the projection of a field: the field's own name, made plural.
//
// A projection returns every element's value of one field, so Ages reads as
// what it is where Age would read as one value and AgeValues as a generator
// giving up. That leaves forge pluralising an English word, which is a thing
// with no complete answer — so what is written here is the part that has one.
//
// Three rules, and they are the regular ones: a name ending in a sound that
// cannot take a bare s takes es, a name ending in a consonant and a y trades
// the y for ies, and everything else takes s. They get Ages, Addresses,
// Categories and Boxes right.
//
// What they get wrong they get wrong in the direction of Persons rather than of
// something unpronounceable: Person, Child and Datum, and Epoch, whose ch is
// the sound in monarch rather than the one in match — the two are spelled alike
// and nothing here can hear them, and the match sound is far the commoner
// ending for a field name. There is no table of exceptions that ends, either: a
// generator that knew about children would still not know about the author's
// own domain words.
//
// A name the rules get wrong is a method name an author has to live with. That
// is worth saying plainly rather than hiding: the alternative is a projection
// named after nothing, and the rules are right for the field names Go code
// actually has.
func plural(name string) string {
	switch {
	case name == "":
		return name

	// A sibilant cannot take a bare s: Address becomes Addresses and Box
	// becomes Boxes, where Addresss and Boxs are not words.
	case endsIn(name, "s", "x", "z", "ch", "sh"):
		return name + "es"

	// A y after a consonant is a vowel, and becomes ies: Category becomes
	// Categories. A y after a vowel is not, and takes a bare s: Day becomes
	// Days. Case-blind like the rule above it, since CATEGORY ends the same way
	// Category does.
	case endsIn(name, "y") && len(name) > 1 && !vowel(name[len(name)-2]):
		return name[:len(name)-1] + "ies"

	default:
		return name + "s"
	}
}

// endsIn reports whether a name ends in any of these, ignoring the case of the
// name's own letters — a field is named Address or ADDRESS or address and ends
// the same way in each.
func endsIn(name string, suffixes ...string) bool {
	lowered := strings.ToLower(name)

	for _, suffix := range suffixes {
		if strings.HasSuffix(lowered, suffix) {
			return true
		}
	}
	return false
}

// vowel reports whether a byte is one, which is what decides whether a
// trailing y is.
func vowel(b byte) bool {
	return strings.IndexByte("aeiouAEIOU", b) >= 0
}
