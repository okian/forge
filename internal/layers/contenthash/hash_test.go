package contenthash_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/goldentest"
	"github.com/okian/forge/plugin"
)

// Each of the things the language gives a value directly is mixed in as itself,
// through a conversion, so that a name over one of them reaches the same
// arithmetic as the thing it names.
func TestWhatTheLanguageGivesDirectly(t *testing.T) {
	held := source(t, written(t, "Flat"))

	for _, want := range []string{
		"h = fnvString(h, string(v.Name))",
		"h = fnvUint64(h, uint64(v.Count))",
		"h = fnvUint64(h, uint64(v.Size))",
		"h = fnvFloat(h, float64(v.Ratio))",
		"h = fnvFloat(h, float64(v.Small))",
		"h = fnvBool(h, bool(v.Ready))",
		"h = fnvFloat(h, real(complex128(v.Signal)))",
		"h = fnvFloat(h, imag(complex128(v.Signal)))",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the hash does not hold %q:\n%s", want, held)
		}
	}
}

// Fields are mixed in the order they are declared, which is what makes two
// fields of one type tell a value from the one with them the other way round.
func TestFieldsAreMixedInDeclarationOrder(t *testing.T) {
	held := source(t, written(t, "Flat"))

	name := strings.Index(held, "string(v.Name)")
	count := strings.Index(held, "uint64(v.Count)")

	if name < 0 || count < 0 || name > count {
		t.Errorf("the fields are not mixed in the order they are declared:\n%s", held)
	}
}

// A slice says whether it is there and how long it is before it says what is in
// it, and a pointer says whether there is anything there at all.
//
// Both are values a hash would otherwise confuse: a nil slice and an empty one
// are different values, and so are a nil pointer and a pointer to a zero.
func TestWhatIsThereAndHowMuchOfIt(t *testing.T) {
	held := source(t, written(t, "Referring"))

	for _, want := range []string{
		"h = fnvBool(h, v.Tags != nil)",
		"h = fnvUint64(h, uint64(len(v.Tags)))",
		"h = fnvBool(h, v.Count != nil)",
		"if v.Count != nil {\n\t\th = fnvUint64(h, uint64(*v.Count))\n\t}",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the hash does not hold %q:\n%s", want, held)
		}
	}
}

// An array carries no length, because its length is part of its type and so is
// the same for every value of it.
func TestAnArrayCarriesNoLength(t *testing.T) {
	held := source(t, written(t, "Referring"))

	if strings.Contains(held, "len(v.Fixed)") {
		t.Errorf("an array's length is mixed in, though every value of the type has the same one:\n%s", held)
	}
	if !strings.Contains(held, "for _, one := range v.Fixed {") {
		t.Errorf("an array's elements are not mixed in:\n%s", held)
	}
}

// A map's entries are totalled rather than chained, because ranging over one is
// deliberately unordered and a chained hash would give a map as many answers as
// it has orders to be walked in.
func TestAMapIsTotalledRatherThanChained(t *testing.T) {
	held := source(t, written(t, "Referring"))

	for _, want := range []string{
		"var total uint64",
		"for key, one := range v.Lookup {",
		"part := fnvSeed",
		"total += part",
		"h = fnvUint64(h, uint64(len(v.Lookup)))",
		"h = fnvUint64(h, total)",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the hash does not hold %q:\n%s", want, held)
		}
	}
}

// Two maps side by side get an accumulator each, because they are in one scope
// and a name declared twice there is a function that does not compile.
//
// The nesting cannot decide it: two maps beside each other are at the same
// depth. Everything else a map binds lives inside its own loop, where two of
// one name are two variables, so this is the one name that has to be counted.
func TestEveryMapGetsAnAccumulatorOfItsOwn(t *testing.T) {
	held := source(t, written(t, "Twice"))

	for _, want := range []string{"var total uint64", "var total2 uint64", "var total3 uint64"} {
		if got := strings.Count(held, want); got != 1 {
			t.Errorf("%q appears %d times, want once:\n%s", want, got, held)
		}
	}

	// A map inside a map is counted like any other, and totals into the entry
	// its own map is an entry of rather than into the hash.
	if !strings.Contains(held, "part = fnvUint64(part, total4)") {
		t.Errorf("a nested map does not total into the entry that holds it:\n%s", held)
	}
}

