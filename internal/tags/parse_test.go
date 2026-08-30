package tags_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/okian/forge/internal/tags"
)

// messages returns the problems as text, for comparing against what a test
// expects to have been reported.
func messages(problems []tags.Problem) []string {
	out := make([]string, len(problems))
	for i, problem := range problems {
		out[i] = problem.String()
	}
	return out
}

// sameTag reports whether two parsed tags agree in every part.
func sameTag(a, b tags.Tag) bool {
	return a.Key == b.Key && a.Raw == b.Raw && a.Name == b.Name &&
		a.Ignored == b.Ignored && slices.Equal(a.Options, b.Options)
}

// keys returns the keys of the tags that were read, in order.
func keys(found []tags.Tag) []string {
	out := make([]string, len(found))
	for i, tag := range found {
		out[i] = tag.Key
	}
	return out
}

// reported fails unless exactly one problem was reported and it mentions each
// of the given fragments.
func reported(t *testing.T, problems []tags.Problem, fragments ...string) {
	t.Helper()

	if len(problems) != 1 {
		t.Fatalf("reported %d problems, want 1: %v", len(problems), messages(problems))
	}
	for _, fragment := range fragments {
		if !strings.Contains(problems[0].String(), fragment) {
			t.Errorf("problem %q does not mention %q", problems[0], fragment)
		}
	}
}

// The outer grammar is the reflect package's, and it is the only definition of
// a struct tag there is.
func TestParseReadsEveryKey(t *testing.T) {
	found, problems := tags.Parse(`json:"name,omitzero" db:"user_id" validate:"required,min=3"`)

	if len(problems) != 0 {
		t.Fatalf("reported %v", messages(problems))
	}
	if got, want := keys(found), []string{"json", "db", "validate"}; !slices.Equal(got, want) {
		t.Fatalf("read keys %v, want %v", got, want)
	}
	if got, want := found[0].Raw, "name,omitzero"; got != want {
		t.Errorf("json tag holds %q, want %q", got, want)
	}
}

// Nothing separates one pair from the next except the spaces that may be there,
// so a tag written without them still reads. It is worth pinning down because
// go vet objects to it and the reflect package does not.
func TestParseDoesNotRequireSpacesBetweenKeys(t *testing.T) {
	found, problems := tags.Parse(`json:"a"db:"b"`)

	if len(problems) != 0 {
		t.Fatalf("reported %v", messages(problems))
	}
	if got, want := keys(found), []string{"json", "db"}; !slices.Equal(got, want) {
		t.Fatalf("read keys %v, want %v", got, want)
	}
}

// A value is a Go string literal, so a quote inside it is escaped and is not
// the end of it.
func TestParseUnquotesValues(t *testing.T) {
	found, problems := tags.Parse(`db:"a\"b"`)

	if len(problems) != 0 {
		t.Fatalf("reported %v", messages(problems))
	}
	if got, want := found[0].Raw, `a"b`; got != want {
		t.Errorf("db tag holds %q, want %q", got, want)
	}
}

// One key, one interpretation. Preferring either of two spellings silently is
// the failure this package exists to prevent, so the repeat is reported and the
// first is what everything reads, which is what the reflect package does.
func TestParseReportsAKeyWrittenTwice(t *testing.T) {
	found, problems := tags.Parse(`json:"first" json:"second"`)

	if got, want := len(found), 1; got != want {
		t.Fatalf("read %d tags, want %d", got, want)
	}
	if got, want := found[0].Raw, "first"; got != want {
		t.Errorf("json tag holds %q, want %q", got, want)
	}
	reported(t, problems, "json", "twice")
}

func TestParseReportsAMalformedTag(t *testing.T) {
	cases := map[string]struct {
		raw   string
		keys  []string
		after string
	}{
		"no colon":              {raw: `json"a"`, after: `json"a"`},
		"no quote":              {raw: `json:a`, after: "json:a"},
		"no key":                {raw: `:"a"`, after: `:"a"`},
		"value not terminated":  {raw: `json:"a`, after: `json:"a`},
		"trailing text":         {raw: `json:"a" and then some`, keys: []string{"json"}, after: "and then some"},
		"key holds a character": {raw: `js on:"a"`, after: `js on:"a"`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			found, problems := tags.Parse(tc.raw)

			if got := keys(found); !slices.Equal(got, tc.keys) {
				t.Errorf("read keys %v, want %v", got, tc.keys)
			}
			// The remainder is quoted into the message, so that a tag which
			// ends in whitespace reads as one.
			reported(t, problems, strconv.Quote(tc.after))
			if problems[0].Key != "" {
				t.Errorf("problem is attributed to key %q, want the whole tag", problems[0].Key)
			}
		})
	}
}

