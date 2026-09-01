package people_test

import (
	"bytes"
	json "encoding/json/v2"
	"slices"
	"testing"

	"github.com/okian/forge/examples/people"
)

// The container reads exactly the documents the standard library reads, and
// makes the same values out of them.
//
// A reader is where a codec is dangerous. What it is handed comes from outside
// the program, so every branch of it is reachable by somebody who is not the
// author — and a reader that accepts a document the rest of the world rejects,
// or builds a different value from one everybody else agrees on, is a hole
// nothing written by hand would find. The other implementation is the oracle:
// where the two disagree, one of them is wrong, and this one is the new one.
//
// Three claims, in the order they matter. It does not stop the program: a panic
// on input is a denial of service, and the container is bounded so a large
// document cannot be one either. It agrees about whether the document is valid.
// And where both accept it, they agree about what it says.
//
// A document longer than the container is where they legitimately differ — the
// ring keeps the last of it and a slice keeps all of it — so what the container
// holds is compared against the tail of what reflection read rather than
// against the whole of it.
func FuzzTheContainerReadsWhatReflectionReads(f *testing.F) {
	for _, seed := range []string{
		"", " ", "null", "[]", "[ ]", "[null]", "[{}]", "{}", "[",
		`[{"ID":1}`, `[{"ID":1},]`, `[{"ID":1} {"ID":2}]`,
		`[{"ID":1,"Name":"a","Email":"b","Age":2}]`,
		`[{"ID":-1,"Name":"","Email":"","Age":0},{"ID":2,"Name":" ","Email":"\\","Age":-3}]`,
		`[{"Unknown":1,"ID":2}]`,
		`[{"ID":1,"ID":2}]`,
		`[{"ID":1.5}]`,
		`[{"ID":"1"}]`,
		`[{"Name":"\ud800"}]`,
		`[1,2,3]`,
		`[[]]`,
		`["a"]`,
		`[{"ID":9223372036854775807},{"ID":-9223372036854775808}]`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, document []byte) {
		held := people.NewRecent()
		ours := json.Unmarshal(document, held)

		var theirs []twin
		reflected := json.Unmarshal(document, &theirs)

		if (ours == nil) != (reflected == nil) {
			t.Fatalf("the container %s and reflection %s for %q\n\tcontainer:  %v\n\treflection: %v",
				verdict(ours), verdict(reflected), document, ours, reflected)
		}
		if ours != nil {
			return
		}

		// A document longer than the container is where the two legitimately
		// differ: the ring keeps the last of it and a slice keeps all of it.
		// Compared against the tail rather than skipped, because skipping would
		// leave every long document unchecked — and a long document is exactly
		// what a fuzzer produces once it has learnt what an element looks like.
		want := theirs
		if len(want) > held.Cap() {
			want = want[len(want)-held.Cap():]
		}

		if got := twins(held); !slices.Equal(got, want) {
			t.Fatalf("the container and reflection read %q differently\n\tcontainer:  %v\n\treflection: %v",
				document, got, want)
		}

		// And what was read writes back as what the same elements write, so a
		// document that arrived from somewhere else leaves in the shape the
		// rest of the world expects.
		ourBytes, err := json.Marshal(held)
		if err != nil {
			t.Fatalf("marshaling what was read from %q: %v", document, err)
		}
		theirBytes, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshaling reflectively what was read from %q: %v", document, err)
		}
		if !bytes.Equal(ourBytes, theirBytes) {
			t.Fatalf("re-writing %q differs\n\tcontainer:  %s\n\treflection: %s",
				document, ourBytes, theirBytes)
		}
	})
}

// The subject's own codec reads exactly what the standard library reads.
//
// The container's fuzz above covers the array around the elements; this covers
// the object inside one, which is where the tags, the names and the number
// conversions are. They are separate targets because a corpus that has to get
// past a bracket to reach a member spends its budget on brackets.
func FuzzTheSubjectReadsWhatReflectionReads(f *testing.F) {
	for _, seed := range []string{
		"", "null", "{}", "{ }", "[]", `{"ID":1}`, `{"ID":1,`,
		`{"ID":1,"Name":"a","Email":"b","Age":2}`,
		`{"Unknown":true}`, `{"ID":1,"ID":2}`, `{"ID":1.0}`, `{"ID":"1"}`,
		`{"Name":null}`, `{"Name":"é"}`, `{"Age":-0}`,
		`{"ID":9223372036854775807}`, `{"ID":18446744073709551616}`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, document []byte) {
		var held people.Person
		ours := json.Unmarshal(document, &held)

		var reflectedInto twin
		reflected := json.Unmarshal(document, &reflectedInto)

		if (ours == nil) != (reflected == nil) {
			t.Fatalf("the subject %s and reflection %s for %q\n\tsubject:    %v\n\treflection: %v",
				verdict(ours), verdict(reflected), document, ours, reflected)
		}
		if ours == nil && held != people.Person(reflectedInto) {
			t.Fatalf("the subject and reflection read %q differently\n\tsubject:    %+v\n\treflection: %+v",
				document, held, reflectedInto)
		}
	})
}

// verdict says what a reader did with a document, for a message a person reads.
func verdict(err error) string {
	if err == nil {
		return "read"
	}
	return "refused"
}
