package diag_test

import (
	"errors"
	"testing"

	"github.com/okian/forge/internal/diag"
)

// A set that found nothing is not a failure, and an interface that returns an
// error has to be told so in the only way it can hear it.
func TestASetThatFoundNothingIsNoError(t *testing.T) {
	var empty diag.Set

	if err := empty.Err(); err != nil {
		t.Errorf("an empty set returned %v", err)
	}
}

// One diagnostic comes back as itself, so that the stage above recovers it and
// reports it like any other rather than unwrapping a container first.
func TestOneDiagnosticSurvivesUnwrapped(t *testing.T) {
	var set diag.Set
	set.Add(diag.New(codeUnknownOption, at("model/spec.go", 4, 6), "one thing is wrong"))

	reported, ok := diag.From(set.Err())
	if !ok {
		t.Fatalf("the error %v carries no diagnostic", set.Err())
	}
	if reported.Message != "one thing is wrong" {
		t.Errorf("the diagnostic reads %q", reported.Message)
	}
}

// Several come back joined, where each is still reachable — a layer that found
// two problems has found two, and the boundary is not the place to lose one.
func TestSeveralDiagnosticsStayReachable(t *testing.T) {
	var set diag.Set
	set.Add(diag.New(codeUnknownOption, at("model/spec.go", 9, 6), "the second"))
	set.Add(diag.New(codeUnknownOption, at("model/spec.go", 4, 6), "the first"))

	err := set.Err()
	if err == nil {
		t.Fatal("a set holding two returned no error")
	}

	// Joined in report order rather than in the order they were recorded, so
	// that what a reader sees does not depend on which check ran first.
	want := "model/spec.go:4:6: FRG3010: the first\nmodel/spec.go:9:6: FRG3010: the second"
	if err.Error() != want {
		t.Errorf("the error reads\n%s\nwant\n%s", err, want)
	}

	var found diag.Diagnostic
	if !errors.As(err, &found) {
		t.Error("neither diagnostic is reachable through the join")
	}
}
