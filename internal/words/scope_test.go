package words_test

import (
	"errors"
	"testing"

	"github.com/okian/forge/internal/words"
)

// The author's own declarations are reserved before any layer writes, so a
// package that already declares NewPersons gets a report rather than a second
// one.
func TestAPackageThatAlreadyDeclaresTheName(t *testing.T) {
	var scope words.Scope

	if err := scope.Reserve("NewPersons"); err != nil {
		t.Fatalf("reserving the author's own declaration: %v", err)
	}

	name, err := scope.Declare(words.KindFunc, true, "new", "Persons")
	if err == nil {
		t.Fatalf("Declare returned %q over a name the package already declares", name)
	}

	var clash *words.ClashError
	if !errors.As(err, &clash) {
		t.Fatalf("Declare returned %T, want a clash", err)
	}
	if clash.Name != "NewPersons" || clash.Held == "" || clash.Hint == "" {
		t.Errorf("the clash %+v does not say what holds the name or what to do", clash)
	}
	if clash.Error() == "" {
		t.Error("the clash prints as nothing")
	}
}

// Two layers reaching one package-level name is the same failure wearing
// another face, and it is reported rather than resolved.
func TestTwoLayersReachingOneName(t *testing.T) {
	var scope words.Scope

	if _, err := scope.Declare(words.KindError, true, "persons", "full"); err != nil {
		t.Fatalf("the first layer: %v", err)
	}
	if _, err := scope.Declare(words.KindError, true, "ErrPersonsFull"); err == nil {
		t.Error("the second layer got the name the first one has")
	}

	if !scope.Taken("ErrPersonsFull") {
		t.Error("the scope does not report the name as taken")
	}
	if scope.Taken("ErrPersonsEmpty") {
		t.Error("the scope reports a name nothing has taken")
	}
}

// A type cannot have a field and a method of one name. This is not style; it
// does not compile, and no layer generating one method at a time can see it.
func TestAFieldAndAMethodOfOneName(t *testing.T) {
	var scope words.Scope

	if err := scope.ReserveMember("Persons", "Age"); err != nil {
		t.Fatalf("reserving the author's own field: %v", err)
	}
	if _, err := scope.Member("Persons", words.KindMethod, true, "Age"); err == nil {
		t.Error("a method took the name of a field on the same type")
	}

	// A member of one type says nothing about a member of another.
	if _, err := scope.Member("Ages", words.KindMethod, true, "Age"); err != nil {
		t.Errorf("a method on a second type was refused: %v", err)
	}
	if !scope.TakenMember("Ages", "Age") {
		t.Error("the scope does not report the member as taken")
	}
	if scope.TakenMember("Persons", "Name") {
		t.Error("the scope reports a member nothing has taken")
	}
}

// A method derived from a field called Len is a type that sorts wrongly and
// compiles, which is worse than one that does not compile.
func TestAMethodTheStandardLibraryHasNamed(t *testing.T) {
	var scope words.Scope

	if _, err := scope.Member("Persons", words.KindMethod, true, "Len"); err == nil {
		t.Error("a generated method took a name sort.Interface counts through")
	}

	// A layer that means to write one says so, and then writes it.
	if err := scope.ReserveMember("Persons", "String"); err != nil {
		t.Errorf("reserving a name a layer means to write: %v", err)
	}
	if _, err := scope.Member("Persons", words.KindMethod, true, "String"); err == nil {
		t.Error("the reserved name was handed out a second time")
	}
}

// A derived name that lands on a keyword is refused with the reason, not
// quietly suffixed.
func TestANameGoWillNotTake(t *testing.T) {
	var scope words.Scope

	for _, one := range []string{"range", "len", "type"} {
		if _, err := scope.Declare(words.KindType, false, one); err == nil {
			t.Errorf("Declare took %q", one)
		}
		if err := scope.Reserve(one); err == nil {
			t.Errorf("Reserve took %q", one)
		}
	}
}

// Nothing here depends on anything but the order the layers asked in, which is
// the stack's own order.
func TestTheSameInputGivesTheSameNames(t *testing.T) {
	run := func() []string {
		var scope words.Scope
		var out []string

		for _, parts := range [][]string{{"new", "Persons"}, {"Persons", "Builder"}, {"persons", "pattern"}} {
			name, err := scope.Declare(words.KindFunc, true, parts...)
			if err != nil {
				t.Fatalf("declaring %q: %v", parts, err)
			}
			out = append(out, name)
		}
		return out
	}

	first, second := run(), run()
	for at := range first {
		if first[at] != second[at] {
			t.Errorf("run one gave %q and run two gave %q", first[at], second[at])
		}
	}
}

// A local can always be renamed, because nothing outside the function can see
// it — so a block resolves rather than refusing. What it must never do is
// shadow a package the file imports.
func TestALocalThatWouldShadowAnImport(t *testing.T) {
	var scope words.Scope
	block := scope.Block("slices", "v", "p")

	if got := block.Declare("slices"); got != "slices2" {
		t.Errorf("a local named for an imported package is %q, want slices2", got)
	}
	if got := block.Declare("held"); got != "held" {
		t.Errorf("a local with a free name is %q, want held", got)
	}
	if got := block.Declare("held"); got != "held2" {
		t.Errorf("a second local of one name is %q, want held2", got)
	}
	if got := block.Declare("held"); got != "held3" {
		t.Errorf("a third local of one name is %q, want held3", got)
	}
	if got := block.Declare("range"); got != "range2" {
		t.Errorf("a local named for a keyword is %q, want range2", got)
	}
	if got := block.Declare(); got != "v2" {
		t.Errorf("a local with no name at all is %q, want v2", got)
	}

	if !block.Shadows("slices") {
		t.Error("the block does not report an imported package as shadowed")
	}
	if block.Shadows("elsewhere") {
		t.Error("the block reports a free name as shadowed")
	}
}

// A walk over a nested value numbers its locals by depth, so that the number
// says which level a variable belongs to and two loops at one depth may share
// a name.
func TestNamingTheLocalsOfANestedWalk(t *testing.T) {
	block := words.Locals("slices", "v")

	for _, one := range []struct {
		depth int
		parts []string
		want  string
	}{
		{0, []string{"one"}, "one"},
		{1, []string{"one"}, "one1"},
		{2, []string{"one"}, "one2"},

		// A sibling loop at the same depth is in its own scope and takes the
		// same name, which is what keeps a wide struct from counting up.
		{1, []string{"one"}, "one1"},

		// A name the file already binds is not one a body may take, however
		// deep it is.
		{0, []string{"slices"}, "slices2"},
		{0, []string{"v"}, "v2"},
	} {
		if got := block.Nested(one.depth, one.parts...); got != one.want {
			t.Errorf("Nested(%d, %q) = %q, want %q", one.depth, one.parts, got, one.want)
		}
	}
}