// A reference whose elements are themselves references is followed all the way
// down rather than one level.
func TestAHashGoesAllTheWayDown(t *testing.T) {
	held := source(t, written(t, "Deep"))

	for _, want := range []string{
		"for _, one1 := range one {",
		"if *v.Deeper != nil {",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the hash does not hold %q:\n%s", want, held)
		}
	}
}

// A struct another struct holds is hashed by its own method, however it is
// held, which is what makes the output one method per type rather than one
// enormous function.
func TestAStructIsHashedByItsOwn(t *testing.T) {
	held := source(t, written(t, "Holding"))

	for _, want := range []string{
		"h = fnvUint64(h, v.Home.Hash())",
		"h = fnvUint64(h, (*v.Work).Hash())",
		"h = fnvUint64(h, one.Hash())",
		"part = fnvUint64(part, one.Hash())",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the hash does not hold %q:\n%s", want, held)
		}
	}
}

// A dereference is parenthesised where a selector follows it and left alone
// where nothing does, because without the parentheses *p.Hash() is a
// dereference of what p.Hash() answered with.
func TestADereferenceBindsBeforeTheCall(t *testing.T) {
	held := source(t, written(t, "Holding"))

	if strings.Contains(held, "*v.Work.Hash()") {
		t.Errorf("a dereference is written where the selector would take it first:\n%s", held)
	}
}

// A type that reaches itself produces a method that calls itself, which is a
// finite amount of code however deep the values go.
func TestATypeThatReachesItselfTerminates(t *testing.T) {
	held := source(t, written(t, "Node"))

	if strings.Count(held, "func (v Node) Hash() uint64 {") != 1 {
		t.Errorf("the method was written more than once:\n%s", held)
	}
	if !strings.Contains(held, "h = fnvUint64(h, (*v.Next).Hash())") {
		t.Errorf("a self-referential field is not hashed by the type's own method:\n%s", held)
	}
}

// A struct written in place has no name to hang a method on, so it is taken
// apart where it is used.
func TestAStructWrittenInPlaceIsTakenApart(t *testing.T) {
	held := source(t, written(t, "Anonymous"))

	for _, want := range []string{
		"h = fnvUint64(h, uint64(v.At.Line))",
		"h = fnvUint64(h, uint64(v.At.Column))",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the hash does not hold %q:\n%s", want, held)
		}
	}
}

// A type whose author wrote the hash is called rather than written a second
// time.
func TestAHandWrittenHashStaysAuthoritative(t *testing.T) {
	held := source(t, written(t, "Owning"))

	if !strings.Contains(held, "h = fnvUint64(h, v.Held.Hash())") {
		t.Errorf("a type that hashes itself is not called:\n%s", held)
	}
	if strings.Contains(held, "func (v Counter) Hash()") {
		t.Errorf("a hash the author wrote was written a second time:\n%s", held)
	}
}

// A subject that is not a struct is hashed as whatever its name is a name for,
// which is the shape an enumeration and a named slice both have.
func TestASubjectThatIsNotAStruct(t *testing.T) {
	number := source(t, written(t, "Age"))
	if !strings.Contains(number, "func (v Age) Hash() uint64 {\n\th := fnvSeed\n\th = fnvUint64(h, uint64(v))") {
		t.Errorf("a name over a number is not hashed as the number:\n%s", number)
	}

	names := source(t, written(t, "Names"))
	if !strings.Contains(names, "for _, one := range v {") {
		t.Errorf("a name over a slice is not walked:\n%s", names)
	}
}

