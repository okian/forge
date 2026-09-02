package plugin_test

import (
	"go/token"
	"strings"
	"testing"

	"github.com/okian/forge/plugin"
)

// The codes these tests report under, registered at package scope.
//
// Where the documentation says to put them, and the only place they work: a
// code is claimed once per process, so registering inside a test function
// panics the second time the test runs — which `go test -count=2` does, and
// which a test demonstrating the documented way should not do.
var (
	documented = plugin.Register(6001, "the example in the documentation")
	collected  = plugin.Register(6003, "two things wrong with one declaration")
)

// The documented example works: a code above forge's own registers, builds a
// diagnostic, and renders under its own number.
//
// The first line a layer author writes by the book, and the one that used to
// end the process: forge's own ranges stopped at 5999 and everything above was
// refused, so following the instruction panicked at package initialisation.
func TestALayersOwnCode(t *testing.T) {
	if got := documented.String(); got != "FRG6001" {
		t.Errorf("the code prints as %s, want FRG6001", got)
	}

	held := plugin.New(documented, token.Position{Filename: "spec.go", Line: 8, Column: 6},
		"%s is not something this layer can generate for", "Persons").
		WithHint("write %s instead", "by=Field")

	rendered := held.Error()
	for _, want := range []string{
		"spec.go:8:6", "FRG6001",
		"Persons is not something this layer can generate for",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the diagnostic does not carry %q:\n%s", want, rendered)
		}
	}

	// The hint is beside the message rather than in it: one line says what is
	// wrong and the other says what to do, and a report puts them on two lines
	// under one position.
	if got := held.Hint; got != "write by=Field instead" {
		t.Errorf("the hint is %q, want the one the layer wrote", got)
	}

	// And it comes back out of an error, which is how a layer returns one from
	// Generate.
	back, is := plugin.From(held)
	if !is || back.Code != documented {
		t.Errorf("the diagnostic did not survive being returned as an error: %v, %v", back.Code, is)
	}
}

// A code says whether it is forge's own, which is what tells a reader whose
// index to look in.
func TestWhoseCodeItIs(t *testing.T) {
	for code, ours := range map[plugin.Code]bool{
		1000: true, 5999: true,
		6000: false, 9999: false,
	} {
		if got := code.Ours(); got != ours {
			t.Errorf("Code(%d).Ours() = %v, want %v", int(code), got, ours)
		}
	}
}

// Diagnostics collect, so a layer reports everything wrong with a declaration
// rather than the first thing.
func TestDiagnosticsCollect(t *testing.T) {
	var held plugin.Diagnostics
	if !held.Empty() {
		t.Error("a set with nothing in it says it holds something")
	}

	at := token.Position{Filename: "spec.go", Line: 3}
	held.Add(plugin.New(collected, at, "the first thing"))
	held.Add(plugin.New(collected, at, "the second thing"))

	if got := len(held.All()); got != 2 {
		t.Errorf("the set holds %d, want both", got)
	}
	if err := held.Err(); err == nil {
		t.Error("a set holding two things reports no error")
	}
}

// The three ways a Go name is written, and what each is for.
//
// Held against the documented examples rather than against the implementation,
// because the documentation is what a layer author reads and the two disagreeing
// is how a wire member ends up spelled one way by one layer and another way by
// the next.
//
// The initialisms are the cases that separate them. Camel reads a word at a
// time, so ID becomes id; Lower reads a letter, so the same name becomes iD.
// One of those is a name a document carries and the other is a fragment of an
// identifier, and mixing them up is the mistake this is written to catch.
func TestHowANameIsWritten(t *testing.T) {
	for _, one := range []struct {
		name           string
		camel, low, up string
	}{
		{"ID", "id", "iD", "ID"},
		{"Name", "name", "name", "Name"},
		{"StatusOK", "statusOK", "statusOK", "StatusOK"},
		{"JSONValue", "jsonValue", "jSONValue", "JSONValue"},
		{"city", "city", "city", "City"},
	} {
		if got := plugin.Camel(one.name); got != one.camel {
			t.Errorf("Camel(%q) = %q, want %q", one.name, got, one.camel)
		}
		if got := plugin.Lower(one.name); got != one.low {
			t.Errorf("Lower(%q) = %q, want %q", one.name, got, one.low)
		}
		if got := plugin.Upper(one.name); got != one.up {
			t.Errorf("Upper(%q) = %q, want %q", one.name, got, one.up)
		}
	}
}

// A capability set holds what was put in it and answers about what was not.
func TestCapabilities(t *testing.T) {
	held := plugin.Caps(plugin.Sized, plugin.Streamable)

	if !held.Has(plugin.Sized) || !held.Has(plugin.Streamable) {
		t.Errorf("the set does not hold what it was given: %v", held.All())
	}
	if held.Has(plugin.Concurrent) {
		t.Error("the set holds a capability nobody put in it")
	}
	if got := len(plugin.Every().All()); got <= len(held.All()) {
		t.Error("the set of every capability is no larger than a set of two")
	}
}

// A comment wraps at the width generated files use, counted in runes.
//
// Runes rather than bytes, which is what the em dashes are for: a line of them
// is three bytes to the rune, so a wrapper counting bytes would break it at a
// third of the width and a reader would see comments ragged for no reason they
// could find.
func TestWrapping(t *testing.T) {
	held := plugin.Wrapped(strings.Repeat("word — ", 30), plugin.CommentWidth)

	if len(held) < 2 {
		t.Fatalf("a long line wrapped into %d", len(held))
	}
	for _, line := range held {
		if got := len([]rune(line)); got > plugin.CommentWidth {
			t.Errorf("a line is %d runes wide, want at most %d: %q", got, plugin.CommentWidth, line)
		}
	}
}
