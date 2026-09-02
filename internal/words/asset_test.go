package words

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

// The committed dictionary, held to the shape it promises.
//
// It is a file people edit — that is the whole reason it is text — so what the
// loader is lenient about is checked here instead. A word inserted out of order
// still works, and a word inserted out of order in the committed file is a diff
// that will be painful next release; a duplicate answers once and hides the
// other; a word with an upper-case letter can never be found, because every
// lookup folds the question and not the key.
func TestTheCommittedDictionaryIsWellFormed(t *testing.T) {
	if !utf8.Valid(asset) {
		t.Fatal("the dictionary is not valid UTF-8")
	}

	section := ""
	seen := map[string]map[string]bool{}
	last := map[string]string{}
	count := map[string]int{}

	for line := range lines(asset) {
		if name, is := header(line); is {
			section = string(name)
			seen[section] = map[string]bool{}
			continue
		}
		if section == "" {
			t.Fatalf("%q comes before any section", line)
		}

		word := string(line)
		if at := strings.IndexByte(word, '\t'); at >= 0 {
			word = word[:at]
		}
		count[section]++

		if seen[section][word] {
			t.Errorf("[%s] carries %q twice", section, word)
		}
		seen[section][word] = true

		if held := last[section]; held != "" && word < held {
			t.Errorf("[%s] has %q after %q, which is out of order", section, word, held)
		}
		last[section] = word

		if word != strings.ToLower(word) {
			t.Errorf("[%s] carries %q, which no lookup can reach: keys are folded, questions are not", section, word)
		}
		if strings.TrimSpace(word) != word {
			t.Errorf("[%s] carries %q, which has whitespace at an edge", section, word)
		}
	}

	for _, want := range []string{"plural", "agent", "vocabulary"} {
		if count[want] == 0 {
			t.Errorf("the dictionary has no [%s] section, or an empty one", want)
		}
	}
	if _, held := seen["singular"]; held {
		t.Error("the dictionary writes a singular section, which the loader derives")
	}
}

// And that the file the repository holds is the one the tests above ran
// against: a dictionary that stopped loading would make every test in this
// package pass against an empty one.
func TestTheCommittedDictionaryLoads(t *testing.T) {
	held, err := Load(asset)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}

	for _, one := range []struct{ word, want string }{
		{"person", "people"},
		{"child", "children"},
		{"datum", "data"},
		{"alias", "aliases"},
	} {
		if got, is := held.Plural(one.word); !is || got != one.want {
			// alias is regular, so the dictionary is right not to carry it.
			if one.word == "alias" && !is {
				continue
			}
			t.Errorf("Plural(%q) = %q, %v; want %q", one.word, got, is, one.want)
		}
	}

	if got, is := held.Singular("people"); !is || got != "person" {
		t.Errorf("Singular(people) = %q, %v; want person", got, is)
	}

	// The provenance line is a comment, so it must not have become an entry.
	if held.Known("#") || held.Known("forge") {
		t.Error("the comment at the head of the file was read as words")
	}
}

// The blob every table points into is one string, and the singulars point into
// the plurals' half of it. That is what makes the derived table cost nothing,
// and it is the kind of thing a later change quietly loses.
func TestTheTablesShareOneBlob(t *testing.T) {
	held, err := Load(asset)
	if err != nil {
		t.Fatal(err)
	}

	one, is := held.(*embedded)
	if !is {
		t.Fatalf("Load returned %T", held)
	}

	blob := one.tables[0].blob
	for at := range sections {
		if one.tables[at].blob != blob {
			t.Errorf("table %d holds a blob of its own", at)
		}
	}

	if got := len(blob); got < 60_000 {
		t.Errorf("the blob is %d bytes, which is too few to be the dictionary", got)
	}
	if bytes.Contains([]byte(blob), []byte{'\t'}) || bytes.Contains([]byte(blob), []byte{'\n'}) {
		t.Error("the blob carries the file's punctuation, so a lookup can return it")
	}
}
