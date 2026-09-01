package enum_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers/enum"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// A named integer with constants against it gets the whole API of a closed set.
func TestWhatAClosedSetGets(t *testing.T) {
	held := source(t, written(t, "Status"))

	for _, want := range []string{
		"func (v Status) String() string {",
		"func ValuesStatus() []Status {",
		"func ParseStatus(s string) (Status, error) {",
		"func (v Status) MarshalText() ([]byte, error) {",
		"func (v Status) AppendText(b []byte) ([]byte, error) {",
		"func (v *Status) UnmarshalText(b []byte) error {",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the set does not hold %q:\n%s", want, held)
		}
	}

	compiles(t, held)
}

// A member is called its constant's name with the type's taken off the front,
// and what is left lower-cased a word at a time.
func TestWhatAMemberIsCalled(t *testing.T) {
	held := source(t, written(t, "Status"))

	for _, want := range []string{
		`case StatusUnknown:`,
		`return "unknown"`,
		`case "active":`,
		`return StatusActive, nil`,
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the set does not hold %q:\n%s", want, held)
		}
	}

	// And the prefix is gone rather than lower-cased in place, which is the
	// mistake that reads as working until somebody looks at the wire.
	if strings.Contains(held, `"statusActive"`) {
		t.Errorf("the type's name was kept in the member's:\n%s", held)
	}
}

// A named string carries its text already, so that is what a member is called.
//
// Deriving one from the constant's name would give two answers about a member —
// the author's and forge's — and put the wrong one on the wire.
func TestAMemberOfANamedStringIsItsOwnText(t *testing.T) {
	held := source(t, written(t, "Grade"))

	for _, want := range []string{`return "pass"`, `case "fail":`} {
		if !strings.Contains(held, want) {
			t.Errorf("the set does not hold %q:\n%s", want, held)
		}
	}
	if strings.Contains(held, `"gradePass"`) {
		t.Errorf("a member's name was derived rather than read:\n%s", held)
	}

	compiles(t, held)
}

// Values reports the members in declaration order, not in the order they sort.
//
// Declaration order is what the constant block reads in and what a run counted
// by iota means. The scope reports names alphabetically, which would put Active
// before Unknown and make the list read as somebody's mistake.
func TestTheMembersAreInDeclarationOrder(t *testing.T) {
	listed := listing(t, source(t, written(t, "Status")), "ValuesStatus() []Status {")

	at := 0
	for _, one := range []string{"StatusUnknown", "StatusActive", "StatusSuspended", "StatusClosed"} {
		next := strings.Index(listed[at:], one)
		if next < 0 {
			t.Fatalf("%s is missing from the list, or is out of order:\n%s", one, listed)
		}
		at += next
	}
}

// listing returns the body of the function that lists a set's members.
func listing(t *testing.T, held, after string) string {
	t.Helper()

	_, out, found := strings.Cut(held, after)
	if !found {
		t.Fatalf("nothing lists the members:\n%s", held)
	}

	out, _, found = strings.Cut(out, "\n}")
	if !found {
		t.Fatalf("the list does not end:\n%s", out)
	}
	return out
}

// Constants declared away from their type are found, because the whole package
// is walked rather than the file the type is in.
//
// A large set is usually written that way, and a walk over one file would find
// half of it and say nothing at all about the rest.
func TestMembersDeclaredAwayFromTheirType(t *testing.T) {
	held := source(t, written(t, "Elsewhere"))

	for _, want := range []string{"ElsewhereNear", "ElsewhereFar"} {
		if !strings.Contains(held, want) {
			t.Errorf("a member declared in another file was not found:\n%s", held)
		}
	}

	compiles(t, held)
}

// Two names for one value are both kept, and the first is what is written back.
//
// Aliasing is what a package does while a name is being changed, so a reader
// that took only one of them would break every caller the moment the other was
// added. What String answers with is the first declared, because a value that
// rendered as its alias would rename itself as soon as the alias appeared.
func TestTwoNamesForOneValue(t *testing.T) {
	held := source(t, written(t, "Aliased"))

	// Both parse.
	for _, want := range []string{`case "first":`, `case "one":`} {
		if !strings.Contains(held, want) {
			t.Errorf("the set does not parse %q:\n%s", want, held)
		}
	}

	// And the rendering keeps the first, which is what a switch over duplicate
	// cases would otherwise refuse to compile at all.
	compiles(t, held)
}

// A member's name is lowered a word at a time, not a letter at a time.
//
// An exported Go name often opens with an initialism, and StatusOK lowered by
// one letter is "oK" — a name nobody would write and no reader would recognise.
// The rule is the one a codec already names a field by, so a package holding
// both writes one kind of name rather than two.
func TestAMemberThatOpensWithAnInitialism(t *testing.T) {
	held := source(t, written(t, "Coded"))

	for _, want := range []string{`return "ok"`, `return "httpError"`, `return "id"`} {
		if !strings.Contains(held, want) {
			t.Errorf("the set does not hold %q:\n%s", want, held)
		}
	}
	for _, unwanted := range []string{`"oK"`, `"hTTPError"`, `"iD"`} {
		if strings.Contains(held, unwanted) {
			t.Errorf("a member was lowered a letter at a time: %s\n%s", unwanted, held)
		}
	}

	compiles(t, held)
}

