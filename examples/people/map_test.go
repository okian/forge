package people

import (
	"bytes"
	"testing"
)

// applicantFixture is one applicant with every member set, so a mapping that
// dropped one would show as a difference rather than as two zeros agreeing.
func applicantFixture() Applicant {
	return Applicant{
		ID:      7,
		Name:    "Ada",
		Contact: "ada@example.com",
		Age:     36,
		Aliases: []string{"al", "ada l"},
	}
}

// The constructor moves every member, the pinned one included, and the fused
// writers put the same bytes on the wire as building the Person and encoding
// it — which is the whole of what the declaration promises.
func TestTheWireAgreesWithConstructThenEncode(t *testing.T) {
	src := applicantFixture()

	held := PersonFromApplicant(&src)
	if held.Email != src.Contact {
		t.Errorf("Email = %q, want the pinned Contact %q", held.Email, src.Contact)
	}

	want, wantErr := held.AppendJSON(nil)
	got, gotErr := AppendPersonJSONFromApplicant(nil, &src)
	if (gotErr == nil) != (wantErr == nil) {
		t.Fatalf("fused err = %v, construct-then-encode err = %v", gotErr, wantErr)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("fused wrote %s, want %s", got, want)
	}

	var buf bytes.Buffer
	n, err := WritePersonJSONFromApplicant(&buf, &src)
	if err != nil || n != int64(len(want)) || buf.String() != string(want) {
		t.Errorf("Write wrote %q (%d, %v), want %q", buf.String(), n, err, want)
	}
}