// A struct the subject reaches in a package of its own is hashed by a function
// rather than by a method, and everything holding one calls that function.
//
// Go puts a method only where its type is, so a hash written as a method there
// is not a hash that is missing something — it is a file that does not compile.
func TestAStructInAnotherPackageIsHashedByAFunction(t *testing.T) {
	held := source(t, written(t, "Elsewhere"))

	if !strings.Contains(held, "func hashOtherPlace(v other.Place) uint64 {") {
		t.Errorf("the hash for a struct of another package is not a function:\n%s", held)
	}
	if strings.Contains(held, "func (v other.Place)") {
		t.Errorf("a method was declared on another package's type:\n%s", held)
	}

	for _, want := range []string{
		"h = fnvUint64(h, hashOtherPlace(v.Home))",
		"h = fnvUint64(h, hashOtherPlace(*v.Work))",
		"h = fnvUint64(h, hashOtherPlace(one))",
		"part = fnvUint64(part, hashOtherPlace(one))",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the hash does not hold %q:\n%s", want, held)
		}
	}
}

// The arithmetic is contributed once however many types were hashed, because a
// package holding two of any of these functions does not compile.
func TestTheArithmeticIsContributedOnce(t *testing.T) {
	held := source(t, written(t, "Holding"))

	for _, want := range []string{"func fnvUint64(", "func fnvString(", "func fnvBool(", "func fnvFloat("} {
		if got := strings.Count(held, want); got != 1 {
			t.Errorf("%q is written %d times, want once:\n%s", want, got, held)
		}
	}
}

// What every subject generates compiles, which is the claim no assertion about
// a substring makes.
func TestWhatIsWrittenCompiles(t *testing.T) {
	for _, name := range []string{
		"Flat", "Referring", "Deep", "Holding", "Node", "Anonymous", "Owning",
		"Marked", "Elsewhere", "Age", "Names", "Twice",
	} {
		t.Run(name, func(t *testing.T) {
			sources := []goldentest.Source{
				{Name: "model.go", Content: fixtureSource(t)},
				{Name: "zz_forge.go", Content: []byte(source(t, written(t, name))), Generated: true},
			}

			held := goldentest.Package{
				Path:     modelPkg,
				Files:    sources,
				Requires: []goldentest.Package{besideFixture(t)},
			}
			if err := goldentest.Compiles(held); err != nil {
				t.Errorf("the hash for %s does not compile: %v", name, err)
			}
		})
	}
}

// A value nothing can hash by its content is refused rather than left out
// behind the author's back.
//
// A hash that quietly skipped what it could not read would be worse than none
// at all: it would call two different values the same, and the program that
// relied on it would be wrong in a way no test of the hash could find.
func TestWhatCannotBeHashed(t *testing.T) {
	cases := map[string]struct {
		code  string
		says  string
		hints string
	}{
		"Opaque":      {"FRG2017", "whose identity is not its content", "//forge:hash ignore"},
		"Sealing":     {"FRG2017", "unexported fields", "//forge:hash ignore"},
		"Cyclic":      {"FRG2017", "contains itself with no struct in between", "//forge:hash ignore"},
		"Mistaken":    {"FRG2018", "does not answer with a number", "rename the method"},
		"Misoptioned": {"FRG3024", "whatever is not an option", "takes ignore and nothing else"},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := generating(t, name)
			if err == nil {
				t.Fatalf("a hash was written for %s", name)
			}

			reported, ok := plugin.From(err)
			if !ok {
				t.Fatalf("%v is not a diagnostic", err)
			}

			if got := reported.Code.String(); got != want.code {
				t.Errorf("%s is reported as %s, want %s: %s", name, got, want.code, reported.Message)
			}
			if !strings.Contains(reported.Message, want.says) {
				t.Errorf("the complaint about %s does not mention %q:\n%s", name, want.says, reported.Message)
			}
			if !strings.Contains(reported.Hint, want.hints) {
				t.Errorf("the hint for %s does not say %q:\n%s", name, want.hints, reported.Hint)
			}
		})
	}
}

