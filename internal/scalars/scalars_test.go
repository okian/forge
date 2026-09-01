package scalars_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
)

// A subject that asked for nothing gets nothing.
//
// It is the case that has to be right before any of the others matter. These
// emitters run for every declaration in every package, so one that wrote
// something for a plain struct would put methods on types whose authors never
// mentioned any of this — and a String on somebody's type changes how it prints
// everywhere in their program.
func TestASubjectThatAskedForNothing(t *testing.T) {
	for _, name := range []string{"Quiet", "Pair", "Wide"} {
		t.Run(name, func(t *testing.T) {
			held, diags := writing(t, name)
			if !diags.Empty() {
				t.Fatalf("a subject that asked for nothing was reported:\n%s", diags.Render())
			}
			if len(held) != 0 {
				t.Errorf("%s earned %d contributions, want none: %v", name, len(held), keysOf(held))
			}
		})
	}
}

// A display tag is what asks for a String, and what it says goes in it.
//
// The tag rather than the fields, because how a value reads to a person is not
// something its fields decide. Two strings and an int is a name, an address and
// an age in one type and a host, a path and a port in another.
func TestWhatADisplayTagAsksFor(t *testing.T) {
	held := written(t, "Labelled")["display"]

	for _, want := range []string{
		"func (v Labelled) String() string",
		"b.WriteString(v.Name)",
		`b.WriteString("age=")`,
		"strconv.FormatInt(int64(v.Age), 10)",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the rendering does not hold %q:\n%s", want, held)
		}
	}

	// The untagged field is not in it, which is the difference between a tag
	// that selects and one that merely turns the method on.
	if strings.Contains(held, "v.Note") {
		t.Errorf("a field with no display tag is rendered anyway:\n%s", held)
	}

	// And the labelled one is the only one labelled: a tag with no name renders
	// its value alone, because an author who wanted a label had somewhere to
	// put one.
	if strings.Contains(held, `b.WriteString("Name=")`) {
		t.Errorf("a tag with no name was labelled anyway:\n%s", held)
	}
}

// Nothing is rendered through fmt.
//
// The whole of what these emitters offer is that generated code costs what the
// code it replaces costs, and a String that reached for fmt would pull
// reflection into a binary to write an integer.
func TestNothingIsRenderedThroughFmt(t *testing.T) {
	for _, name := range []string{"Labelled", "Wrapped", "Counted", "Secret"} {
		t.Run(name, func(t *testing.T) {
			for verb, held := range written(t, name) {
				if strings.Contains(held, "fmt.") {
					t.Errorf("%s reaches for fmt:\n%s", verb, held)
				}
			}
		})
	}
}

// A label is written as a Go literal, whatever the author put in the tag.
//
// A tag holds whatever they wrote, quotes included, and a literal assembled by
// concatenation is how a generator writes a file that does not parse.
func TestALabelThatNeedsEscaping(t *testing.T) {
	held := written(t, "Quoted")["display"]

	if !strings.Contains(held, `b.WriteString("the \"path\"=")`) {
		t.Errorf("the label is not written as a literal:\n%s", held)
	}
}

// A struct wrapping one scalar gets a text codec, and one wrapping anything
// else does not.
//
// The narrowness is the point. What the text of a single-field wrapper should
// be is not a design question — there is one value in it and its text is that
// value's — where a struct with two fields has a format, and a format is
// something an author picks.
func TestWhatEarnsATextCodec(t *testing.T) {
	held := written(t, "Wrapped")["text"]

	for _, want := range []string{
		"func (v Wrapped) AppendText(b []byte) ([]byte, error)",
		"func (v Wrapped) MarshalText() ([]byte, error)",
		"func (v *Wrapped) UnmarshalText(b []byte) error",
		"return v.AppendText(nil)",
		"append(b, v.ID...)",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the codec does not hold %q:\n%s", want, held)
		}
	}
}

