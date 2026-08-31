package diag

import (
	"cmp"
	"errors"
	"slices"
	"strings"
)

// Process exit statuses. A run either succeeds, reports diagnostics, or was
// asked to do something that is not a command.
const (
	// ExitOK reports that the run succeeded and wrote whatever it was asked to.
	ExitOK = 0

	// ExitDiagnostics reports that the run completed and found problems. It is
	// not a crash: the diagnostics are the output.
	ExitDiagnostics = 1

	// ExitUsage reports that the command line itself was wrong, which is a
	// different failure from the input being wrong.
	ExitUsage = 2
)

// Set collects the diagnostics a run produces.
//
// Collecting rather than returning the first failure is deliberate: an author
// who has made three mistakes should learn about three mistakes, not be walked
// through them one build at a time.
//
// The zero value is ready to use. A Set is not safe for concurrent use: work
// that fans out over packages should give each goroutine its own set and
// [Set.Merge] them at the join, which is also the only way to keep the report
// order from depending on which goroutine finished first.
type Set struct {
	diagnostics []Diagnostic
}

// Add records a diagnostic.
func (s *Set) Add(d Diagnostic) { s.diagnostics = append(s.diagnostics, d) }

// AddError records the diagnostic err carries, and reports whether it carried
// one. An error that is not a diagnostic is left for the caller to handle,
// because turning an unexpected failure into a diagnostic would give it a
// position and an identifier it has not earned.
func (s *Set) AddError(err error) bool {
	d, ok := From(err)
	if ok {
		s.Add(d)
	}
	return ok
}

// Merge records everything in other. The result is ordered by [Set.All] like
// any other set, so merging in a different sequence changes nothing.
func (s *Set) Merge(other *Set) {
	if other == nil {
		return
	}
	s.diagnostics = append(s.diagnostics, other.diagnostics...)
}

// Len returns the number of diagnostics recorded.
func (s *Set) Len() int { return len(s.diagnostics) }

// Empty reports whether nothing has been recorded.
func (s *Set) Empty() bool { return len(s.diagnostics) == 0 }

// All returns the diagnostics in report order: by position, then by code, then
// by every part of the rendering, in the order those parts are printed.
//
// The ordering is total over everything a reader can see, so two diagnostics
// that sort equal also render identically and it does not matter which comes
// first. That is what makes a run's output depend on its inputs alone.
func (s *Set) All() []Diagnostic {
	sorted := slices.Clone(s.diagnostics)
	slices.SortStableFunc(sorted, compare)
	return sorted
}

// compare orders two diagnostics for reporting.
func compare(a, b Diagnostic) int {
	// Position first: a reader works through a file top to bottom.
	if c := strings.Compare(a.Pos.Filename, b.Pos.Filename); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Pos.Line, b.Pos.Line); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Pos.Column, b.Pos.Column); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Pos.Offset, b.Pos.Offset); c != 0 {
		return c
	}

	// Then everything that ends up on screen. Two diagnostics about one
	// declaration commonly share a position, a code and a message and differ
	// only in which layer the caret marks, so the caret has to be a tiebreak or
	// the two render in whichever order they happened to be recorded.
	if c := cmp.Compare(a.Code, b.Code); c != 0 {
		return c
	}
	if c := strings.Compare(a.Message, b.Message); c != 0 {
		return c
	}
	if c := strings.Compare(a.Stack, b.Stack); c != 0 {
		return c
	}
	if c := strings.Compare(a.Caret, b.Caret); c != 0 {
		return c
	}
	return strings.Compare(a.Hint, b.Hint)
}

// Render returns every diagnostic in report order, one block each, separated
// by newlines and with no trailing newline.
func (s *Set) Render() string {
	all := s.All()
	blocks := make([]string, len(all))
	for i, d := range all {
		blocks[i] = d.Render()
	}
	return strings.Join(blocks, "\n")
}

// Err returns the set as one error, and nil for a set holding nothing.
//
// It is the bridge from a stage that collects to an interface that returns a
// single error, which is what a layer's Generate is. A set holding one returns
// that one unwrapped, so the caller recovers it with [From] and reports it like
// any other.
//
// A set holding several is joined, and joining is lossy in one direction worth
// naming: the error's text holds every diagnostic, in report order, but [From]
// answers with the first and there is no second call that reaches the rest. A
// caller that has to report each one separately should be handed the set rather
// than an error — which is why every stage that can produce more than one is
// written to return one.
func (s *Set) Err() error {
	switch len(s.diagnostics) {
	case 0:
		return nil
	case 1:
		return s.diagnostics[0]
	default:
		joined := make([]error, len(s.diagnostics))
		for i, d := range s.All() {
			joined[i] = d
		}
		return errors.Join(joined...)
	}
}

// ExitCode returns the process status a run ending with this set should exit
// with.
func (s *Set) ExitCode() int {
	if s.Empty() {
		return ExitOK
	}
	return ExitDiagnostics
}
