package validate_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/goldentest"
)

// Every rule becomes the condition it means, written against the field it was
// written on.
//
// The condition rather than the wording, because the condition is what runs. A
// rule that reported the right sentence and compared the wrong thing would read
// perfectly in a failure and let the value through.
//
// The condition alone, without whatever branches on it: how the rules of one
// field are chained is the next test's subject, and a table that spelled the
// branch as well would have to be rewritten every time the chaining changed.
func TestWhatEachRuleBecomes(t *testing.T) {
	cases := map[string]struct {
		subject string
		want    []string
	}{
		"required on a string is emptiness": {
			subject: "Person", want: []string{`v.Name == ""`},
		},
		"required on a pointer is nil": {
			subject: "Composites", want: []string{`v.Pointer == nil`},
		},
		"required on an interface is nil": {
			subject: "Composites", want: []string{`v.Any == nil`},
		},
		"required on a map is emptiness": {
			subject: "Composites", want: []string{`len(v.Lookup) == 0`},
		},
		"a bound on a number is the number": {
			subject: "Scalars", want: []string{`v.Whole < 1`, `v.Whole > 10`},
		},
		"a bound on a string is its length": {
			subject: "Person", want: []string{`len(v.Name) < 2`, `len(v.Name) > 64`},
		},
		"a bound on a slice is its length": {
			subject: "Composites", want: []string{`len(v.Items) < 1`, `len(v.Items) > 8`},
		},
		"a fractional bound on a float": {
			subject: "Scalars", want: []string{`v.Fraction < 0.5`, `v.Fraction > 99.5`},
		},
		"len is an exact length": {
			subject: "Composites", want: []string{`len(v.Fixed) != 4`},
		},
		"nonzero on a number": {
			subject: "Scalars", want: []string{`v.Unsigned == 0`},
		},
		"nonzero on a boolean": {
			subject: "Scalars", want: []string{`v.Flag == false`},
		},
		"oneof is a chain of comparisons": {
			subject: "Person",
			want:    []string{`v.Role != "admin" && v.Role != "editor" && v.Role != "reader"`},
		},
		"oneof on a number is not quoted": {
			subject: "Scalars", want: []string{`v.Ranked != 1 && v.Ranked != 2 && v.Ranked != 3`},
		},
		"a pattern is matched against": {
			subject: "Person", want: []string{`!patternPersonEmail.MatchString(string(v.Email))`},
		},
		"a rule reaches through a named type": {
			subject: "Named",
			want:    []string{`v.Kind != "fast" && v.Kind != "slow"`, `v.Size < 1`, `v.Rate > 1.0`},
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			held := source(t, written(t, want.subject))

			for _, one := range want.want {
				if !strings.Contains(held, one) {
					t.Errorf("the check does not hold %q:\n%s", one, held)
				}
			}
		})
	}
}

// The rules of one field are a chain: the first that fails is the one reported.
//
// An empty address does not match a pattern either, and reporting both tells
// somebody two things about one mistake — the second of which is about a value
// that is not there. Across fields it is the other way round, which is the
// distinction this pins.
func TestOneFieldReportsOneFailure(t *testing.T) {
	held := source(t, written(t, "Person"))

	want := "switch {\n\tcase v.Email == \"\":"
	if !strings.Contains(held, want) {
		t.Errorf("the rules on one field are not chained:\n%s", held)
	}

	// And a field with one rule is not dressed up as a chain of one.
	if !strings.Contains(held, `if len(v.Country) != 2 {`) {
		t.Errorf("a field with one rule was written as a switch:\n%s", held)
	}
}

// A pattern is compiled once when the package loads, rather than once per call.
//
// It is the whole reason a pattern is worth generating for: compiling one costs
// many times what matching against it does, and the pattern cannot change
// because it was written in the source.
func TestAPatternIsCompiledOnce(t *testing.T) {
	held := source(t, written(t, "Person"))

	if !strings.Contains(held, `var patternPersonEmail = regexp.MustCompile("^[^@,]+@[^@]+$")`) {
		t.Errorf("the pattern is not a package-level variable:\n%s", held)
	}
	if strings.Contains(held, "regexp.MustCompile") && strings.Count(held, "regexp.MustCompile") != 1 {
		t.Errorf("the pattern is compiled %d times, want once", strings.Count(held, "regexp.MustCompile"))
	}
}

// A pattern holding a comma survives being written in a tag whose options are
// separated by commas.
//
// It is why the rule is written last and takes the rest of the tag: {2,4} is
// the ordinary way to write a repetition, and a grammar with no escape has to
// end somewhere.
func TestAPatternMayHoldCommas(t *testing.T) {
	held := source(t, written(t, "Person"))

	if !strings.Contains(held, `[^@,]+@`) {
		t.Errorf("the comma inside the pattern did not survive the tag:\n%s", held)
	}
}