// An unexported constant is not a member.
//
// A run counted by iota usually ends in a sentinel nobody outside the package
// is meant to hold — a count, an end marker — and a set that offered one would
// be offering the one value whose whole purpose is not being one.
func TestAnUnexportedConstantIsNotAMember(t *testing.T) {
	held := source(t, written(t, "Coded"))

	if strings.Contains(held, "codedCount") {
		t.Errorf("an unexported sentinel was written into the set:\n%s", held)
	}
}

// Members split across two files come back in the order a reader sees them.
//
// The order a raw position gives is not that order and is not even stable: a
// package's files are parsed in parallel into one file set, so which gets the
// lower base is decided by which goroutine finished first. A set inside one
// file is ordered correctly by accident, which is why this one is not.
func TestMembersSplitAcrossFiles(t *testing.T) {
	want := []string{"SplitFirst", "SplitSecond", "SplitThird", "SplitFourth"}

	// Several times, because what is being tested is that the answer does not
	// depend on which parse finished first — and one run cannot say.
	for range 5 {
		listed := listing(t, source(t, written(t, "Split")), "ValuesSplit() []Split {")

		at := 0
		for _, one := range want {
			next := strings.Index(listed[at:], one)
			if next < 0 {
				t.Fatalf("%s is missing, or the members are out of order:\n%s", one, listed)
			}
			at += next
		}
	}

	compiles(t, source(t, written(t, "Split")))
}

// Two constants written with one text are one member, because a switch cannot
// hold the same case twice.
//
// For a named string a member is its own value, so two spellings of one value
// are two names for one member in the way that matters to a switch — unlike a
// named number, where the two would have different names and different texts.
func TestTwoConstantsWithOneText(t *testing.T) {
	held := source(t, written(t, "Renamed"))

	if got := strings.Count(held, `case "pass":`); got != 1 {
		t.Errorf("the parser holds the same case %d times, want once:\n%s", got, held)
	}

	compiles(t, held)
}

// A value nobody declared does not go on a wire.
//
// String renders one as the type and the value it holds, which is what a log
// wants and is nothing a reader could take back — so the text form refuses it
// instead. The zero of a set whose members start elsewhere is exactly such a
// value, and would otherwise encode as something no decoder accepts.
func TestAValueNobodyDeclaredIsNotWritten(t *testing.T) {
	held := source(t, written(t, "Status"))

	for _, want := range []string{
		"func (v Status) Valid() bool {",
		"if !v.Valid() {",
		`return nil, errors.New(v.String() + " is not a member of Status")`,
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the set does not hold %q:\n%s", want, held)
		}
	}

	// Both halves of the text form, because encoding/json reaches for whichever
	// the type has and they would otherwise disagree.
	if got := strings.Count(held, "if !v.Valid() {"); got != 2 {
		t.Errorf("%d of the two writers guard against an undeclared value, want both:\n%s", got, held)
	}
}

// An unsigned set renders a value that is not a member as the number it is.
//
// Read through the signed function, every value above what a signed one holds
// comes out negative — so the top half of a uint64 set would render as the
// bottom half with a minus in front, and two different values would print the
// same.
func TestAnUnsignedSet(t *testing.T) {
	held := source(t, written(t, "Permitted"))

	if !strings.Contains(held, "strconv.FormatUint(uint64(v), 10)") {
		t.Errorf("an unsigned value is rendered through the signed function:\n%s", held)
	}

	compiles(t, held)
}

// A subject that is not a named scalar is refused, because there are no
// constants of one to find.
func TestASubjectThatIsNotAScalar(t *testing.T) {
	err := refused(t, "Structured")

	held, is := diag.From(err)
	if !is {
		t.Fatalf("the refusal is not a diagnostic: %v", err)
	}
	if got := held.Code.String(); got != "FRG2027" {
		t.Errorf("code is %s, want FRG2027", got)
	}
	if held.Hint == "" {
		t.Error("the complaint says nothing to do about it")
	}
}

// A named scalar with no constants is refused rather than given an empty set.
//
// What an empty one would be written is a Parse that accepts nothing, a Values
// that returns nothing and a String that always fails, which is a type made
// harder to use in exchange for nothing.
func TestAScalarWithNoMembers(t *testing.T) {
	err := refused(t, "Empty")

	held, is := diag.From(err)
	if !is {
		t.Fatalf("the refusal is not a diagnostic: %v", err)
	}
	if got := held.Code.String(); got != "FRG2028" {
		t.Errorf("code is %s, want FRG2028", got)
	}
	if !strings.Contains(held.Hint, "declare the members as constants of the type") {
		t.Errorf("the complaint does not say what to do:\n  hint: %s", held.Hint)
	}
}

