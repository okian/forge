package tags_test

import (
	"encoding/json/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/okian/forge/internal/tags"
)

// goFieldName is the name the differential corpus's field carries, and so the
// name a tag that overrides nothing has to produce.
const goFieldName = "Field"

func TestParseJSON(t *testing.T) {
	cases := map[string]struct {
		value   string
		want    tags.Tag
		options []tags.Option
	}{
		"a name on its own": {
			value: "name",
			want:  tags.Tag{Name: "name"},
		},
		"a name and an option": {
			value:   "name,omitzero",
			want:    tags.Tag{Name: "name"},
			options: []tags.Option{{Name: "omitzero", Raw: "omitzero"}},
		},
		"options only": {
			value:   ",omitzero,omitempty",
			options: []tags.Option{{Name: "omitzero", Raw: "omitzero"}, {Name: "omitempty", Raw: "omitempty"}},
		},
		"nothing at all": {
			value: "",
		},
		// A name is a JSON member, not a Go identifier, so it may hold anything
		// but the five characters that would end it.
		"a name that is not an identifier": {
			value: "a b~naïve",
			want:  tags.Tag{Name: "a b~naïve"},
		},
		"the whole tag hides the field": {
			value: "-",
			want:  tags.Tag{Ignored: true},
		},
		// Only the whole tag hides it. A dash with options after it is a field
		// named "-", which is a distinction worth a test because the v1 package
		// spelled the escape hatch this way.
		"a dash with options names the field": {
			value:   "-,omitzero",
			want:    tags.Tag{Name: "-"},
			options: []tags.Option{{Name: "omitzero", Raw: "omitzero"}},
		},
		"an option that takes a value": {
			value:   "a,case:ignore",
			want:    tags.Tag{Name: "a"},
			options: []tags.Option{{Name: "case", Value: "ignore", HasValue: true, Raw: "case:ignore"}},
		},
		// The one place a quote is legal, and the reason splitting a tag on
		// commas is wrong: this value holds one.
		"a quoted value holding a comma": {
			value: "a,format:'2006-01-02, 15:04'",
			want:  tags.Tag{Name: "a"},
			options: []tags.Option{
				{Name: "format", Value: "2006-01-02, 15:04", HasValue: true, Raw: "format:'2006-01-02, 15:04'"},
			},
		},
		"a quoted value holding an escape": {
			value: `a,format:'it\'s'`,
			want:  tags.Tag{Name: "a"},
			options: []tags.Option{
				{Name: "format", Value: "it's", HasValue: true, Raw: `format:'it\'s'`},
			},
		},
		// A double quote is ordinary inside single quotes, and has to survive
		// being turned into the delimiter and back.
		"a quoted value holding a double quote": {
			value: `a,format:'say "so"'`,
			want:  tags.Tag{Name: "a"},
			options: []tags.Option{
				{Name: "format", Value: `say "so"`, HasValue: true, Raw: `format:'say "so"'`},
			},
		},
		// An option nothing understands still decomposes. Rejecting it belongs
		// to the layer that would have read it.
		"an option nothing understands": {
			value:   "a,whatever",
			want:    tags.Tag{Name: "a"},
			options: []tags.Option{{Name: "whatever", Raw: "whatever"}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			found, problems := tags.Parse(`json:` + strconv.Quote(tc.value))

			if len(problems) != 0 {
				t.Fatalf("reported %v", messages(problems))
			}
			if len(found) != 1 {
				t.Fatalf("read %d tags, want 1", len(found))
			}

			want := tc.want
			want.Key, want.Raw, want.Options = "json", tc.value, tc.options

			if got := found[0]; !sameTag(got, want) {
				t.Errorf("tag = %+v, want %+v", got, want)
			}
		})
	}
}

