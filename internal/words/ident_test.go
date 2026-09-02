package words_test

import (
	"testing"

	"github.com/okian/forge/internal/words"
)

// Every kind of identifier forge writes, held to the shape its kind decides and
// the case its declaration decides.
func TestSpellingEachKindOfName(t *testing.T) {
	for _, one := range []struct {
		what     string
		kind     words.Kind
		exported bool
		parts    []string
		want     string
	}{
		{"a type", words.KindType, true, []string{"persons"}, "Persons"},
		{"an unexported type", words.KindType, false, []string{"Persons"}, "persons"},

		{"an interface", words.KindInterface, true, []string{"validate"}, "Validator"},
		{"an interface for a subject", words.KindInterface, true, []string{"person", "notify"}, "PersonNotifier"},

		{"a constructor", words.KindFunc, true, []string{"new", "Persons"}, "NewPersons"},
		{"an unexported constructor", words.KindFunc, false, []string{"new", "persons"}, "newPersons"},
		{"a second constructor", words.KindFunc, true, []string{"new", "Persons", "from", "slice"}, "NewPersonsFromSlice"},

		{"a method", words.KindMethod, true, []string{"sorted", "by", "userId"}, "SortedByUserID"},
		{"a bool method", words.KindMethod, true, []string{"is", "Active"}, "IsActive"},

		{"a field", words.KindField, false, []string{"held"}, "held"},
		{"an exported field", words.KindField, true, []string{"user", "id"}, "UserID"},

		{"a sentinel error", words.KindError, true, []string{"persons", "full"}, "ErrPersonsFull"},
		{"an unexported sentinel", words.KindError, false, []string{"persons", "full"}, "errPersonsFull"},
		{"a sentinel already named one", words.KindError, true, []string{"ErrPersonsFull"}, "ErrPersonsFull"},
		{"an error type", words.KindType, true, []string{"validation", "errors"}, "ValidationErrors"},

		{"an enumeration member", words.KindConst, true, []string{"Status", "active"}, "StatusActive"},

		{"a variable", words.KindVar, false, []string{"persons", "pattern"}, "personsPattern"},

		{"a receiver", words.KindReceiver, false, []string{"Persons"}, "p"},
		{"a receiver of two words", words.KindReceiver, false, []string{"HomeAddress"}, "ha"},
		{"a receiver of three", words.KindReceiver, false, []string{"UserIDToken"}, "ui"},
		{"a receiver of nothing", words.KindReceiver, false, nil, ""},

		{"a local", words.KindLocal, true, []string{"Held", "Value"}, "heldValue"},

		{"a type parameter", words.KindTypeParam, true, []string{"T"}, "T"},
		{"a named type parameter", words.KindTypeParam, true, []string{"element"}, "E"},
		{"a type parameter of nothing", words.KindTypeParam, true, []string{"_"}, "T"},
	} {
		if got := words.Spell(one.kind, one.exported, one.parts...); got != one.want {
			t.Errorf("%s: Spell(%v, %v, %q) = %q, want %q",
				one.what, one.kind, one.exported, one.parts, got, one.want)
		}
	}
}

// Every method of one type takes the same receiver, which is the part a reader
// notices when it is not true and the part no layer generating one method at a
// time can hold on its own.
func TestOneTypeTakesOneReceiver(t *testing.T) {
	first := words.Spell(words.KindReceiver, false, "Persons")

	for _, method := range []string{"Len", "Push", "SortedByAge"} {
		if got := words.Spell(words.KindReceiver, false, "Persons"); got != first {
			t.Errorf("the receiver for %s is %q, want %q", method, got, first)
		}
	}
}

