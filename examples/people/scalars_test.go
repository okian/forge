package people_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/okian/forge/examples/people"
)

// A person reads as the display tags on its fields ask.
//
// The tags are the whole of the decision. Two of the five fields carry one, one
// of those carries a name, and what comes out is those two in the order they
// were declared with the labelled one labelled.
func TestHowAPersonReads(t *testing.T) {
	held := people.Person{ID: 7, Name: "Ada", Email: "ada@example.com", Age: 36}

	if got, want := held.String(), "Ada age=36"; got != want {
		t.Errorf("a person reads as %q, want %q", got, want)
	}
}

// And what is not tagged is not in it.
//
// Worth its own test because the failure is quiet: a rendering that included
// every field would look reasonable, pass any eye test, and put an email
// address in every log line that printed a person.
func TestWhatADisplayLeavesOut(t *testing.T) {
	held := people.Person{ID: 7, Name: "Ada", Email: "ada@example.com", Age: 36}

	for _, gone := range []string{"ada@example.com", "7"} {
		if strings.Contains(held.String(), gone) {
			t.Errorf("a field with no display tag reads anyway: %q holds %q", held.String(), gone)
		}
	}
}

// A person logs without its email.
//
// Through a real handler rather than by reading the slog.Value, because what is
// being tested is what reaches the output: a LogValue that built the right
// value and was never consulted would pass the narrower test and fail the only
// one that matters.
func TestWhatAPersonLogs(t *testing.T) {
	var out bytes.Buffer

	log := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{
		// The time and level are noise here, and the time makes the output
		// different on every run.
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		},
	}))

	log.Info("seen", "person", people.Person{ID: 7, Name: "Ada", Email: "ada@example.com", Age: 36})

	held := out.String()

	if strings.Contains(held, "ada@example.com") {
		t.Errorf("a redacted field reached the log: %s", held)
	}
	if !strings.Contains(held, "[redacted]") {
		t.Errorf("the field is missing rather than redacted: %s", held)
	}

	// The rest is still there, so redaction took one field rather than the
	// record.
	for _, want := range []string{"Ada", "36"} {
		if !strings.Contains(held, want) {
			t.Errorf("the log does not carry %q: %s", want, held)
		}
	}
}