// A broken tag is still decomposed, and where it breaks decides what the pieces
// are. The standard library re-reads a value it could not read as the option
// that value is not, rather than swallowing it, so a tag that both refuse still
// has to come apart into the same options — otherwise a caller inspecting a
// rejected tag sees a tag nobody wrote.
func TestParseJSONReportsABrokenGrammar(t *testing.T) {
	cases := map[string]struct {
		value    string
		fragment string
		options  []string
	}{
		"trailing comma":        {value: "a,", fragment: "trailing comma"},
		"nothing but a comma":   {value: ",", fragment: "trailing comma"},
		"an option that is not": {value: "a,1bad", fragment: "invalid character", options: []string{"1bad"}},
		"an empty option":       {value: "a,,b", fragment: "invalid character", options: []string{"", "b"}},
		// The quote is consumed as the name it could not be, so nothing is left
		// to read as an option.
		"a quoted name":           {value: "'name'", fragment: "invalid character"},
		"a quoted option":         {value: "a,'omitzero'", fragment: "invalid character", options: []string{"'omitzero'"}},
		"an equals separator":     {value: "a,min=3", fragment: "before the next option", options: []string{"min", "=3"}},
		"a value nothing takes":   {value: "a,foo:bar", fragment: "before the next option", options: []string{"foo", ":bar"}},
		"a missing case value":    {value: "a,case", fragment: "missing its value", options: []string{"case"}},
		"a missing format value":  {value: "a,format", fragment: "missing its value", options: []string{"format"}},
		"an empty format value":   {value: "a,format:", fragment: "an option is missing", options: []string{"format"}},
		"an unterminated quote":   {value: "a,format:'unfinished", fragment: "not terminated", options: []string{"format", "'unfinished"}},
		"an unquotable escape":    {value: `a,format:'\q'`, fragment: "invalid single-quoted string", options: []string{"format", `'\q'`}},
		"a name that is not text": {value: "\xff", fragment: "not valid UTF-8"},

		// A value that could not be read stays where it is. Consuming it would
		// merge it into the option it follows and lose the one after that.
		"a quoted case value": {
			value:    "a,case:'ignore',omitzero",
			fragment: "malformed value",
			options:  []string{"case", "'ignore'", "omitzero"},
		},
		"a value that is quoted to nothing": {
			value:    "a,format:'',omitzero",
			fragment: "cannot have an empty value",
			options:  []string{"format", "''", "omitzero"},
		},
		"a value whose quote never closes": {
			value:    "a,format:'x,omitzero",
			fragment: "not terminated",
			options:  []string{"format", "'x", "omitzero"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			found, problems := tags.Parse(`json:` + strconv.Quote(tc.value))

			if len(problems) == 0 {
				t.Fatalf("Parse(%q) reported nothing", tc.value)
			}
			if !strings.Contains(problems[0].String(), tc.fragment) {
				t.Errorf("first problem is %q, want it to mention %q", problems[0], tc.fragment)
			}
			if problems[0].Key != "json" {
				t.Errorf("problem is attributed to %q, want json", problems[0].Key)
			}

			if len(found) != 1 {
				t.Fatalf("read %d tags, want 1", len(found))
			}
			var written []string
			for _, option := range found[0].Options {
				written = append(written, option.Raw)
			}
			if !slices.Equal(written, tc.options) {
				t.Errorf("decomposed into %q, want %q", written, tc.options)
			}
		})
	}
}

// A broken tag still yields what was read before it broke, so that a layer
// looking the key up finds it present rather than absent — and finds the
// problem beside it, which is what stops anything being generated from it.
func TestParseJSONKeepsWhatItReadBeforeBreaking(t *testing.T) {
	found, problems := tags.Parse(`json:"name,omitzero,1bad"`)

	if len(problems) == 0 {
		t.Fatal("reported nothing")
	}
	if len(found) != 1 {
		t.Fatalf("read %d tags, want 1", len(found))
	}
	if got, want := found[0].Name, "name"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
	if !found[0].Has("omitzero") {
		t.Errorf("options = %+v, want the option before the break kept", found[0].Options)
	}
}

// jsonCorpus is the set of tag shapes the grammar is held against. What each
// one means is deliberately not written down: the standard library is asked at
// test time, so the corpus cannot drift away from the thing it is a corpus of.
var jsonCorpus = []string{
	// Shapes both accept.
	"", "name", "name,omitzero", ",omitzero", ",omitempty", "-", "-,omitzero",
	"a b", "naïve", "0digit", "a,case:ignore", "a,case:strict", "a,whatever",
	"a,_ok", "a,omitzero,omitempty", "a\tb,omitzero",

	// Shapes whose grammar is broken.
	"a,", ",", "a,,b", "'name'", "a,'omitzero'", "a,case:'ignore'", "a,1bad",
	"a,min=3", "a,foo:bar", "-,", "a,format:", "a,format:'unfinished", "a,case",
	"a,format", "\xff", "a,format:''", "a,case:'ignore',omitzero",
	"a,format:'x,omitzero", "a`b", `a"b`,

	// Shapes the grammar decomposes and the standard library still refuses.
	"a,omitzero,omitzero", "a,omitEmpty", "a,omit_empty",
	"a,case:ignore,case:strict", "a,case:bogus", "a,embed", "a,string",
	"a,format:RFC3339", "a,format:RFC3339,omitzero",
}