func TestWritingAGoNameWithItsFirstWordLowered(t *testing.T) {
	for _, one := range []struct {
		name           string
		camel, low, up string
	}{
		// The cases the plugin surface documents, which layer authors read.
		{"ID", "id", "iD", "ID"},
		{"Name", "name", "name", "Name"},
		{"StatusOK", "statusOK", "statusOK", "StatusOK"},
		{"JSONValue", "jsonValue", "jSONValue", "JSONValue"},
		{"city", "city", "city", "City"},

		// A word at a time all the way down, which is what an initialism made
		// plural needs and what a run of capitals that is not one needs too.
		{"IDs", "ids", "iDs", "IDs"},
		{"ABC", "abc", "aBC", "ABC"},

		// Only the case changes: a wire member keeps its own spelling and its
		// own separators.
		{"UserId", "userId", "userId", "UserId"},
		{"http_server", "http_server", "http_server", "Http_server"},
		{"", "", "", ""},
		{"_x", "_x", "_x", "_x"},
	} {
		if got := words.Camel(one.name); got != one.camel {
			t.Errorf("Camel(%q) = %q, want %q", one.name, got, one.camel)
		}
		if got := words.Lower(one.name); got != one.low {
			t.Errorf("Lower(%q) = %q, want %q", one.name, got, one.low)
		}
		if got := words.Upper(one.name); got != one.up {
			t.Errorf("Upper(%q) = %q, want %q", one.name, got, one.up)
		}
	}
}

func TestAskingAQuestionOfABoolean(t *testing.T) {
	for name, want := range map[string]string{
		"Active":   "IsActive",
		"Empty":    "IsEmpty",
		"IsActive": "IsActive",
		"HasKey":   "HasKey",
		"CanRetry": "CanRetry",
		"":         "Is",
	} {
		if got := words.Question(name); got != want {
			t.Errorf("Question(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestNamesGoWillNotTake(t *testing.T) {
	for _, one := range []struct {
		name   string
		reason string
	}{
		{"Persons", ""},
		{"", "is empty"},
		{"_", "is the blank identifier, which nothing may be declared as"},
		{"range", "is a Go keyword"},
		{"type", "is a Go keyword"},
		{"len", "is a predeclared identifier"},
		{"any", "is a predeclared identifier"},
		{"error", "is a predeclared identifier"},
		{"2fast", "does not begin with a letter"},
		{"has-a-dash", "holds a character a Go identifier may not"},
	} {
		reason, ok := words.Safe(one.name)
		if ok != (one.reason == "") || reason != one.reason {
			t.Errorf("Safe(%q) = %q, %v; want %q", one.name, reason, ok, one.reason)
		}
	}
}

func TestNamesTheStandardLibraryHasTaken(t *testing.T) {
	if _, is := words.Standard("String"); !is {
		t.Error("Standard(String) does not report the method fmt prints through")
	}
	if _, is := words.Standard("Persons"); is {
		t.Error("Standard(Persons) reports a meaning it does not have")
	}
}

// A name built around one that is already spelled keeps that name exactly.
// Forge does not derive a declaration's name and must not respell one inside a
// name derived from it.
func TestBuildingANameAroundADeclaration(t *testing.T) {
	for _, one := range []struct {
		what     string
		exported bool
		before   string
		held     string
		after    []string
		want     string
	}{
		{"a constructor", true, "new", "Persons", nil, "NewPersons"},
		{"an unexported constructor", false, "new", "persons", nil, "newPersons"},
		{"a sentinel error", true, "err", "Persons", []string{"full"}, "ErrPersonsFull"},
		{"an unexported sentinel", false, "err", "persons", []string{"full"}, "errPersonsFull"},
		{"a helper type", true, "", "Persons", []string{"builder"}, "PersonsBuilder"},
		{"an unexported helper", false, "", "Roster", []string{"held"}, "rosterHeld"},

		// The declaration's own spelling survives, however forge would have
		// spelled it: a constructor that renamed the type it builds would read
		// as belonging to something else.
		{"a type spelled the author's way", true, "new", "MyIdThing", nil, "NewMyIdThing"},
		{"and unexported", false, "", "myIdThing", []string{"held"}, "myIdThingHeld"},
	} {
		if got := words.Around(one.exported, one.before, one.held, one.after...); got != one.want {
			t.Errorf("%s: Around(%v, %q, %q, %q) = %q, want %q",
				one.what, one.exported, one.before, one.held, one.after, got, one.want)
		}
	}
}
