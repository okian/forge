package racetest_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/okian/forge/internal/racetest/matrix"
)

// TestWhatTheMatrixCommitsRuns round-trips a value through the committed
// artifact, which the stress tests beside it never do.
//
// They hold the lock to the detector — writers and readers contending is what
// they are for — and every route they take is a writing or walking one, so the
// reading half of the committed file is code an ordinary run only compiles.
// Compiling is the weaker claim: a committed artifact is the file users get,
// and this is the one place the repository runs it rather than the layer's own
// fixtures. The subject is deliberately dull, so what is exercised is the
// machinery every subject shares — the scanner, the names, the escapes — not
// anything about a Person.
func TestWhatTheMatrixCommitsRuns(t *testing.T) {
	held := matrix.Person{ID: 7, Name: "ada"}

	written, err := json.Marshal(held)
	if err != nil {
		t.Fatalf("the committed codec would not write %#v: %v", held, err)
	}

	var back matrix.Person
	if err := back.UnmarshalJSON(written); err != nil {
		t.Fatalf("the committed codec would not read %s back: %v", written, err)
	}
	if back != held {
		t.Errorf("wrote %#v and read %#v", held, back)
	}

	var borrowed matrix.Person
	if err := borrowed.UnmarshalJSONBorrowed(written); err != nil {
		t.Fatalf("the borrowing read refused %s: %v", written, err)
	}
	if borrowed != held {
		t.Errorf("wrote %#v and borrowed %#v", held, borrowed)
	}

	// A name that arrives escaped is still the name, and a value with an
	// escape in it comes back as the character it spells.
	var escaped matrix.Person
	if err := escaped.UnmarshalJSON([]byte(`{"ID":3,"Name":"a\tb"}`)); err != nil {
		t.Fatalf("an escaped member name was refused: %v", err)
	}
	if escaped.ID != 3 || escaped.Name != "a\tb" {
		t.Errorf("the escaped document read as %#v", escaped)
	}

	// And the container: what the lock writes is the snapshot as one array,
	// each element through the codec above.
	people := matrix.NewGuardedPersons()
	people.Do(func(v matrix.GuardedPersonsView) {
		v.Push(matrix.Person{ID: 1, Name: "grace"})
		v.Push(held)
	})
	doc, err := json.Marshal(people)
	if err != nil {
		t.Fatalf("the committed container codec would not write: %v", err)
	}
	if string(doc) != `[{"ID":1,"Name":"grace"},{"ID":7,"Name":"ada"}]` {
		t.Errorf("the container wrote %s", doc)
	}
}

// TestWhatTheMatrixCommitsRefuses holds the committed reader to a sample of
// the documents it must not accept, one per way a document goes wrong: the
// grammar, the names, the escapes, UTF-8, the value kinds, the widths, and the
// depth bound.
func TestWhatTheMatrixCommitsRefuses(t *testing.T) {
	for _, doc := range []string{
		``,
		`{`,
		`{"ID":}`,
		`{"ID":1,"ID":2}`,
		`{"ID":"7"}`,
		`{"ID":1.5}`,
		`{"ID":9223372036854775808}`,
		`{"Name":"unterminated`,
		`{"Name":"bad \z escape"}`,
		`{"Name":"lone \ud800 surrogate"}`,
		"{\"Name\":\"bad \xff utf8\"}",
		`{"ID":1} trailing`,
		`[{"ID":1}]`,
		`{"Zed":` + strings.Repeat(`{"x":`, 10001) + `1` + strings.Repeat(`}`, 10002),
	} {
		var into matrix.Person
		if err := into.UnmarshalJSON([]byte(doc)); err == nil {
			t.Errorf("the committed codec read %q", doc)
		}
	}
}
