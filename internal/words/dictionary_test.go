package words_test

import (
	"compress/flate"
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

	if _, is := words.English().Plural("person"); !is {
		t.Error("the embedded dictionary does not know person")
	}
	if words.English().Known("cuju") {
		t.Error("the embedded dictionary claims to know cuju")
	}
}

// An asset this package cannot read is a broken build rather than a bad input,
// and every rule works without one — so it loads as an empty dictionary and the
// names come out regular.
func TestAnAssetThatIsNotOne(t *testing.T) {
	for _, one := range []struct {
		what  string
		asset []byte
	}{
		{"nothing at all", nil},
		{"a provenance line and no body", []byte("forge words 1\n")},
	} {
		held, err := words.Load(one.asset)
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
		asset []byte
	}{
		{"a body that is not deflated", []byte("forge words 1\nnot deflated at all")},
		{"a body with the wrong magic", append([]byte("forge words 1\n"), deflated(t, "NOPE")...)},
		{"a body that stops mid-table", append([]byte("forge words 1\n"), deflated(t, "FWD1")...)},
	} {
		if _, err := words.Load(one.asset); !errors.Is(err, words.ErrAsset) {
			t.Errorf("%s: Load = %v, want an asset failure", one.what, err)
		}
	}
}

// deflated returns a body the loader will decompress.
func deflated(t *testing.T, text string) []byte {
	t.Helper()

	var out strings.Builder
	held := flateWriter(t, &out)

	if _, err := held.Write([]byte(text)); err != nil {
		t.Fatalf("deflating: %v", err)
	}
	if err := held.Close(); err != nil {
		t.Fatalf("deflating: %v", err)
	}
	return []byte(out.String())
}

// flateWriter is the compressor the asset is written with, kept beside the test
// that needs it rather than imported at the top of a file about words.
func flateWriter(t *testing.T, into *strings.Builder) *flate.Writer {
	t.Helper()

	held, err := flate.NewWriter(into, flate.BestCompression)
	if err != nil {
		t.Fatalf("deflating: %v", err)
	}
	return held
}
