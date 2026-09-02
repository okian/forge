package words

import "testing"

// What the text form costs to turn into the shape a lookup wants.
//
// It is paid once per process, and only by a process that resolves a name the
// rules cannot answer on their own — so what matters is that it is small
// against the work of a forge run, not that it is smaller than reading a blob.
func BenchmarkLoadTheDictionary(b *testing.B) {
	held := asset

	b.ReportAllocs()
	b.SetBytes(int64(len(held)))
	for b.Loop() {
		if _, err := Load(held); err != nil {
			b.Fatal(err)
		}
	}
}

// And that a hit still answers with a slice of the blob rather than a copy of
// it, which is the property the whole layout exists for and the one a parse
// step could quietly lose.
func TestALookupAllocatesNothing(t *testing.T) {
	held, err := Load(asset)
	if err != nil {
		t.Fatal(err)
	}

	for _, one := range []struct {
		what string
		call func()
	}{
		{"Plural", func() { held.Plural("Person") }},
		{"Singular", func() { held.Singular("People") }},
		{"Agent", func() { held.Agent("run") }},
		{"Known", func() { held.Known("address") }},
		{"a miss", func() { held.Plural("Widget") }},
	} {
		if got := testing.AllocsPerRun(200, one.call); got != 0 {
			t.Errorf("%s allocates %.1f times per call, want 0", one.what, got)
		}
	}
}
