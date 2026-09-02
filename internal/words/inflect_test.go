package words_test

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/okian/forge/internal/words"
)

// row is one line of the corpus: a word and what the two inflections must make
// of it.
type row struct {
	word, plural, singular string
	line                   int
}

// corpus reads testdata/corpus.txt.
//
// A file rather than a table in the source, because it is the contract this
// package is written against and it is read by people arguing about English
// rather than about Go. Adding a word is a line; the properties below then hold
// it to more than the line says.
func corpus(t *testing.T) []row {
	t.Helper()

	held, err := os.Open("testdata/corpus.txt")
	if err != nil {
		t.Fatalf("opening the corpus: %v", err)
	}
	defer func() { _ = held.Close() }()

	var out []row
	scan := bufio.NewScanner(held)

	for at := 1; scan.Scan(); at++ {
		text, _, _ := strings.Cut(scan.Text(), "#")

		fields := strings.Fields(text)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 {
			t.Fatalf("corpus line %d: want a word, a plural and a singular, got %q", at, text)
		}
		out = append(out, row{word: fields[0], plural: fields[1], singular: fields[2], line: at})
	}

	if err := scan.Err(); err != nil {
		t.Fatalf("reading the corpus: %v", err)
	}
	return out
}

func TestTheCorpusInflects(t *testing.T) {
	for _, one := range corpus(t) {
		if got := words.Plural(one.word); got != one.plural {
			t.Errorf("corpus line %d: Plural(%q) = %q, want %q", one.line, one.word, got, one.plural)
		}
		if got := words.Singular(one.word); got != one.singular {
			t.Errorf("corpus line %d: Singular(%q) = %q, want %q", one.line, one.word, got, one.singular)
		}
	}
}

// A plural is a fixed point of itself, which is the property that kills
// Aliaseses: a name derived from an already-plural field is that name.
func TestPluralisingTwiceChangesNothing(t *testing.T) {
	for _, one := range corpus(t) {
		once := words.Plural(one.word)
		if twice := words.Plural(once); twice != once {
			t.Errorf("corpus line %d: Plural(Plural(%q)) = %q, want %q", one.line, one.word, twice, once)
		}
		if !words.IsPlural(once) {
			t.Errorf("corpus line %d: IsPlural(Plural(%q)) is false", one.line, one.word)
		}
	}
}

// The two inflections are inverses over every word the corpus holds that is not
// already plural. A word that is already plural has a singular of its own —
// Data is Datum — so it is asked the other question instead.
func TestSingularUndoesPlural(t *testing.T) {
	for _, one := range corpus(t) {
		if words.IsPlural(one.word) {
			continue
		}
		if got := words.Singular(words.Plural(one.word)); got != one.word {
			t.Errorf("corpus line %d: Singular(Plural(%q)) = %q", one.line, one.word, got)
		}
	}
}

// What answered is what forge explain reports, so it is worth pinning: a change
// that moved a word from the rules to the dictionary would be a change in the
// asset that nothing else would notice.
func TestWhatAnsweredAnInflection(t *testing.T) {
	for _, one := range []struct {
		word string
		from words.Source
	}{
		{"Person", words.FromDictionary},
		{"Leaf", words.FromDictionary},
		{"Box", words.FromRule},
		{"Widget", words.FromRule},
		{"ID", words.FromInitialism},
		{"Aliases", words.FromPlural},
		{"Data", words.FromPlural},
	} {
		if _, got := words.PluralFrom(one.word); got != one.from {
			t.Errorf("PluralFrom(%q) came from %v, want %v", one.word, got, one.from)
		}
	}

	if got := words.Source(200).String(); got != "unknown" {
		t.Errorf("Source(200) = %q, want the unknown source", got)
	}
}

// A word with no letters in it has nothing to inflect, and comes back as it
// went in rather than with an s stuck on the end of a separator.
func TestANameWithNoWordInIt(t *testing.T) {
	for _, one := range []string{"", "_", "__"} {
		if got := words.Plural(one); got != one {
			t.Errorf("Plural(%q) = %q", one, got)
		}
		if got := words.Singular(one); got != one {
			t.Errorf("Singular(%q) = %q", one, got)
		}
		if words.IsPlural(one) {
			t.Errorf("IsPlural(%q) is true", one)
		}
	}

	if got := words.Regular(""); got != "" {
		t.Errorf("Regular(%q) = %q", "", got)
	}
}

// The regular rules alone, which is what the converter drops entries against
// and what forge explain compares the dictionary to.
func TestTheRegularRules(t *testing.T) {
	for word, want := range map[string]string{
		"Box": "Boxes", "Address": "Addresses", "Category": "Categories",
		"Day": "Days", "Person": "Persons", "Epoch": "Epoches",
		"CATEGORY": "CATEGORies", "Dish": "Dishes",
		// What the rules alone make of a word the dictionary has to correct.
		"Quiz": "Quizes", //nolint:misspell // The rules' answer, which is why the dictionary carries the word.
	} {
		if got := words.Regular(word); got != want {
			t.Errorf("Regular(%q) = %q, want %q", word, got, want)
		}
	}
}