// Nothing is allocated by a value that satisfies its rules.
//
// The failures are gathered into a slice that starts as nothing, so a check
// that finds nothing wrong never appends and never allocates. It is what makes
// the rule worth generating rather than reflecting over: the ordinary case,
// which is the one that runs on every request, costs the comparisons alone.
func TestTheSuccessPathAllocatesNothing(t *testing.T) {
	held := source(t, written(t, "Person"))

	if !strings.Contains(held, "var failed ValidationErrors") {
		t.Errorf("the failures are not gathered into a nil slice:\n%s", held)
	}
	if !strings.Contains(held, "if len(failed) == 0 {\n\t\treturn nil\n\t}") {
		// Returned as nothing rather than as the empty list: a nil slice held
		// in an interface is an interface that is not nil, and every caller
		// checking err != nil would find one every time.
		t.Errorf("an empty list of failures is returned as an error:\n%s", held)
	}
}

// A struct inside a struct is checked by its own method, and what it reports is
// re-pathed by the value that holds it.
func TestAStructInsideAStructIsCheckedByItsOwn(t *testing.T) {
	held := source(t, written(t, "Nested"))

	for _, want := range []string{
		"func (v Address) Validate() error {",
		"if err := v.Home.Validate(); err != nil {",
		`failed = nestedValidation(failed, "Home", err)`,
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the check does not hold %q:\n%s", want, held)
		}
	}
}

// A struct held by a pointer is asked about before it is followed.
//
// A field that is not there has nothing to check, and calling through a nil
// pointer would stop the program rather than report anything — which is what
// required is for, and which is the author's to ask for.
func TestANilStructIsNotFollowed(t *testing.T) {
	held := source(t, written(t, "Nested"))

	if !strings.Contains(held, "if v.Work != nil {\n\t\tif err := v.Work.Validate(); err != nil {") {
		t.Errorf("a pointer is followed without being asked about:\n%s", held)
	}
}

// The author's own check is called where the field's own rules are checked, and
// what it says is reported under that field.
func TestTheAuthorsOwnCheckIsCalled(t *testing.T) {
	held := source(t, written(t, "Hooked"))

	for _, want := range []string{
		"if err := v.ValidateToken(); err != nil {",
		`failed = append(failed, ValidationError{Path: "Token", Cause: err})`,
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the check does not hold %q:\n%s", want, held)
		}
	}
	if strings.Contains(held, "func (v Hooked) ValidateToken()") {
		t.Errorf("the author's own check was written a second time:\n%s", held)
	}
}

// A struct with nothing to check gets no method, and nothing that holds it
// calls one.
//
// A method that returns nothing however it is called is a method in a committed
// file that says nothing, and a call to it is a line a reader has to follow to
// find that out.
func TestAStructWithNothingToCheckGetsNothing(t *testing.T) {
	held := source(t, written(t, "Holder"))

	if strings.Contains(held, "func (v Quiet) Validate()") {
		t.Errorf("a struct with no rules was given a check:\n%s", held)
	}
	if strings.Contains(held, "v.Inside.Validate()") {
		t.Errorf("a check that is not written is called anyway:\n%s", held)
	}
}

// The subject gets a check whatever it has to say, because the declaration
// asked for one.
func TestTheSubjectAlwaysGetsACheck(t *testing.T) {
	held := source(t, written(t, "Quiet"))

	if !strings.Contains(held, "func (v Quiet) Validate() error {") {
		t.Errorf("a subject with no rules was given no check:\n%s", held)
	}
}

// What every subject generates compiles, which is the claim no assertion about
// a substring makes.
func TestWhatIsWrittenCompiles(t *testing.T) {
	for _, name := range []string{
		"Person", "Scalars", "Composites", "Named", "Nested", "Hooked", "Holder", "Quiet",
		"Carrying", "Mistaken", "Zeroed", "Ranged", "Elsewhere",
	} {
		t.Run(name, func(t *testing.T) {
			held := source(t, written(t, name))

			sources := []goldentest.Source{
				{Name: "model.go", Content: fixtureSource(t)},
				{Name: "zz_forge.go", Content: []byte(held), Generated: true},
			}

			pkg := goldentest.Package{
				Path:     modelPkg,
				Files:    sources,
				Requires: []goldentest.Package{besideFixture(t)},
			}
			if err := goldentest.Compiles(pkg); err != nil {
				t.Errorf("the check for %s does not compile: %v", name, err)
			}
		})
	}
}