// A value that is not a Go string literal is a key nothing can read, and that
// is all it is. The compiler accepts the struct, and the standard library goes
// on finding every other key in the tag, so a parser that stopped there would
// silently lose a field's name — which is the failure this package exists to
// prevent, arriving through the door nobody watches.
func TestParseKeepsReadingPastAValueItCannotRead(t *testing.T) {
	cases := map[string]string{
		"an escape that is not one": `db:"\q" json:"wire"`,
		// A struct tag written as a raw literal can hold a newline, which an
		// interpreted one cannot, so this is a value that exists and cannot be
		// read.
		"a line break": "db:\"a\nb\" json:\"wire\"",
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			found, problems := tags.Parse(raw)

			if got, want := keys(found), []string{"json"}; !slices.Equal(got, want) {
				t.Fatalf("read keys %v, want %v", got, want)
			}
			if got, want := found[0].Name, "wire"; got != want {
				t.Errorf("the json tag names %q, want %q", got, want)
			}
			reported(t, problems, "db", "not a Go string literal")

			// The offending value is quoted into the message rather than
			// spliced in, or a tag holding a newline reports across two lines
			// and stops composing into a diagnostic.
			if strings.ContainsAny(problems[0].Message, "\n\t") {
				t.Errorf("message %q spans lines", problems[0].Message)
			}
		})
	}
}

// The unreadable key is still a key that was written, so a second one after it
// does not quietly take its place. The reflect package would not find that
// second one either: it stops at the first spelling of the key it was asked
// for.
func TestParseDoesNotPromoteARepeatOfAnUnreadableKey(t *testing.T) {
	found, problems := tags.Parse(`db:"\q" db:"second"`)

	if len(found) != 0 {
		t.Fatalf("read %v, want nothing", found)
	}
	if got, want := len(problems), 2; got != want {
		t.Fatalf("reported %d problems, want %d: %v", got, want, messages(problems))
	}
	if !strings.Contains(problems[1].String(), "twice") {
		t.Errorf("second problem is %q, want it to report the repeat", problems[1])
	}
}

// An empty tag is not a malformed one.
func TestParseReadsNothingFromNothing(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		found, problems := tags.Parse(raw)
		if len(found) != 0 || len(problems) != 0 {
			t.Errorf("Parse(%q) = %v, %v, want nothing", raw, found, messages(problems))
		}
	}
}

// Every key but json is a convention, and a convention cannot be broken: every
// byte belongs to some part of the result.
func TestParseConventionalKeys(t *testing.T) {
	cases := map[string]struct {
		raw     string
		want    tags.Tag
		options []tags.Option
	}{
		"a name on its own": {
			raw:  `db:"user_id"`,
			want: tags.Tag{Key: "db", Raw: "user_id", Name: "user_id"},
		},
		"rules after the first": {
			raw:  `validate:"required,nonzero,min=3,oneof=a b c"`,
			want: tags.Tag{Key: "validate", Raw: "required,nonzero,min=3,oneof=a b c", Name: "required"},
			options: []tags.Option{
				{Name: "nonzero", Raw: "nonzero"},
				{Name: "min", Value: "3", HasValue: true, Raw: "min=3"},
				{Name: "oneof", Value: "a b c", HasValue: true, Raw: "oneof=a b c"},
			},
		},
		"a colon separates too": {
			raw:  `forge:"name,format:RFC3339"`,
			want: tags.Tag{Key: "forge", Raw: "name,format:RFC3339", Name: "name"},
			options: []tags.Option{
				{Name: "format", Value: "RFC3339", HasValue: true, Raw: "format:RFC3339"},
			},
		},
		"the first separator wins": {
			raw:  `validate:"x,min=a=b"`,
			want: tags.Tag{Key: "validate", Raw: "x,min=a=b", Name: "x"},
			options: []tags.Option{
				{Name: "min", Value: "a=b", HasValue: true, Raw: "min=a=b"},
			},
		},
		"a dash hides the field": {
			raw:  `db:"-"`,
			want: tags.Tag{Key: "db", Raw: "-", Ignored: true},
		},
		"an empty value names nothing": {
			raw:  `db:""`,
			want: tags.Tag{Key: "db"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			found, problems := tags.Parse(tc.raw)

			if len(problems) != 0 {
				t.Fatalf("reported %v", messages(problems))
			}
			if len(found) != 1 {
				t.Fatalf("read %d tags, want 1", len(found))
			}

			want := tc.want
			want.Options = tc.options

			if got := found[0]; !sameTag(got, want) {
				t.Errorf("tag = %+v, want %+v", got, want)
			}
		})
	}
}

func TestProblemString(t *testing.T) {
	cases := map[string]struct {
		problem tags.Problem
		want    string
	}{
		"attributed":     {tags.Problem{Key: "json", Message: "trailing comma"}, "json: trailing comma"},
		"whole tag":      {tags.Problem{Message: "struct tag is malformed"}, "struct tag is malformed"},
		"nothing at all": {tags.Problem{}, ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.problem.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
