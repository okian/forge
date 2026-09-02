package words

import (
	"strings"
	"unicode"
)

// Agent names a one-method interface after the method it holds.
//
// A Go interface with one method is named for that method with an -er ending —
// Reader, Validator, Encoder — and the ending is not a suffix anybody can
// append. Notify becomes Notifier, Marshal becomes Marshaller, and Validate
// becomes Validator rather than Validater. That is why it lives beside the
// plural tables rather than in whichever layer first wanted one: it is the same
// kind of question, answered from the same dictionary, and getting it wrong
// produces a name a reader trips over in a file they cannot edit.
//
// Never IFoo, never FooInterface, never an Abstract prefix. Forge does not
// invent a name for an interface with more than one method either — a name for
// what a type *is* rather than what it *does* is not derivable, and a layer
// that wants one says it in the declaration.
//
// Parts before the last prefix the name, so Agent("person", "validate") is
// PersonValidator: the last part is the verb and everything before it says
// whose.
func Agent(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}

	verb := Words(parts[len(parts)-1])
	if len(verb) == 0 {
		return Join(parts...)
	}

	last := len(verb) - 1
	verb[last] = ending(verb[last])

	return Join(append(append([]string{}, parts[:len(parts)-1]...), verb...)...)
}

// ending returns the agent noun of one verb, spelled in the case it arrived in.
//
// Three answers in order, because they answer different things. The -ate rule
// is Latin rather than English and the dictionary does not carry it, so it is
// written here: a verb ending in -ate takes -ator, which is right for validate,
// iterate, generate and translate and wrong for the handful of verbs that are a
// short word with date or rate inside them. Those are listed rather than
// derived, because the list is short and the alternative is counting syllables.
//
// The dictionary answers next, and what it knows is where a final consonant
// doubles: begin becomes beginner and open does not become openner, and nothing
// in the spelling of either says which. What it does not carry is the choice
// between marshaler and marshaller, which is a dialect rather than a fact — so
// a verb ending in l is not asked about and takes the doubled form this
// repository's own prose is written in.
//
// The regular rules answer last, which is where a domain verb the dictionary
// has never heard of lands.
func ending(verb string) string {
	if held, is := latinate(verb); is {
		return held
	}
	if held, is := dictionary().Agent(verb); is {
		return recased(verb, held)
	}
	return regularAgent(verb)
}

// latin lists the verbs ending in -ate that take -er rather than -ator.
//
// Short, and it is the whole exception: every other -ate verb a method is named
// after — validate, iterate, generate, allocate, annotate, aggregate — takes
// -ator. What these have in common is that the ate is not a suffix at all but
// the end of a word in its own right, which is a thing a rule cannot see and a
// list can.
var latin = map[string]string{
	"date":   "dater",
	"gate":   "gater",
	"mate":   "mater",
	"rate":   "rater",
	"state":  "stater",
	"update": "updater",
	"debate": "debater",
}

// latinate returns the agent noun of a verb ending in -ate, and whether the
// verb is one.
func latinate(verb string) (string, bool) {
	folded := strings.ToLower(verb)

	if held, listed := latin[folded]; listed {
		return recased(verb, held), true
	}
	if stem, is := strings.CutSuffix(folded, "ate"); is && stem != "" {
		return recased(verb, stem+"ator"), true
	}
	return "", false
}

// regularAgent returns the agent noun the ordinary rules give.
func regularAgent(verb string) string {
	folded := strings.ToLower(verb)

	switch {
	case strings.HasSuffix(folded, "e"):
		// Encode is Encoder and Write is Writer: the e is already there.
		return verb + "r"

	case len(folded) > 1 && strings.HasSuffix(folded, "y") && !isVowel(folded[len(folded)-2]):
		// A y after a consonant is a vowel, and becomes ier: Notify is
		// Notifier. A y after a vowel is not, and Play is Player.
		return verb[:len(verb)-1] + "ier"

	case doubles(folded):
		return verb + folded[len(folded)-1:] + "er"

	default:
		return verb + "er"
	}
}

// doubles reports whether a verb's final consonant doubles before the ending.
//
// A single vowel between two consonants, in a word of one syllable, is the
// case: Run is Runner and Get is Getter, where Read is Reader because ea is two
// vowels and Open is Opener because it has two syllables. A final l doubles
// however many syllables there are, which is the spelling this repository is
// written in and the reason marshal is Marshaller.
//
// Syllables counted as runs of vowels, which is close enough for the question
// being asked: what it decides is a letter in a name, and the words it gets
// wrong are the ones the dictionary was consulted about first.
func doubles(folded string) bool {
	if len(folded) < 3 {
		return false
	}

	last, before := folded[len(folded)-1], folded[len(folded)-2]
	if !doubling(last) || !isVowel(before) || isVowel(folded[len(folded)-3]) {
		return false
	}

	return last == 'l' || syllables(folded) == 1
}

// doubling reports whether a consonant is one that doubles at all. Nothing in
// English ends in a doubled w, x or y, and h, j, q and v do not double either.
func doubling(b byte) bool {
	return b >= 'a' && b <= 'z' && !isVowel(b) && strings.IndexByte("hjqvwxy", b) < 0
}

// syllables counts a word's runs of vowels, which stands in for its syllables.
func syllables(folded string) int {
	count, inside := 0, false

	for i := range len(folded) {
		vowel := isVowel(folded[i])
		if vowel && !inside {
			count++
		}
		inside = vowel
	}
	return count
}

// isVowel reports whether a byte is one.
func isVowel(b byte) bool { return strings.IndexByte("aeiouAEIOU", b) >= 0 }

// recased writes a dictionary's answer in the case the question was asked in.
//
// Address, ADDRESS and address are one word and inflect one way, and each comes
// back spelled the way it went in — an all-capital name stays one, a name with
// a capital at the front keeps it, and anything else is left as the dictionary
// has it.
func recased(asked, answer string) string {
	switch {
	case asked == "":
		return answer
	case upperRun(asked):
		return strings.ToUpper(answer)
	case capitalised(asked):
		return Upper(answer)
	default:
		return answer
	}
}

// capitalised reports whether a name opens with a capital.
func capitalised(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}
