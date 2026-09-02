package diag_test

import (
	"errors"
	"fmt"
	"go/token"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
)

// codeTwoStorageLayers stands in for the real composition codes, which the
// packages that report them register for themselves.
var codeTwoStorageLayers = diag.Register(1003, "two storage layers in stack")

// specPos is the position of a declaration in a fixture spec file.
var specPos = token.Position{Filename: "model/spec.go", Line: 12, Column: 6}

// twoStorageLayers builds the diagnostic the rendered format is specified by.
func twoStorageLayers() diag.Diagnostic {
	return diag.New(codeTwoStorageLayers, specPos, "two storage layers in stack (%s, %s)", "Ring", "Heap").
		WithStack("Collection[Ring[Heap[Person]]]", "                ^^^^").
		WithHint("at most one Storage layer; mark %s as Refining or drop %s", "Heap", "Ring")
}

func TestDiagnosticRender(t *testing.T) {
	checkGolden(t, "diagnostic", twoStorageLayers().Render())
}

// Not every diagnostic has a stack to show or a fix to suggest, and the parts
// that are missing must not leave blank lines behind.
func TestDiagnosticRenderOmitsAbsentParts(t *testing.T) {
	base := diag.New(codeTwoStorageLayers, specPos, "two storage layers in stack")

	cases := map[string]struct {
		diagnostic diag.Diagnostic
		want       string
	}{
		"message only": {
			base,
			"model/spec.go:12:6: FRG1003: two storage layers in stack",
		},
		"hint but no stack": {
			base.WithHint("drop one of them"),
			"model/spec.go:12:6: FRG1003: two storage layers in stack\n  hint: drop one of them",
		},
		"stack but no caret": {
			base.WithStack("Collection[Person]", ""),
			"model/spec.go:12:6: FRG1003: two storage layers in stack\n  Collection[Person]",
		},
		"caret without a stack is dropped": {
			base.WithStack("", "^^^^"),
			"model/spec.go:12:6: FRG1003: two storage layers in stack",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.diagnostic.Render(); got != tc.want {
				t.Errorf("Render() =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// The caret has to sit under the layer the message names, which is the whole
// reason the rendering exists.
func TestDiagnosticCaretLinesUpWithTheStack(t *testing.T) {
	rendered := twoStorageLayers().Render()

	lines := strings.Split(rendered, "\n")
	if len(lines) != 4 {
		t.Fatalf("rendered %d lines, want 4:\n%s", len(lines), rendered)
	}
	stack, caret := lines[1], lines[2]

	start := strings.IndexByte(caret, '^')
	if start < 0 {
		t.Fatalf("caret line holds no caret: %q", caret)
	}
	width := len(caret) - start

	if start+width > len(stack) {
		t.Fatalf("caret runs past the stack line: %q under %q", caret, stack)
	}
	if got, want := stack[start:start+width], "Heap"; got != want {
		t.Errorf("caret underlines %q, want %q", got, want)
	}
	if got := strings.Trim(caret[start:], "^"); got != "" {
		t.Errorf("caret line has trailing text %q; it must be carets only", got)
	}
}

// A copy is returned rather than the receiver mutated, so a diagnostic built
// once and specialised twice does not leak one specialisation into the other.
func TestDiagnosticBuildersDoNotMutateTheReceiver(t *testing.T) {
	base := diag.New(codeTwoStorageLayers, specPos, "two storage layers in stack")

	withHint := base.WithHint("drop one of them")
	withStack := base.WithStack("Collection[Person]", "^^^^^^^^^^")

	if base.Hint != "" {
		t.Errorf("WithHint modified the receiver: Hint = %q", base.Hint)
	}
	if base.Stack != "" || base.Caret != "" {
		t.Errorf("WithStack modified the receiver: Stack = %q, Caret = %q", base.Stack, base.Caret)
	}
	if withHint.Stack != "" {
		t.Errorf("WithHint carried a stack across: %q", withHint.Stack)
	}
	if withStack.Hint != "" {
		t.Errorf("WithStack carried a hint across: %q", withStack.Hint)
	}
}

// Each builder sets its own fields and leaves the rest alone, so the order they
// are applied in cannot change the result. Without this, a builder that rebuilt
// the value from scratch would silently drop whatever was set before it.
func TestDiagnosticBuildersCommute(t *testing.T) {
	base := diag.New(codeTwoStorageLayers, specPos, "two storage layers in stack")

	stackFirst := base.
		WithStack("Collection[Ring[Heap[Person]]]", "                ^^^^").
		WithHint("drop one of them")
	hintFirst := base.
		WithHint("drop one of them").
		WithStack("Collection[Ring[Heap[Person]]]", "                ^^^^")

	if stackFirst != hintFirst {
		t.Errorf("builders do not commute:\n%#v\nand\n%#v", stackFirst, hintFirst)
	}
	if stackFirst.Render() != hintFirst.Render() {
		t.Errorf("renderings differ:\n%s\nand\n%s", stackFirst.Render(), hintFirst.Render())
	}
}

// A code outside every reserved range places itself nowhere, so it is rejected
// where it is built rather than printed to a user as a number nothing explains.
//
// Forge's own ranges end at 5999 and a layer forge does not ship takes a code
// above that, so what is rejected is a code with no range at all: one too small
// to be printed as four digits, and one past the last.
func TestNewRejectsCodesOutsideTheReservedRanges(t *testing.T) {
	for _, code := range []diag.Code{0, 999, 10000, 99999} {
		t.Run(code.String(), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("New returned; want a panic")
				}
			}()
			_ = diag.New(code, specPos, "should not be reachable")
		})
	}
}

// A layer that returns a pointer to a diagnostic has still reported one, and
// dropping it would be exactly the silent failure this package exists to
// prevent.
func TestFromAcceptsAPointerDiagnostic(t *testing.T) {
	original := twoStorageLayers()

	got, ok := diag.From(&original)
	if !ok {
		t.Fatal("From reported no diagnostic in a pointer to one")
	}
	if got.Code != original.Code || got.Message != original.Message {
		t.Errorf("From returned %#v, want %#v", got, original)
	}

	var missing *diag.Diagnostic
	if _, ok := diag.From(missing); ok {
		t.Error("From reported a diagnostic in a nil pointer")
	}
}

func TestDiagnosticError(t *testing.T) {
	want := "model/spec.go:12:6: FRG1003: two storage layers in stack (Ring, Heap)"
	if got := twoStorageLayers().Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// A diagnostic travels through the ordinary error returns layers use, so it has
// to survive being wrapped.
func TestFromRecoversAWrappedDiagnostic(t *testing.T) {
	original := twoStorageLayers()

	got, ok := diag.From(fmt.Errorf("generating Persons: %w", error(original)))
	if !ok {
		t.Fatal("From reported no diagnostic in a wrapped one")
	}
	if got.Code != original.Code || got.Message != original.Message {
		t.Errorf("From returned %#v, want %#v", got, original)
	}

	if _, ok := diag.From(errors.New("something else")); ok {
		t.Error("From reported a diagnostic in an ordinary error")
	}
	if _, ok := diag.From(nil); ok {
		t.Error("From reported a diagnostic in a nil error")
	}
}

// A position that was never resolved still has to render, because a diagnostic
// about a missing file has nowhere to point.
func TestDiagnosticRendersAnUnknownPosition(t *testing.T) {
	d := diag.New(codeTwoStorageLayers, token.Position{}, "no position available")
	if got, want := d.Error(), "-: FRG1003: no position available"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