// A subject another package declares is refused, because every part of the API
// belongs to the type and Go lets only its own package declare that.
func TestASubjectAnotherPackageDeclares(t *testing.T) {
	_, err := asking(t, "Status", "enumfixture/elsewhere")
	if err == nil {
		t.Fatal("a set was written for a type this package cannot declare on")
	}

	held, is := diag.From(err)
	if !is {
		t.Fatalf("the refusal is not a diagnostic: %v", err)
	}
	if got := held.Code.String(); got != "FRG2029" {
		t.Errorf("code is %s, want FRG2029", got)
	}
	if !strings.Contains(held.Hint, "write the declaration in the package that declares Status") {
		t.Errorf("the complaint does not say where the declaration belongs:\n  hint: %s", held.Hint)
	}
}

// Two constants whose names come to one word are refused, because they are two
// members that cannot both be reached.
//
// Not the same as two names for one value, which is an alias and is answered:
// these hold different values, so both would say they are members, both would
// render alike, and the name they share would parse to whichever came first.
func TestTwoMembersOfOneName(t *testing.T) {
	err := refused(t, "Clashing")

	held, is := diag.From(err)
	if !is {
		t.Fatalf("the refusal is not a diagnostic: %v", err)
	}
	if got := held.Code.String(); got != "FRG2030" {
		t.Errorf("code is %s, want FRG2030", got)
	}
	for _, want := range []string{"ClashingOK", "ClashingOk", `"ok"`} {
		if !strings.Contains(held.Message, want) {
			t.Errorf("the complaint does not name %s:\n%s", want, held.Message)
		}
	}
}

// A constant whose name does not begin with the type's keeps all of it.
//
// There is nothing to take off, and cutting somewhere else would name a member
// after a rule rather than after what its author wrote.
func TestAMemberNotNamedAfterItsType(t *testing.T) {
	held := source(t, written(t, "Loose"))

	if !strings.Contains(held, `return "Wandering"`) {
		t.Errorf("an unprefixed constant was not spelled in full:\n%s", held)
	}
	if !strings.Contains(held, `return "first"`) {
		t.Errorf("a prefixed constant beside it was not shortened:\n%s", held)
	}

	compiles(t, held)
}

// A Valid the author wrote is the one that is kept, and the one the text codec
// calls.
//
// Writing a second would not compile, and the collision policy dropping it
// silently would leave the codec calling a method whose answer is somebody
// else's — the right outcome reached by accident rather than on purpose.
func TestAValidTheAuthorWrote(t *testing.T) {
	held := source(t, written(t, "Owned"))

	if strings.Contains(held, "func (v Owned) Valid() bool {") {
		t.Errorf("a second Valid was written beside the author's:\n%s", held)
	}
	if !strings.Contains(held, "if !v.Valid() {") {
		t.Errorf("the text codec does not ask whether the value is a member:\n%s", held)
	}

	compiles(t, held)
}

// The layer names the packages its output imports.
func TestWhatTheLayerBinds(t *testing.T) {
	var paths []string
	for _, one := range enum.New().Binds() {
		paths = append(paths, one.Path)
	}

	for _, want := range []string{"errors", "strconv"} {
		if !contains(paths, want) {
			t.Errorf("the layer binds %v, which does not include %s", paths, want)
		}
	}
}

// contains reports whether a slice holds a value.
func contains(held []string, want string) bool {
	for _, one := range held {
		if one == want {
			return true
		}
	}
	return false
}

// A closed set says its elements are comparable and go to text, which is what a
// container above it can act on.
func TestWhatTheLayerExposes(t *testing.T) {
	got := enum.New().Shape(nil, shape.Shape{})

	for _, want := range []shape.Cap{shape.Comparable, shape.Encodable} {
		if !got.Caps.Has(want) {
			t.Errorf("the layer exposes %s, which does not include %s", got.Caps, want)
		}
	}

	// And nothing is refused beneath it: what a subject is is not a capability,
	// and is asked of the subject instead.
	if err := enum.New().Accepts(shape.Shape{Caps: shape.Set(shape.Structured)}); err != nil {
		t.Errorf("the layer refused a shape rather than a subject: %v", err)
	}
}

// Asked to generate with no declaration, the layer says so rather than
// panicking.
func TestGeneratingWithNoDeclaration(t *testing.T) {
	for name, ctx := range map[string]*layer.Context{
		"no context":     nil,
		"no declaration": {},
		"no subject":     {Model: &model.Model{Name: "Statuses"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := enum.New().Generate(ctx, shape.Shape{}); err == nil {
				t.Error("the layer generated without a declaration")
			}
		})
	}
}
