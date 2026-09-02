package words_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/okian/forge/internal/words"
)

// The dictionary is embedded, so a binary built from a clean checkout answers
// the same way as one built anywhere else. What that is worth is only as good
// as the provenance saying which dictionary it was.
func TestTheDictionaryTravelsWithTheBinary(t *testing.T) {
	held := words.Provenance()

	for _, want := range []string{"forge words 1", "agid=", "sha256=", "converter="} {
		if !strings.Contains(held, want) {
			t.Errorf("the provenance %q does not say %q", held, want)
		}
	}
	if strings.HasPrefix(held, "#") {
		t.Errorf("the provenance %q still carries the comment marker", held)
	}

	if _, is := words.English().Plural("person"); !is {
		t.Error("the embedded dictionary does not know person")
	}
	if words.English().Known("cuju") {
		t.Error("the embedded dictionary claims to know cuju")
	}
}

// The file is the form a person edits, so what it costs to be readable is
// checked here rather than assumed: an asset written the way the converter
// writes it loads, and every table it carries answers.
func TestTheTextFormLoads(t *testing.T) {
	held, err := words.Load([]byte(sample))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}

	for _, one := range []struct{ word, want string }{
		{"person", "people"},
		{"Person", "people"},
		{"basis", "bases"},
		{"alumnus", "alumni"},
	} {
		if got, is := held.Plural(one.word); !is || got != one.want {
			t.Errorf("Plural(%q) = %q, %v; want %q", one.word, got, is, one.want)
		}
	}

	if got, is := held.Agent("run"); !is || got != "runner" {
		t.Errorf("Agent(run) = %q, %v; want runner", got, is)
	}
	if !held.Known("address") {
		t.Error("the vocabulary section did not load")
	}
	if held.Known("cuju") {
		t.Error("a word nothing declares is known")
	}
}

// There is no singular section in the file, and there is a singular table in
// the dictionary. This is the derivation, and the tie-break that keeps it from
// depending on the order anything was built in: base and basis both pluralise
// to bases, and base is the one that comes back.
func TestSingularIsDerivedFromPlural(t *testing.T) {
	held, err := words.Load([]byte(sample))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}

	for _, one := range []struct{ word, want string }{
		{"people", "person"},
		{"People", "person"},
		{"alumni", "alumnus"},
		{"bases", "base"},
	} {
		if got, is := held.Singular(one.word); !is || got != one.want {
			t.Errorf("Singular(%q) = %q, %v; want %q", one.word, got, is, one.want)
		}
	}

	if _, is := held.Singular("person"); is {
		t.Error("a singular answered as though it were a plural")
	}
}

// The whole point of committing text is that somebody edits it, and the entry
// they add lands wherever their editor put it. A word out of order is still a
// word the dictionary finds.
func TestAWordInsertedOutOfOrderIsStillFound(t *testing.T) {
	held, err := words.Load([]byte(strings.Join([]string{
		"# forge words 1",
		"",
		"[plural]",
		"person\tpeople",
		"alumnus\talumni", // before person, alphabetically
		"basis\tbases",
	}, "\n")))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}

	for _, one := range []struct{ word, want string }{
		{"alumnus", "alumni"},
		{"basis", "bases"},
		{"person", "people"},
	} {
		if got, is := held.Plural(one.word); !is || got != one.want {
			t.Errorf("Plural(%q) = %q, %v; want %q", one.word, got, is, one.want)
		}
	}
}

// An asset this package cannot read is a broken build rather than a bad input,
// and every rule works without one — so an empty one loads as an empty
// dictionary and the names come out regular.
func TestAnAssetThatIsNotOne(t *testing.T) {
	for _, one := range []struct {
		what  string
		asset string
	}{
		{"nothing at all", ""},
		{"a provenance line and no sections", "# forge words 1\n"},
		{"comments and blank lines only", "# forge words 1\n\n# and nothing else\n\n"},
		{"a section with no entries", "# forge words 1\n\n[plural]\n"},
	} {
		held, err := words.Load([]byte(one.asset))
		if err != nil {
			t.Fatalf("%s: Load = %v", one.what, err)
		}
		if _, is := held.Plural("person"); is {
			t.Errorf("%s: an empty dictionary answered about person", one.what)
		}
		if _, is := held.Singular("people"); is {
			t.Errorf("%s: an empty dictionary answered about people", one.what)
		}
		if _, is := held.Agent("run"); is {
			t.Errorf("%s: an empty dictionary answered about run", one.what)
		}
		if held.Known("person") {
			t.Errorf("%s: an empty dictionary claims to know person", one.what)
		}
	}

	for _, one := range []struct {
		what  string
		asset string
	}{
		{"an entry before any section", "# forge words 1\nperson\tpeople\n"},
		{"a section nothing reads", "# forge words 1\n\n[plurals]\nperson\tpeople\n"},
		{"an entry with three fields", "# forge words 1\n\n[plural]\nperson\tpeople\tfolk\n"},
		{"an entry with an empty field", "# forge words 1\n\n[plural]\nperson\t\n"},
	} {
		if _, err := words.Load([]byte(one.asset)); !errors.Is(err, words.ErrAsset) {
			t.Errorf("%s: Load = %v, want an asset failure", one.what, err)
		}
	}
}

// Trailing whitespace is invisible in a diff, and a carriage return is what a
// checkout on another platform can leave behind. Neither may become part of a
// word.
func TestTheEdgesOfALineAreTrimmed(t *testing.T) {
	held, err := words.Load([]byte("# forge words 1\r\n\r\n[plural]\r\nperson\tpeople  \r\n"))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}

	if got, is := held.Plural("person"); !is || got != "people" {
		t.Errorf("Plural(person) = %q, %v; want people", got, is)
	}
	if _, is := held.Singular("people"); !is {
		t.Error("the derived singular carried the whitespace across")
	}
}

// sample is the file in miniature: every section, and the shared plural that
// the derivation has to break a tie over.
const sample = `# forge words 1 agid=test sha256=none plurals=4 agents=1 vocabulary=2 converter=1
#
# A comment, which is most of what the head of the real file is.

[plural]
alumnus	alumni
base	bases
basis	bases
person	people

[agent]
run	runner

[vocabulary]
address
analysis
`
