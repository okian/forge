package emit_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/emit"
)

// The same inputs reduce to the same fingerprint however they were collected.
// A caller gathers them by walking packages and layers, so the order they
// arrive in is not the order anything else will produce.
func TestDigestDoesNotDependOnOrder(t *testing.T) {
	var forward, backward emit.Digest

	forward.AddString("model/person.go", "type Person struct{}")
	forward.AddString("forge", "v0.1.0")
	forward.AddString("model/spec.go", "type Persons Collection[Person]")

	backward.AddString("model/spec.go", "type Persons Collection[Person]")
	backward.AddString("model/person.go", "type Person struct{}")
	backward.AddString("forge", "v0.1.0")

	if forward.String() != backward.String() {
		t.Errorf("one set of inputs gave %s and %s", forward.String(), backward.String())
	}
	if got, want := forward.Len(), 3; got != want {
		t.Errorf("Len() = %d, want %d", got, want)
	}
}

// Including two inputs recorded under one name, which is a thing the type
// allows and so a thing the fingerprint has to survive. Ordering by name alone
// would leave the answer depending on which was recorded first.
func TestDigestDoesNotDependOnOrderUnderOneName(t *testing.T) {
	var forward, backward emit.Digest

	forward.AddString("layer", "collection")
	forward.AddString("layer", "ring")

	backward.AddString("layer", "ring")
	backward.AddString("layer", "collection")

	if forward.String() != backward.String() {
		t.Errorf("one set of inputs gave %s and %s", forward.String(), backward.String())
	}
}

// Reading the fingerprint does not consume what it read, because a run writes
// several files from overlapping inputs.
func TestDigestCanBeReadTwice(t *testing.T) {
	var subject emit.Digest
	subject.AddString("forge", "v0.1.0")

	if first, second := subject.String(), subject.String(); first != second {
		t.Errorf("read twice gave %s and %s", first, second)
	}
}

// Anything that changes has to change the fingerprint, or a stale file is
// reported fresh — which is worse than not checking, because it is trusted.
func TestDigestChangesWithItsInputs(t *testing.T) {
	base := func() *emit.Digest {
		d := &emit.Digest{}
		d.AddString("model/person.go", "type Person struct{}")
		return d
	}

	unchanged := base().String()

	cases := map[string]func(*emit.Digest){
		"different content": func(d *emit.Digest) { d.AddString("model/other.go", "x") },
		"different name": func(d *emit.Digest) {
			*d = emit.Digest{}
			d.AddString("model/renamed.go", "type Person struct{}")
		},
		"content moved to another name": func(d *emit.Digest) {
			*d = emit.Digest{}
			d.AddString("model/person.go", "")
			d.AddString("", "type Person struct{}")
		},
	}

	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			changed := base()
			change(changed)

			if changed.String() == unchanged {
				t.Errorf("the fingerprint did not change: %s", unchanged)
			}
		})
	}
}

// Two inputs must not be able to run together into a third that nothing
// produced, which is what folding their lengths in prevents.
func TestDigestCannotBeConfusedByRuns(t *testing.T) {
	var split, joined emit.Digest

	split.AddString("a", "bc")
	joined.AddString("ab", "c")

	if split.String() == joined.String() {
		t.Errorf("two different inputs share the fingerprint %s", split.String())
	}
}

// The fingerprint goes into a header a human reads, so it is short and it is
// hex.
func TestDigestIsShortAndPrintable(t *testing.T) {
	var subject emit.Digest
	subject.AddString("forge", "v0.1.0")

	got := subject.String()
	if len(got) != 16 {
		t.Errorf("fingerprint %q is %d characters", got, len(got))
	}
	if strings.Trim(got, "0123456789abcdef") != "" {
		t.Errorf("fingerprint %q is not hex", got)
	}

	// A digest of nothing is still a fingerprint, since a file made from
	// nothing is a file that never changes.
	if empty := (&emit.Digest{}).String(); len(empty) != 16 {
		t.Errorf("the empty fingerprint is %q", empty)
	}
}

// A caller that keeps the slice it handed over must not be able to change what
// was recorded from it afterwards.
func TestDigestCopiesWhatItIsGiven(t *testing.T) {
	content := []byte("type Person struct{}")

	var subject emit.Digest
	subject.Add("model/person.go", content)
	before := subject.String()

	content[0] = 'X'

	if after := subject.String(); after != before {
		t.Errorf("the fingerprint changed from %s to %s when the caller edited its own slice", before, after)
	}
}