// refusedByPolicy names the corpus entries the grammar decomposes and the
// standard library still refuses, with the reason it refuses each.
//
// None of these is a question the grammar has to answer to find where an option
// ends, which is the line this package draws. Several of them it could answer —
// an option written twice, two options that contradict each other, an option
// written somewhere its own rules forbid — and deliberately does not, because
// the layer that knows what an option means is the layer that should say so.
// The rest it could not answer at all: whether a value is in the set its option
// allows, whether the field's type supports the option, and whether the release
// implements it are facts about everything except the tag.
var refusedByPolicy = map[string]string{
	"a,omitzero,omitzero":       "an option written twice",
	"a,omitEmpty":               "an option that resembles a real one",
	"a,omit_empty":              "an option that resembles a real one",
	"a,case:ignore,case:strict": "two options that contradict each other",
	"a,case:bogus":              "a value outside the set the option allows",
	"a,string":                  "an option the field's type does not allow",
	"a,embed":                   "an option that may not share a tag",
	"a,format:RFC3339,omitzero": "an option written somewhere its rules forbid",

	// Not a judgement about the tag at all: format parses, and this release
	// then refuses every value for it, because support is gated behind an
	// option no code outside the standard library can reach.
	"a,format:RFC3339": "a format value this release has no support for",
}

// The name forge computes has to be the name the standard library computes.
// Anything else is a field that goes onto the wire under one name and is looked
// for under another, which no care taken anywhere else recovers from.
//
// The corpus carries no expected answers. Both parsers are asked, and the two
// have to agree — about the name, and about whether the tag is a tag at all.
func TestJSONAgreesWithTheStandardLibrary(t *testing.T) {
	for _, value := range jsonCorpus {
		t.Run(value, func(t *testing.T) {
			stdName, stdWritten, stdOK := standardLibrary(t, value)
			name, written, broken := parsed(t, value)
			reason, byPolicy := refusedByPolicy[value]

			switch {
			case stdOK:
				if broken != "" {
					t.Fatalf("the grammar refuses this as %q, and the standard library accepts it", broken)
				}
				if byPolicy {
					t.Fatalf("this is listed as refused by policy (%s), and the standard library accepts it", reason)
				}
				if written != stdWritten {
					t.Fatalf("the field is written=%v here and =%v in the standard library", written, stdWritten)
				}
				if written && name != stdName {
					t.Errorf("the member is named %q here and %q in the standard library", name, stdName)
				}

			case broken != "":
				if byPolicy {
					t.Errorf("this is listed as refused only by policy (%s), and the grammar refuses it too: %s", reason, broken)
				}

			case byPolicy:
				// Decomposed here, refused there, for a reason no decomposition
				// could see. The boundary is deliberate and is named above.

			default:
				t.Error("the standard library refuses this and the grammar does not; either the grammar has to report it or the policy that refuses it has to be named")
			}
		})
	}
}

// standardLibrary reports the JSON member name encoding/json/v2 gives a field
// carrying this tag, whether it writes the field at all, and whether it
// accepted the tag.
func standardLibrary(t *testing.T, value string) (name string, written, ok bool) {
	t.Helper()

	subject := reflect.New(reflect.StructOf([]reflect.StructField{{
		Name: goFieldName,
		Type: reflect.TypeFor[string](),
		Tag:  reflect.StructTag(`json:` + strconv.Quote(value)),
	}})).Elem()
	subject.Field(0).SetString("written")

	encoded, err := json.Marshal(subject.Interface())
	if err != nil {
		return "", false, false
	}

	var members map[string]string
	if err := json.Unmarshal(encoded, &members); err != nil {
		t.Fatalf("re-reading %s: %v", encoded, err)
	}
	for member := range members {
		return member, true, true
	}
	return "", false, true
}

// parsed reports the JSON member name this package computes for a field
// carrying this tag, whether the field is written at all, and what it reported
// about the tag.
func parsed(t *testing.T, value string) (name string, written bool, broken string) {
	t.Helper()

	found, problems := tags.Parse(`json:` + strconv.Quote(value))
	if len(found) != 1 {
		t.Fatalf("read %d tags, want 1", len(found))
	}
	broken = strings.Join(messages(problems), "; ")

	tag := found[0]
	switch {
	case tag.Ignored:
		return "", false, broken
	case tag.Name == "":
		// An unnamed tag keeps the field's own name, which is the one rule the
		// member name depends on that is not written in the tag.
		return goFieldName, true, broken
	default:
		return tag.Name, true, broken
	}
}