// A refusal about a type in another package points at the field that reached
// it, which is a line the author can act on.
//
// The member that cannot be read is somewhere they cannot edit, so a complaint
// pointing there would be telling them to write a directive in somebody else's
// module. What they can do is stop holding the type, or say so at the field.
func TestARefusalPointsWhereTheAuthorCanAct(t *testing.T) {
	_, err := generating(t, "Nested")
	if err == nil {
		t.Fatal("a hash was written over a value part of which could not be read")
	}

	reported, ok := plugin.From(err)
	if !ok {
		t.Fatalf("%v is not a diagnostic", err)
	}
	if !strings.Contains(reported.Message, "Held is") {
		t.Errorf("the complaint does not name the field that reached it:\n%s", reported.Message)
	}
	if !strings.HasSuffix(reported.Pos.Filename, "values/model/model.go") {
		t.Errorf("the complaint points at %s, which is not the author's file", reported.Pos)
	}
}

// A subject whose content this package cannot read is refused too, and it is
// the one refusal with no field to point at.
//
// A field reaching such a type is reported where it is used; a subject is what
// the declaration itself asked for, so the report points at the declaration and
// says something a field could not be told — that there is no directive to
// write, only a package to move to.
func TestASubjectThisPackageCannotReadTheWholeOf(t *testing.T) {
	_, err := asking(t, otherPkg, "Sealed")
	if err == nil {
		t.Fatal("a hash was written over a value half of which could not be read")
	}

	reported, ok := plugin.From(err)
	if !ok {
		t.Fatalf("%v is not a diagnostic", err)
	}
	if got, want := reported.Code.String(), "FRG2017"; got != want {
		t.Errorf("reported as %s, want %s: %s", got, want, reported.Message)
	}
	if !strings.Contains(reported.Message, "Sealed") {
		t.Errorf("the complaint does not name the subject:\n%s", reported.Message)
	}
	if !strings.Contains(reported.Hint, "declare this one in the package being generated into") {
		t.Errorf("the hint offers a directive that would not help:\n%s", reported.Hint)
	}
}

// And one this package can read is written for, so the refusal is about what
// can be reached rather than about the package.
func TestASubjectElsewhereThisPackageCanRead(t *testing.T) {
	unit, err := asking(t, otherPkg, "Place")
	if err != nil {
		t.Fatalf("a subject of another package with nothing hidden was refused: %v", err)
	}

	if held := source(t, unit); !strings.Contains(held, "func hashOtherPlace(v other.Place) uint64 {") {
		t.Errorf("the hash for a subject of another package is not a function:\n%s", held)
	}
}

// Every field whose identity is not its content is reported, rather than one of
// them standing in for the others — including the second field of a type
// already refused once.
//
// A form is decided once and looked up thereafter, so the second channel would
// otherwise be silently skipped: the author would fix what they were told
// about, run again, and be told about the next one.
func TestEveryOpaqueFieldIsReported(t *testing.T) {
	_, err := generating(t, "Opaque")
	if err == nil {
		t.Fatal("a hash was written for a subject of things nothing can hash")
	}

	for _, want := range []string{"Anything", "Updates", "Pending", "Do", "Undo", "Where"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s is not reported:\n%v", want, err)
		}
	}
}

// A uintptr is refused, though the type checker calls it an integer.
//
// What it holds is where a value is rather than what it is: two runs of one
// program give one value two addresses, and the collector may move a value
// within a run. Hashing it as a number would compile, run, and quietly break
// the one claim this layer makes.
func TestAnAddressIsNotContent(t *testing.T) {
	_, err := generating(t, "Opaque")
	if err == nil {
		t.Fatal("a hash was written over an address")
	}
	if !strings.Contains(err.Error(), "Where is of type uintptr") {
		t.Errorf("a uintptr is not refused:\n%v", err)
	}
	if !strings.Contains(err.Error(), "holds where a value is rather than what it is") {
		t.Errorf("the complaint does not say why:\n%v", err)
	}
}

// And saying what was meant is what makes them writable.
func TestSayingWhatWasMeantMakesThemWritable(t *testing.T) {
	held := source(t, written(t, "Marked"))

	if strings.Contains(held, "v.Anything") || strings.Contains(held, "v.LastRead") {
		t.Errorf("a field asked to be left out is in the hash:\n%s", held)
	}
	if !strings.Contains(held, "h = fnvString(h, string(v.Name))") {
		t.Errorf("leaving one field out stopped the others being hashed:\n%s", held)
	}
}
