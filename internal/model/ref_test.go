package model_test

import (
	"slices"
	"testing"

	"github.com/okian/forge/internal/model"
)

const markerPkg = "github.com/okian/forge"

func TestTypeRefString(t *testing.T) {
	cases := map[string]struct {
		ref  model.TypeRef
		want string
	}{
		"qualified":   {model.TypeRef{Pkg: markerPkg, Name: "Collection"}, markerPkg + ".Collection"},
		"predeclared": {model.TypeRef{Name: "string"}, "string"},
		"zero":        {model.TypeRef{}, ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.ref.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTypeRefIsZero(t *testing.T) {
	cases := map[string]struct {
		ref  model.TypeRef
		want bool
	}{
		"zero":      {model.TypeRef{}, true},
		"name only": {model.TypeRef{Name: "string"}, false},
		"pkg only":  {model.TypeRef{Pkg: markerPkg}, false},
		"both":      {model.TypeRef{Pkg: markerPkg, Name: "Ring"}, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.ref.IsZero(); got != tc.want {
				t.Errorf("IsZero() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Sorting by reference is how deterministic output is produced from any set of
// types, so the ordering has to be a strict, total order over both fields.
func TestTypeRefLessOrdersByPackageThenName(t *testing.T) {
	refs := []model.TypeRef{
		{Pkg: "b/pkg", Name: "Alpha"},
		{Pkg: "a/pkg", Name: "Zeta"},
		{Name: "string"},
		{Pkg: "a/pkg", Name: "Alpha"},
	}

	slices.SortFunc(refs, model.TypeRef.Compare)

	want := []string{"string", "a/pkg.Alpha", "a/pkg.Zeta", "b/pkg.Alpha"}
	for i, ref := range refs {
		if got := ref.String(); got != want[i] {
			t.Errorf("sorted[%d] = %q, want %q", i, got, want[i])
		}
	}

	same := model.TypeRef{Pkg: "a/pkg", Name: "Alpha"}
	if same.Less(same) {
		t.Error("Less reports a reference as less than itself")
	}
}

// Compare answers in the three directions a sort needs, and every part of a
// reference has to be able to decide it: two instantiations of one generic
// differ in nothing but their arguments.
func TestTypeRefCompare(t *testing.T) {
	cases := map[string]struct {
		a, b model.TypeRef
		want int
	}{
		"by package":   {model.TypeRef{Pkg: "a"}, model.TypeRef{Pkg: "b"}, -1},
		"by name":      {model.TypeRef{Pkg: "a", Name: "Z"}, model.TypeRef{Pkg: "a", Name: "A"}, 1},
		"by arguments": {model.TypeRef{Name: "Pair", Args: "[int]"}, model.TypeRef{Name: "Pair", Args: "[string]"}, -1},
		"alike":        {model.TypeRef{Pkg: "a", Name: "A"}, model.TypeRef{Pkg: "a", Name: "A"}, 0},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := tc.a.Compare(tc.b)
			if (got < 0) != (tc.want < 0) || (got > 0) != (tc.want > 0) {
				t.Errorf("Compare() = %d, want %d", got, tc.want)
			}
			if reversed := tc.b.Compare(tc.a); (reversed < 0) != (got > 0) {
				t.Errorf("Compare() disagrees with itself reversed: %d and %d", got, reversed)
			}
		})
	}
}

func TestLayerRefDirective(t *testing.T) {
	cases := map[string]string{
		"Collection": "collection",
		"Json":       "json",
		"LRU":        "lru",
		"Guarded":    "guarded",
	}

	for name, want := range cases {
		ref := model.LayerRef{Origin: model.TypeRef{Pkg: markerPkg, Name: name}}
		if got := ref.Directive(); got != want {
			t.Errorf("%s.Directive() = %q, want %q", name, got, want)
		}
	}
}

func TestLayerRefString(t *testing.T) {
	cases := map[string]struct {
		ref  model.LayerRef
		want string
	}{
		"written": {
			model.LayerRef{Origin: model.TypeRef{Pkg: markerPkg, Name: "Collection"}, Kind: model.KindRefining},
			"Collection:refining",
		},
		"inferred": {
			model.LayerRef{Origin: model.TypeRef{Pkg: markerPkg, Name: "Slice"}, Kind: model.KindStorage, Implicit: true},
			"Slice:storage(implicit)",
		},
		"unclaimed marker": {
			model.LayerRef{Origin: model.TypeRef{Pkg: markerPkg, Name: "Topic"}},
			"Topic:invalid",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.ref.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