// A wrapper over a number reads its text back through strconv, and refuses text
// that does not parse before it assigns anything.
func TestAWrapperOverANumber(t *testing.T) {
	held := written(t, "Counted")["text"]

	for _, want := range []string{
		"strconv.AppendInt(b, int64(v.N), 10)",
		"strconv.ParseInt(string(b), 10, 32)",
		"v.N = int32(parsed)",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the codec does not hold %q:\n%s", want, held)
		}
	}

	// Read first and assigned last. A caller who ignores the error and carries
	// on gets what they had rather than a value from one input beside a value
	// from another.
	if strings.Index(held, "return err") > strings.Index(held, "v.N = ") {
		t.Errorf("the value is assigned before the text is known to parse:\n%s", held)
	}
}

// A redact tag is what asks for a log value, and the tagged field is not in it.
//
// Implementing this is what takes the field out of a log: slog reaches for a
// value's fields when the value does not say otherwise, so a type with a secret
// in it and no LogValue prints the secret.
func TestWhatARedactTagAsksFor(t *testing.T) {
	held := written(t, "Secret")["log"]

	for _, want := range []string{
		"func (v Secret) LogValue() slog.Value",
		`slog.String("User", v.User)`,
		`slog.String("Token", "[redacted]")`,
		`slog.Int64("Tries", int64(v.Tries))`,
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the log value does not hold %q:\n%s", want, held)
		}
	}

	if strings.Contains(held, "v.Token") {
		t.Errorf("a redacted field's value reaches the log:\n%s", held)
	}
}

// Nothing but a redact tag asks for a log value.
//
// A type with nothing to hide logs perfectly well without one — slog reaches
// for its fields and prints them — so writing one anyway would replace a
// working default with a copy of itself that has to be regenerated whenever a
// field is added.
func TestALogValueIsNotWrittenUnasked(t *testing.T) {
	if held, wrote := written(t, "Labelled")["log"]; wrote {
		t.Errorf("a subject with nothing to hide got a log value:\n%s", held)
	}
}

// Several signals on one subject are several contributions, under several keys.
//
// The keys are what stop a package writing one method twice: two declarations
// over one subject each ask and each are answered, and one key per verb is what
// makes the two answers the same entry rather than two.
func TestASubjectThatAsksForEverything(t *testing.T) {
	held := written(t, "Everything")

	for _, want := range []string{"display", "log"} {
		if _, wrote := held[want]; !wrote {
			t.Errorf("Everything earned no %s, only %v", want, keysOf(held))
		}
	}
	if _, wrote := held["text"]; wrote {
		t.Error("a two-field struct earned a text codec")
	}
}

// A field that is not a scalar is rendered through its own String.
//
// Which is not a fallback but the right answer: a type that says how it reads
// has said it, and calling that is what a person writing this by hand would
// have written. Reaching for fmt to render it would be reaching for reflection
// to call a method the compiler can resolve.
func TestADisplayedFieldThatIsNotAScalar(t *testing.T) {
	held := written(t, "Timed")["display"]

	if !strings.Contains(held, "b.WriteString(v.At.String())") {
		t.Errorf("a named field is not rendered through its own String:\n%s", held)
	}
}

// A displayed pointer is asked whether it is nil before it is called.
//
// A String is the method a caller reaches for when they want a value written
// down safely, so one that panics on a nil field is worse than any rendering of
// it. fmt writes <nil> for exactly this case, and a reader who sees that will
// know what happened.
func TestADisplayedPointerThatMayBeNil(t *testing.T) {
	held := written(t, "Maybe")["display"]

	for _, want := range []string{"if v.At == nil {", `b.WriteString("<nil>")`, "v.At.String()"} {
		if !strings.Contains(held, want) {
			t.Errorf("the rendering does not hold %q:\n%s", want, held)
		}
	}
}

