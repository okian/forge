package tags_test

import (
	"testing"

	"github.com/okian/forge/internal/tags"
)

// jsonTag is a representative parsed tag: a name plus a bare option, an option
// with a colon separator, and an option with an equals separator.
func jsonTag() tags.Tag {
	return tags.Tag{
		Key:  "json",
		Raw:  "name,omitzero,format:RFC3339,min=3,empty:",
		Name: "name",
		Options: []tags.Option{
			{Name: "omitzero", Raw: "omitzero"},
			{Name: "format", Value: "RFC3339", HasValue: true, Raw: "format:RFC3339"},
			{Name: "min", Value: "3", HasValue: true, Raw: "min=3"},
			{Name: "empty", HasValue: true, Raw: "empty:"},
		},
	}
}

func TestTagLookup(t *testing.T) {
	tag := jsonTag()

	cases := []struct {
		name     string
		option   string
		want     tags.Option
		wantOK   bool
		wantHas  bool
		wantText string
	}{
		{
			name:     "bare option",
			option:   "omitzero",
			want:     tags.Option{Name: "omitzero", Raw: "omitzero"},
			wantOK:   true,
			wantHas:  true,
			wantText: "",
		},
		{
			name:     "colon separated value",
			option:   "format",
			want:     tags.Option{Name: "format", Value: "RFC3339", HasValue: true, Raw: "format:RFC3339"},
			wantOK:   true,
			wantHas:  true,
			wantText: "RFC3339",
		},
		{
			name:     "equals separated value",
			option:   "min",
			want:     tags.Option{Name: "min", Value: "3", HasValue: true, Raw: "min=3"},
			wantOK:   true,
			wantHas:  true,
			wantText: "3",
		},
		{
			name:   "absent",
			option: "inline",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tag.Lookup(tc.option)
			if ok != tc.wantOK {
				t.Fatalf("Lookup(%q) ok = %v, want %v", tc.option, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("Lookup(%q) = %+v, want %+v", tc.option, got, tc.want)
			}
			if has := tag.Has(tc.option); has != tc.wantHas {
				t.Errorf("Has(%q) = %v, want %v", tc.option, has, tc.wantHas)
			}
			if value := tag.Value(tc.option); value != tc.wantText {
				t.Errorf("Value(%q) = %q, want %q", tc.option, value, tc.wantText)
			}
		})
	}
}

// An option written with a separator and no value is not the same thing as an
// option written without one, and Value alone cannot tell them apart.
func TestTagDistinguishesEmptyValueFromNoValue(t *testing.T) {
	tag := jsonTag()

	empty, ok := tag.Lookup("empty")
	if !ok {
		t.Fatal("Lookup(empty) reported absent")
	}
	if !empty.HasValue {
		t.Error("empty: parsed as carrying no value")
	}

	bare, ok := tag.Lookup("omitzero")
	if !ok {
		t.Fatal("Lookup(omitzero) reported absent")
	}
	if bare.HasValue {
		t.Error("omitzero parsed as carrying a value")
	}

	if empty.Value != bare.Value {
		t.Errorf("values differ (%q, %q); only HasValue should separate them", empty.Value, bare.Value)
	}
}

// Lookup resolves a repeat to the first occurrence, and Count is what lets a
// validator see that there was a repeat at all. json/v2 rejects a repeated
// option outright, so the two have to be separable.
func TestTagLookupAndCountOnARepeatedOption(t *testing.T) {
	tag := tags.Tag{
		Options: []tags.Option{
			{Name: "case", Value: "ignore", HasValue: true, Raw: "case:ignore"},
			{Name: "case", Value: "strict", HasValue: true, Raw: "case:strict"},
			{Name: "omitzero", Raw: "omitzero"},
		},
	}

	if got := tag.Value("case"); got != "ignore" {
		t.Errorf("Value(case) = %q, want the first occurrence %q", got, "ignore")
	}
	if got := tag.Count("case"); got != 2 {
		t.Errorf("Count(case) = %d, want 2", got)
	}
	if got := tag.Count("omitzero"); got != 1 {
		t.Errorf("Count(omitzero) = %d, want 1", got)
	}
	if got := tag.Count("embed"); got != 0 {
		t.Errorf("Count(embed) = %d, want 0", got)
	}
}

// A bare "-" excludes the field. A trailing comma with nothing after it is a
// malformed tag under json/v2 rather than a way to name a field "-", so the
// model must not represent it as one.
func TestTagIgnoredIsSeparateFromTheName(t *testing.T) {
	hidden := tags.Tag{Key: "json", Raw: "-", Ignored: true}
	if !hidden.Ignored || hidden.Name != "" {
		t.Errorf("json:\"-\" modelled as %+v, want an ignored field with no name", hidden)
	}

	named := tags.Tag{
		Key:     "json",
		Raw:     "-,omitzero",
		Name:    "-",
		Options: []tags.Option{{Name: "omitzero", Raw: "omitzero"}},
	}
	if named.Ignored {
		t.Error("a field named \"-\" was modelled as ignored")
	}
	if named.Name != "-" {
		t.Errorf("Name = %q, want %q", named.Name, "-")
	}
}

func TestTagIsZero(t *testing.T) {
	cases := map[string]struct {
		tag  tags.Tag
		want bool
	}{
		"zero value":      {tags.Tag{}, true},
		"key only":        {tags.Tag{Key: "json"}, false},
		"raw only":        {tags.Tag{Raw: "-"}, false},
		"name only":       {tags.Tag{Name: "id"}, false},
		"options only":    {tags.Tag{Options: []tags.Option{{Name: "inline"}}}, false},
		"ignored is data": {tags.Tag{Ignored: true}, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.tag.IsZero(); got != tc.want {
				t.Errorf("IsZero() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTagString(t *testing.T) {
	if got, want := jsonTag().String(), `json:"name,omitzero,format:RFC3339,min=3,empty:"`; got != want {
		t.Errorf("String() = %s, want %s", got, want)
	}
}

func TestOptionString(t *testing.T) {
	opt := tags.Option{Name: "format", Value: "RFC3339", HasValue: true, Raw: "format:RFC3339"}
	if got, want := opt.String(), "format:RFC3339"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
