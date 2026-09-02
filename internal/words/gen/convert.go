package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/okian/forge/internal/words"
)

// converter is the version of this program, recorded in the asset so that a
// change in what the conversion keeps is visible without diffing the bytes.
const converter = 1

// built is everything that goes into one asset.
type built struct {
	from release

	// plurals holds a singular against its plural, and only where the regular
	// rules do not already reach it. Its reverse is written as the singular
	// table, so the two cannot disagree.
	plurals map[string]string

	// agents holds a verb against its -er form, for the verbs whose final
	// consonant doubles.
	agents map[string]string

	// vocabulary is every singular noun ending in s, which is the one shape
	// nothing else can tell from a plural.
	vocabulary []string
}

// convert reads a release into the tables the asset holds.
func convert(from release) (built, error) {
	out := built{from: from, plurals: map[string]string{}, agents: map[string]string{}}

	nouns := map[string]string{}
	for _, held := range parse(from.inflections) {
		switch {
		case !held.certain || !plainWord(held.word):
			continue
		case held.pos == 'N' && !capitalised(held.word):
			noun(nouns, held)
		case held.pos == 'V':
			verb(out.agents, held)
		}
	}

	if err := override(nouns, out.agents); err != nil {
		return built{}, err
	}

	for _, one := range slices.Sorted(maps.Keys(nouns)) {
		if strings.HasSuffix(one, "s") {
			out.vocabulary = append(out.vocabulary, one)
		}
		if held := nouns[one]; held != words.Regular(one) {
			out.plurals[one] = held
		}
	}
	return out, nil
}

// noun records a lemma's preferred plural, keeping the first spelling upstream
// gives where a word is listed more than once.
//
// A word upstream says is its own plural is dropped wherever upstream also
// lists the regular plural, however faintly. Four hundred entries claim a zero
// plural and most of them are of that shape — sibling, handshake, coupling,
// sampling, handling — where the s form is there and merely marked as a
// possibility. Believing the claim would leave a field named Sibling with a
// projection called Sibling, which is a collision reported at every generation
// rather than the plural anybody wanted. What survives the rule is the words
// that really have no plural and never list one: sheep, aircraft, chassis,
// police.
func noun(into map[string]string, held line) {
	if len(held.groups) == 0 {
		return
	}

	folded := strings.ToLower(held.word)
	if _, seen := into[folded]; seen {
		return
	}

	plural := strings.ToLower(pick(held.groups[0]))
	if plural == "" || (plural == folded && holds(held.groups[0], words.Regular(folded))) {
		return
	}
	into[folded] = plural
}

// holds reports whether a group lists a form at all, whatever upstream tagged
// it with. Asked of a form that was already rejected, to find out whether it
// was rejected for being absent or for being doubted.
func holds(group []string, want string) bool {
	for _, one := range group {
		spelled, _, _ := strings.Cut(strings.TrimSpace(one), " ")
		if strings.EqualFold(strings.TrimRight(spelled, "~<!?"), want) {
			return true
		}
	}
	return false
}

// verb records the -er form of a verb whose final consonant doubles.
//
// Read off the -ing form rather than decided here, because nothing in the
// spelling of begin and open says why one is beginning and the other is not
// openning — and an agent noun doubles exactly where the -ing form does.
//
// A verb ending in l is left out on purpose. Upstream prefers marshaling to
// marshalling, which is a dialect rather than a fact about English, and forge
// spells that ending doubled to agree with its own prose. Leaving the entry out
// is what lets the rule in words decide it.
func verb(into map[string]string, held line) {
	if len(held.groups) < 2 {
		return
	}

	folded := strings.ToLower(held.word)
	if _, seen := into[folded]; seen || strings.HasSuffix(folded, "l") {
		return
	}

	ing := strings.ToLower(pick(held.groups[len(held.groups)-2]))
	if stem, cut := strings.CutSuffix(ing, "ing"); cut && stem == folded+folded[len(folded)-1:] {
		into[folded] = stem + "er"
	}
}

// capitalised reports whether upstream wrote a lemma as a proper noun.
//
// Dropped, because forge asks about a word without regard to the case it was
// written in: an entry that can only be reached by a field named after a Greek
// genus or an Italian city is bytes spent on an answer nobody wants, and it is
// bytes spent making Anser into Anseres.
func capitalised(word string) bool { return word != "" && word[0] >= 'A' && word[0] <= 'Z' }

// provenance returns the line the asset opens with.
func (b built) provenance() string {
	return fmt.Sprintf(
		"forge words 1 agid=%s sha256=%s plurals=%d agents=%d vocabulary=%d converter=%d",
		b.from.version, b.from.sum, len(b.plurals), len(b.agents), len(b.vocabulary), converter)
}