// A struct the subject reaches in a package of its own is checked by a function
// rather than by a method, and everything holding one calls that function.
//
// Go puts a method only where its type is, so a check written as a method there
// is not a check that is missing something — it is a file that does not
// compile. A field holding one by pointer is dereferenced at the call, since a
// function takes the value where a method took the receiver.
func TestAStructInAnotherPackageIsCheckedByAFunction(t *testing.T) {
	held := source(t, written(t, "Elsewhere"))

	if !strings.Contains(held, "func validateOtherPlace(v other.Place) error {") {
		t.Errorf("the check for a struct of another package is not a function:\n%s", held)
	}
	if strings.Contains(held, "func (v other.Place)") {
		t.Errorf("a method was declared on another package's type:\n%s", held)
	}

	for _, want := range []string{
		"if err := validateOtherPlace(v.Home); err != nil {",
		"if err := validateOtherPlace(*v.Work); err != nil {",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the check does not hold %q:\n%s", want, held)
		}
	}
}

// A struct that declares something called Validate which is not a check is
// reported rather than worked around.
//
// Both ways out are worse. Writing the check would redeclare the name, in a
// file the author cannot edit; not writing it would leave the type without the
// method every other type in the closure has, and the call sites would not
// compile either.
func TestAStructWhoseValidateIsNotOne(t *testing.T) {
	_, err := generating(t, "Wrongly")
	if err == nil {
		t.Fatal("a check was written beside a method of the same name")
	}

	reported, ok := diag.From(err)
	if !ok {
		t.Fatalf("%v is not a diagnostic", err)
	}
	if got, want := reported.Code.String(), "FRG2019"; got != want {
		t.Errorf("reported as %s, want %s: %s", got, want, reported.Message)
	}
	if !strings.Contains(reported.Message, "Confused") {
		t.Errorf("the complaint does not name the type:\n%s", reported.Message)
	}
	if !strings.Contains(reported.Hint, "rename the method") {
		t.Errorf("the hint does not say what to do:\n%s", reported.Hint)
	}
}

// A struct in another package that checks itself is called rather than written
// for, which is the same rule a struct in this one gets.
//
// It matters most where the type is not this package's. What such a type checks
// is usually an invariant on a field nothing here can read, so a check derived
// from the tags would pass a value the type itself refuses — and would do it
// silently, since both are called Validate and both answer with an error.
func TestAStructInAnotherPackageThatChecksItself(t *testing.T) {
	held := source(t, written(t, "Trusting"))

	if !strings.Contains(held, "if err := v.Where.Validate(); err != nil {") {
		t.Errorf("a type in another package that checks itself is not called:\n%s", held)
	}
	if strings.Contains(held, "validateOtherGuarded") {
		t.Errorf("a check the author wrote was written a second time:\n%s", held)
	}
}

// A rule on an unexported field of a struct in another package is left where it
// was written, because generated code here cannot read the field.
func TestARuleOnWhatAnotherPackageKeepsToItself(t *testing.T) {
	held := source(t, written(t, "Elsewhere"))

	if strings.Contains(held, "unread") {
		t.Errorf("a check names a field it cannot reach:\n%s", held)
	}
}

// A field whose type brings a check of its own is checked by calling it, even
// where that type is not a struct this run writes for.
//
// The two answers come from different places and have to agree: a struct in the
// closure is answered by the plan, because a method a previous run wrote is not
// the author's; everything else is answered by asking the type, because forge
// has never written anything for it.
func TestATypeThatChecksItselfIsCalled(t *testing.T) {
	held := source(t, written(t, "Carrying"))

	if !strings.Contains(held, "if err := v.Where.Validate(); err != nil {") {
		t.Errorf("a type that checks itself is not called:\n%s", held)
	}
	if strings.Contains(held, "func (v Postcode) Validate()") {
		t.Errorf("a check the author wrote was written a second time:\n%s", held)
	}
}

// A method called Validate that is not a check is not called as one.
//
// The name alone would be enough to reach for and wrong to: a method taking an
// argument is somebody else's method that happens to share a name, and calling
// it would not compile.
func TestAMethodThatOnlyLooksLikeACheckIsNotCalled(t *testing.T) {
	held := source(t, written(t, "Mistaken"))

	if strings.Contains(held, "v.What.Validate()") {
		t.Errorf("a method that is not a check was called as one:\n%s", held)
	}
}

// The zero of a type the language has no literal for is written as the type.
func TestTheZeroOfSomethingWithNoLiteral(t *testing.T) {
	held := source(t, written(t, "Zeroed"))

	for _, want := range []string{"v.Point == (Coordinate{})", "v.Window == ([2]int{})"} {
		if !strings.Contains(held, want) {
			t.Errorf("the check does not hold %q:\n%s", want, held)
		}
	}
}

// oneof over fractions writes the fractions.
func TestOneOfOverFractions(t *testing.T) {
	held := source(t, written(t, "Ranged"))

	if !strings.Contains(held, "v.Share != 0.25 && v.Share != 0.5 && v.Share != 1.0") {
		t.Errorf("the check does not compare the fractions:\n%s", held)
	}
}
