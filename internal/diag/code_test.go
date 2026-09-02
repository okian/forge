package diag_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
)

func TestCodeString(t *testing.T) {
	cases := map[diag.Code]string{
		1003: "FRG1003",
		2001: "FRG2001",
		5999: "FRG5999",
	}

	for code, want := range cases {
		if got := code.String(); got != want {
			t.Errorf("Code(%d).String() = %q, want %q", int(code), got, want)
		}
	}
}

// A code's number places it, so the mapping from range to stage is part of the
// contract rather than a convention.
//
// Forge's own ranges end at 5999 and everything above belongs to a layer forge
// does not ship — one category rather than a range each, since forge cannot
// hand out ranges to code it has never seen.
func TestCodeCategory(t *testing.T) {
	cases := map[diag.Code]diag.Category{
		1000:  diag.CategoryComposition,
		1999:  diag.CategoryComposition,
		2000:  diag.CategorySubject,
		3500:  diag.CategoryOptions,
		4001:  diag.CategoryEmission,
		5999:  diag.CategoryToolchain,
		6000:  diag.CategoryLayer,
		9999:  diag.CategoryLayer,
		999:   diag.CategoryInvalid,
		10000: diag.CategoryInvalid,
		0:     diag.CategoryInvalid,
		-1:    diag.CategoryInvalid,
	}

	for code, want := range cases {
		if got := code.Category(); got != want {
			t.Errorf("Code(%d).Category() = %v, want %v", int(code), got, want)
		}
	}
}

// A code says whether it is forge's own, so that a report can send a reader to
// forge's index or to the layer that raised it.
func TestWhoseCodeItIs(t *testing.T) {
	for code, want := range map[diag.Code]bool{
		1000: true, 5999: true,
		6000: false, 9999: false,
		999: false, 10000: false, 0: false,
	} {
		if got := code.Ours(); got != want {
			t.Errorf("Code(%d).Ours() = %v, want %v", int(code), got, want)
		}
	}
}

func TestCategoryString(t *testing.T) {
	cases := map[diag.Category]string{
		diag.CategoryInvalid:     "invalid",
		diag.CategoryComposition: "composition",
		diag.CategorySubject:     "subject",
		diag.CategoryOptions:     "options",
		diag.CategoryEmission:    "emission",
		diag.CategoryToolchain:   "toolchain",
		diag.Category(9):         "category(9)",
	}

	for category, want := range cases {
		if got := category.String(); got != want {
			t.Errorf("Category(%d).String() = %q, want %q", uint8(category), got, want)
		}
	}
}

// Two failures answering to one identifier would make the identifier useless,
// so registering a code twice is a panic at initialisation rather than a
// surprise in the field.
func TestRegisterRejectsBadRegistrations(t *testing.T) {
	cases := map[string]struct {
		code    diag.Code
		summary string
		wants   string
	}{
		"below every range": {999, "too low", "outside the reserved ranges"},
		"above every range": {10000, "too high", "outside the reserved ranges"},
		"no summary":        {1900, "", "without a summary"},
		"already taken":     {1003, "something else", "already registered"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatal("Register returned; want a panic")
				}
				message, ok := recovered.(string)
				if !ok {
					t.Fatalf("panicked with %T, want a string", recovered)
				}
				if !strings.Contains(message, tc.wants) {
					t.Errorf("panic %q does not mention %q", message, tc.wants)
				}
			}()

			diag.Register(tc.code, tc.summary)
		})
	}
}

// Registration is process-global and has no undo, so codes used by tests are
// declared once per binary. Registering inside a test body would make the
// package fail under -count=2.
var (
	codeHelperCollision = diag.Register(4100, "helper type collides with an existing declaration")
	codeSubjectUnnamed  = diag.Register(2900, "subject is not a named type")
	codeSubjectPointer  = diag.Register(2901, "subject is a pointer")
)

func TestRegisterAndSummary(t *testing.T) {
	code := codeHelperCollision

	summary, ok := diag.Summary(code)
	if !ok {
		t.Fatal("Summary reported the code as unregistered")
	}
	if want := "helper type collides with an existing declaration"; summary != want {
		t.Errorf("Summary = %q, want %q", summary, want)
	}

	if _, ok := diag.Summary(5900); ok {
		t.Error("Summary reported an unregistered code as registered")
	}
}

// The index of codes is written out in documentation, so it has to come back
// in a stable order and hold each code once.
func TestRegisteredIsSortedAndUnique(t *testing.T) {
	entries := diag.Registered()
	if len(entries) < 2 {
		t.Fatalf("Registered returned %d entries, want the ones just registered", len(entries))
	}

	codes := make([]diag.Code, len(entries))
	for i, entry := range entries {
		codes[i] = entry.Code
		if entry.Summary == "" {
			t.Errorf("%v has no summary", entry.Code)
		}
	}

	if !slices.IsSorted(codes) {
		t.Errorf("Registered returned %v, want ascending order", codes)
	}
	if len(slices.Compact(slices.Clone(codes))) != len(codes) {
		t.Errorf("Registered returned duplicates: %v", codes)
	}

	for _, want := range []diag.Code{codeSubjectUnnamed, codeSubjectPointer} {
		if !slices.Contains(codes, want) {
			t.Errorf("Registered omits %v", want)
		}
	}
}
