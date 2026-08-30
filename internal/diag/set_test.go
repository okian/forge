package diag_test

import (
	"errors"
	"fmt"
	"go/token"
	"slices"
	"testing"

	"github.com/okian/forge/internal/diag"
)

// Codes used only by these tests, spread across ranges so that ordering by code
// is exercised alongside ordering by position.
var (
	codeSubjectIsPointer = diag.Register(2010, "subject is a pointer")
	codeUnknownOption    = diag.Register(3010, "unknown option key")
	codeNameCollision    = diag.Register(4010, "generated name collides with an existing declaration")
)

// at is shorthand for a position in a fixture file.
func at(file string, line, column int) token.Position {
	return token.Position{Filename: file, Line: line, Column: column}
}

func TestSetRender(t *testing.T) {
	var set diag.Set

	// Added out of order, and with two diagnostics sharing a position, so that
	// the rendering exercises every tiebreak.
	set.Add(diag.New(codeNameCollision, at("model/spec.go", 12, 6), "generated type PersonsSeq collides with an existing declaration").
		WithHint("rename it with //forge:collection seq=..."))
	set.Add(diag.New(codeUnknownOption, at("model/spec.go", 9, 1), "unknown option key %q for layer %s", "sorted", "collection").
		WithHint("did you mean sort?"))
	set.Add(diag.New(codeSubjectIsPointer, at("model/spec.go", 12, 6), "subject *Person is a pointer").
		WithStack("Collection[Person]", "           ^^^^^^").
		WithHint("declare the stack over Person and let the container hold values"))
	set.Add(diag.New(codeUnknownOption, at("catalog/spec.go", 4, 6), "unknown option key %q for layer %s", "capacity", "ring").
		WithHint("did you mean cap?"))

	checkGolden(t, "set", set.Render())
}

// Report order has to be total, or two runs over the same inputs print the same
// diagnostics in different sequences.
func TestSetAllIsOrderedByPositionThenCodeThenMessage(t *testing.T) {
	var set diag.Set
	// Offset separates two positions that agree on line and column, which happens
	// when a file is parsed with a different base.
	offset := at("a.go", 2, 1)
	offset.Offset = 40
	set.Add(diag.New(codeSubjectIsPointer, offset, "same position, later offset"))
	set.Add(diag.New(codeUnknownOption, at("b.go", 1, 1), "second file"))
	set.Add(diag.New(codeNameCollision, at("a.go", 9, 1), "later line"))
	set.Add(diag.New(codeUnknownOption, at("a.go", 2, 5), "later column"))
	set.Add(diag.New(codeSubjectIsPointer, at("a.go", 2, 1), "same position, lower code"))
	set.Add(diag.New(codeUnknownOption, at("a.go", 2, 1), "same position, zebra message"))
	set.Add(diag.New(codeUnknownOption, at("a.go", 2, 1), "same position, alpha message"))

	want := []string{
		"same position, lower code",
		"same position, alpha message",
		"same position, zebra message",
		"same position, later offset",
		"later column",
		"later line",
		"second file",
	}

	all := set.All()
	if len(all) != len(want) {
		t.Fatalf("All returned %d diagnostics, want %d", len(all), len(want))
	}
	for i, message := range want {
		if all[i].Message != message {
			t.Errorf("All()[%d] = %q, want %q", i, all[i].Message, message)
		}
	}
}

// Sorting must not disturb the set itself, or a second call could report a
// different order from the first.
func TestSetAllDoesNotDisturbTheSet(t *testing.T) {
	var set diag.Set
	set.Add(diag.New(codeUnknownOption, at("b.go", 1, 1), "second"))
	set.Add(diag.New(codeUnknownOption, at("a.go", 1, 1), "first"))

	first := set.All()
	first[0] = diag.New(codeUnknownOption, at("z.go", 1, 1), "overwritten by the caller")

	second := set.All()
	if second[0].Message != "first" {
		t.Errorf("All()[0] = %q after the caller modified an earlier result, want %q", second[0].Message, "first")
	}
}

func TestSetCounting(t *testing.T) {
	var set diag.Set
	if !set.Empty() || set.Len() != 0 {
		t.Errorf("a new set reports Len=%d Empty=%v, want 0 and true", set.Len(), set.Empty())
	}

	set.Add(diag.New(codeUnknownOption, at("a.go", 1, 1), "something"))
	if set.Empty() || set.Len() != 1 {
		t.Errorf("after one Add, Len=%d Empty=%v, want 1 and false", set.Len(), set.Empty())
	}

	// The zero value is the empty set, so nothing has to construct one.
	var zero diag.Set
	if !zero.Empty() || zero.Len() != 0 || zero.All() != nil || zero.Render() != "" {
		t.Error("the zero value does not read as an empty set")
	}
}

