package model_test

import (
	"testing"

	"github.com/okian/forge/internal/model"
)

func TestKindString(t *testing.T) {
	cases := map[model.Kind]string{
		model.KindInvalid:   "invalid",
		model.KindSubject:   "subject",
		model.KindElement:   "element",
		model.KindStorage:   "storage",
		model.KindRefining:  "refining",
		model.KindDecorator: "decorator",
		model.KindTransport: "transport",
		model.KindBridge:    "bridge",
		model.Kind(99):      "kind(99)",
	}

	for kind, want := range cases {
		if got := kind.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", uint8(kind), got, want)
		}
	}
}

func TestKindValid(t *testing.T) {
	cases := map[model.Kind]bool{
		model.KindInvalid:   false,
		model.KindSubject:   true,
		model.KindTransport: true,
		model.KindBridge:    true,
		model.Kind(99):      false,
	}

	for kind, want := range cases {
		if got := kind.Valid(); got != want {
			t.Errorf("Kind(%d).Valid() = %v, want %v", uint8(kind), got, want)
		}
	}
}

// Every kind must have a distinct name, or diagnostics that report a kind stop
// being able to tell two of them apart.
func TestKindNamesAreDistinct(t *testing.T) {
	seen := make(map[string]model.Kind)
	for kind := model.KindInvalid; kind <= model.KindBridge; kind++ {
		name := kind.String()
		if other, ok := seen[name]; ok {
			t.Errorf("Kind(%d) and Kind(%d) both render as %q", uint8(other), uint8(kind), name)
		}
		seen[name] = kind
	}
	if len(seen) != 8 {
		t.Errorf("got %d distinct kind names, want 8", len(seen))
	}
}

func TestFormString(t *testing.T) {
	cases := map[model.Form]string{
		model.FormInvalid: "invalid",
		model.FormInline:  "inline",
		model.FormSpec:    "spec",
		model.Form(42):    "form(42)",
	}

	for form, want := range cases {
		if got := form.String(); got != want {
			t.Errorf("Form(%d).String() = %q, want %q", uint8(form), got, want)
		}
	}
}

func TestFormValid(t *testing.T) {
	cases := map[model.Form]bool{
		model.FormInvalid: false,
		model.FormInline:  true,
		model.FormSpec:    true,
		model.Form(42):    false,
	}

	for form, want := range cases {
		if got := form.Valid(); got != want {
			t.Errorf("Form(%d).Valid() = %v, want %v", uint8(form), got, want)
		}
	}
}