// Each kind a text codec covers is converted with the calls that kind takes.
//
// One entry per width rather than one shared entry, because strconv answers
// with an int64 and a uint64 whatever the field is: a conversion written for
// the wrong width is a truncation nothing reports, and a round trip that
// appeared to work would give back a different number.
func TestEachKindATextCodecCovers(t *testing.T) {
	cases := map[string][]string{
		"Flagged":  {"strconv.AppendBool(b, v.On)", "strconv.ParseBool(string(b))", "v.On = parsed"},
		"Measured": {"strconv.AppendFloat(b, v.Value, 'g', -1, 64)", "strconv.ParseFloat(string(b), 64)"},
		"Ported": {
			"strconv.AppendUint(b, uint64(v.Port), 10)",
			"strconv.ParseUint(string(b), 10, 16)",
			"v.Port = uint16(parsed)",
		},
	}

	for name, wants := range cases {
		t.Run(name, func(t *testing.T) {
			held := written(t, name)["text"]

			for _, want := range wants {
				if !strings.Contains(held, want) {
					t.Errorf("the codec does not hold %q:\n%s", want, held)
				}
			}
		})
	}
}

// A logged field that is not a scalar goes in as itself.
//
// Which is the right answer rather than a fallback: a field whose type has its
// own LogValue gets to use it, and one that does not is resolved the way it
// would have been if this method had never been written.
func TestALoggedFieldThatIsNotAScalar(t *testing.T) {
	held := written(t, "Held")["log"]

	if !strings.Contains(held, `slog.Any("Names", v.Names)`) {
		t.Errorf("a field that is not a scalar is not handed to slog as itself:\n%s", held)
	}
}

// A display tag on a field nothing can render is refused, not written.
//
// The one failure mode a generator must not have: a slice says nothing about
// how it reads and has no String, so writing v.Names.String() would produce a
// file that does not compile — in a package the author may not edit, about a
// line they did not write. What they get instead is the field's name and what
// to do about it.
func TestADisplayTagOnSomethingUnrenderable(t *testing.T) {
	held, diags := writing(t, "Unrenderable")

	if diags.Empty() {
		t.Fatal("a field that cannot be rendered was written anyway")
	}
	if _, wrote := held["display"]; wrote {
		t.Errorf("a rendering was written for a field that cannot be:\n%s", held["display"])
	}

	found := reported(t, diags, "FRG3021")
	for _, want := range []string{"Names", "[]string"} {
		if !strings.Contains(found.Message, want) {
			t.Errorf("the complaint does not mention %q:\n%s", want, found.Message)
		}
	}
	if found.Hint == "" {
		t.Error("the complaint says nothing to do about it")
	}
}

// A display tag carrying an option is reported, since nothing reads one.
//
// Silence would leave an author believing the option does something. The tag's
// name is read and nothing else is, so what they wrote after the comma is a
// word typed for no effect.
func TestADisplayTagWithAnOptionNobodyReads(t *testing.T) {
	_, diags := writing(t, "Optioned")

	found := reported(t, diags, "FRG3022")
	for _, want := range []string{"Name", "omitempty"} {
		if !strings.Contains(found.Message, want) {
			t.Errorf("the complaint does not mention %q:\n%s", want, found.Message)
		}
	}
}

// A dash excludes the field, which is the conventional meaning and not a
// mistake.
func TestADisplayTagThatExcludes(t *testing.T) {
	held := written(t, "Skipped")["display"]

	if strings.Contains(held, "v.Hidden") {
		t.Errorf("a field excluded by its tag is rendered anyway:\n%s", held)
	}
	if !strings.Contains(held, "v.Name") {
		t.Errorf("excluding one field dropped the other:\n%s", held)
	}
}