func TestSetAddError(t *testing.T) {
	var set diag.Set

	reported := diag.New(codeSubjectIsPointer, at("a.go", 1, 1), "subject is a pointer")
	if !set.AddError(fmt.Errorf("resolving Persons: %w", error(reported))) {
		t.Error("AddError refused a wrapped diagnostic")
	}
	if set.Len() != 1 {
		t.Errorf("Len = %d after adding one diagnostic, want 1", set.Len())
	}

	// An error that is not a diagnostic has earned no position and no code, so
	// it is left for the caller rather than dressed up as one.
	if set.AddError(errors.New("disk full")) {
		t.Error("AddError accepted an ordinary error")
	}
	if set.Len() != 1 {
		t.Errorf("Len = %d after refusing an ordinary error, want 1", set.Len())
	}
}

func TestSetExitCode(t *testing.T) {
	var set diag.Set
	if got := set.ExitCode(); got != diag.ExitOK {
		t.Errorf("an empty set exits %d, want %d", got, diag.ExitOK)
	}

	set.Add(diag.New(codeUnknownOption, at("a.go", 1, 1), "unknown option key %q", "sorted"))
	if got := set.ExitCode(); got != diag.ExitDiagnostics {
		t.Errorf("a non-empty set exits %d, want %d", got, diag.ExitDiagnostics)
	}
}

// Work that fans out over packages gives each goroutine its own set. Merging
// has to leave the report order alone, or the output would depend on which
// goroutine finished first.
func TestSetMergeIsOrderIndependent(t *testing.T) {
	build := func() (*diag.Set, *diag.Set) {
		var first, second diag.Set
		first.Add(diag.New(codeNameCollision, at("b.go", 3, 1), "from the second package"))
		first.Add(diag.New(codeUnknownOption, at("a.go", 9, 1), "from the second package, earlier file"))
		second.Add(diag.New(codeSubjectIsPointer, at("a.go", 1, 1), "from the first package"))
		return &first, &second
	}

	forward, backward := diag.Set{}, diag.Set{}

	a, b := build()
	forward.Merge(a)
	forward.Merge(b)

	c, d := build()
	backward.Merge(d)
	backward.Merge(c)

	if forward.Render() != backward.Render() {
		t.Errorf("merge order changed the report:\n--- forward ---\n%s\n--- backward ---\n%s",
			forward.Render(), backward.Render())
	}
	if forward.Len() != 3 {
		t.Errorf("Len = %d after merging three diagnostics, want 3", forward.Len())
	}

	// Merging nothing is not an error; a package that reported nothing still
	// hands its set back.
	before := forward.Len()
	forward.Merge(nil)
	if forward.Len() != before {
		t.Errorf("Len = %d after merging nil, want %d", forward.Len(), before)
	}
}

// Two diagnostics about one declaration commonly agree on position, code and
// message and differ only in the caret or the hint. If those are not tiebreaks,
// the order they render in is the order they happened to be recorded.
func TestSetOrderDoesNotDependOnInsertionOrder(t *testing.T) {
	pos := at("model/spec.go", 12, 6)

	build := func(reversed bool) string {
		diagnostics := []diag.Diagnostic{
			diag.New(codeNameCollision, pos, "duplicate layer in stack").
				WithStack("Collection[Json[Json[Person]]]", "                ^^^^"),
			diag.New(codeNameCollision, pos, "duplicate layer in stack").
				WithStack("Collection[Json[Json[Person]]]", "           ^^^^"),
			diag.New(codeNameCollision, pos, "duplicate layer in stack").
				WithHint("drop the inner one"),
			diag.New(codeNameCollision, pos, "duplicate layer in stack").
				WithHint("drop the outer one"),
		}
		if reversed {
			slices.Reverse(diagnostics)
		}

		var set diag.Set
		for _, d := range diagnostics {
			set.Add(d)
		}
		return set.Render()
	}

	if forward, backward := build(false), build(true); forward != backward {
		t.Errorf("report order depends on insertion order:\n--- forward ---\n%s\n--- reversed ---\n%s",
			forward, backward)
	}
}

// The three statuses have to stay distinct: a caller that cannot tell a bad
// command line from bad input cannot report either one usefully.
func TestExitCodesAreDistinct(t *testing.T) {
	codes := map[int]string{
		diag.ExitOK:          "ok",
		diag.ExitDiagnostics: "diagnostics",
		diag.ExitUsage:       "usage",
	}
	if len(codes) != 3 {
		t.Errorf("exit statuses collide: %v", codes)
	}
}