// A text codec is opt-in, and a labelled tag asks for something else.
//
// encoding/json takes a TextMarshaler for a type with no JSON codec of its own,
// so a codec written unasked would change a wrapper from {"ID":"x"} to "x" in
// every document it appears in. And a labelled rendering is for a person to
// read: a String of "id=x" beside a text form of "x" would be two answers to
// one question.
func TestWhatDoesNotEarnATextCodec(t *testing.T) {
	if held, wrote := written(t, "Bare")["text"]; wrote {
		t.Errorf("a wrapper nobody tagged earned a codec:\n%s", held)
	}

	labelled := written(t, "Named")
	if held, wrote := labelled["text"]; wrote {
		t.Errorf("a wrapper tagged for a person earned a codec:\n%s", held)
	}
	if _, wrote := labelled["display"]; !wrote {
		t.Error("a labelled wrapper earned no rendering either")
	}
}

// A field whose type will earn its String from this run counts as saying how
// it reads.
//
// The alternative is to ask the type checker, and the type checker answers with
// whatever the last run left on disk. That makes a package that builds from a
// committed tree and fails from a clean checkout, and makes what forge writes
// depend on what forge wrote — which is the trap the claims path two hundred
// lines away already refuses to fall into.
func TestAFieldWhoseTypeIsAboutToEarnAString(t *testing.T) {
	held := written(t, "Reaching")["display"]

	if !strings.Contains(held, "b.WriteString(v.Held.String())") {
		t.Errorf("a field whose String this run writes is not rendered through it:\n%s", held)
	}
}

// And a field whose type can never be given a String is refused.
//
// The trap in deciding this from the type's tags rather than from the run: a
// type carrying a display tag looks like one about to be given a String, and a
// type that also has a field called String is one no String can ever be
// declared on. The two answers have to agree, and the way they disagree is a
// selector that resolves to the field and a file that does not compile.
func TestAFieldWhoseTypeCanNeverEarnAString(t *testing.T) {
	held, diags := writing(t, "Pointing")

	if _, wrote := held["display"]; wrote {
		t.Errorf("a field was rendered through a String nothing will write:\n%s", held["display"])
	}

	found := reported(t, diags, "FRG3021")
	if !strings.Contains(found.Message, "At") {
		t.Errorf("the complaint does not name the field:\n%s", found.Message)
	}
}

// A displayed interface is asked whether it is nil, like a pointer.
//
// A named interface is ClassNamed rather than ClassInterface, which the model's
// own documentation calls the mistake people make here — so a guard written
// against the class alone covers half the values that can be nothing.
func TestADisplayedInterfaceThatMayBeNil(t *testing.T) {
	held := written(t, "Spoken")["display"]

	if !strings.Contains(held, "if v.By == nil {") {
		t.Errorf("an interface is called without being asked first:\n%s", held)
	}
}

// A subject with a field called String earns no String.
//
// Go does not let a type declare a field and a method under one name, so the
// method cannot be written. It is not caught where every other collision is:
// that check reads what the package declares, and a field is declared by
// neither the package nor the type's method set.
func TestASubjectWhoseFieldIsCalledString(t *testing.T) {
	held, diags := writing(t, "Collides")

	if _, wrote := held["display"]; wrote {
		t.Errorf("a String was written beside a field of that name:\n%s", held["display"])
	}

	found := reported(t, diags, "FRG3023")
	if !strings.Contains(found.Message, "String") {
		t.Errorf("the complaint does not name the collision:\n%s", found.Message)
	}
	if found.Hint == "" {
		t.Error("the complaint says nothing to do about it")
	}
}

// reported returns the one diagnostic carrying a code.
func reported(t *testing.T, diags diag.Set, code string) diag.Diagnostic {
	t.Helper()

	var found []diag.Diagnostic
	for _, one := range diags.All() {
		if one.Code.String() == code {
			found = append(found, one)
		}
	}

	if len(found) != 1 {
		t.Fatalf("%d diagnostics carry %s, want one:\n%s", len(found), code, diags.Render())
	}
	return found[0]
}

// keysOf returns which verbs a subject earned, for a failure to name.
func keysOf(held map[string]string) []string {
	out := make([]string, 0, len(held))
	for verb := range held {
		out = append(out, verb)
	}
	return out
}
