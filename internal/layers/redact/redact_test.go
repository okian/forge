package redact_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/layers/redact"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/shape"
)

// A tagged field logs a fixed string and the rest log themselves.
//
// The fixed string is the whole point: not the value shortened, starred or
// hashed, because a length is something, a prefix is more, and two records
// holding one secret are told apart by any hash of it.
func TestATaggedFieldIsMasked(t *testing.T) {
	held := source(t, written(t, "Account"))

	for _, want := range []string{
		`func (v Account) LogValue() slog.Value {`,
		`slog.String("Token", "[redacted]")`,
		`slog.String("Name", v.Name)`,
		`slog.Int64("Age", int64(v.Age))`,
		`slog.Float64("Ratio", v.Ratio)`,
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the log value does not hold %q:\n%s", want, held)
		}
	}

	// The value itself never appears, which is the claim the whole layer makes.
	if strings.Contains(held, "v.Token") {
		t.Errorf("the redacted field's value is written into the log value:\n%s", held)
	}

	compiles(t, held)
}

// An unexported field is left out, which is what the method is for as much as
// masking is.
//
// A handler cannot read one, so it formats the value with %+v and prints every
// field there is — which means a type with no log value shows a caller more
// than the package's own API does. A type with one shows what its author chose
// to offer.
func TestAnUnexportedFieldIsNotLogged(t *testing.T) {
	held := source(t, written(t, "Account"))

	if strings.Contains(held, "held") {
		t.Errorf("an unexported field reached the log value:\n%s", held)
	}
}

// A secret one struct down gets that struct its own log value.
//
// The difference between this layer and the method a redact tag earns on its
// own. slog resolves one value at a time, so an outer method that hands the
// inner value over unchanged has done nothing at all: slog reaches into the
// struct it was given and prints what is in it, tag or no tag.
func TestASecretOneStructDown(t *testing.T) {
	held := source(t, written(t, "Nested"))

	for _, want := range []string{
		`func (v Nested) LogValue() slog.Value {`,
		`func (v Credentials) LogValue() slog.Value {`,
		`slog.Any("Credentials", v.Credentials)`,
		`slog.String("Token", "[redacted]")`,
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the log value does not hold %q:\n%s", want, held)
		}
	}

	compiles(t, held)
}

// A struct with nothing of its own but something underneath is written too.
//
// It is what the settling pass is for. Reaching sits two levels above the
// secret and has no tag anywhere in it, so a walk that only asked what a struct
// holds directly would leave it printing its own fields — and the one it holds
// is the struct whose method would have masked anything.
func TestAStructThatOnlyReachesASecret(t *testing.T) {
	held := source(t, written(t, "Reaching"))

	for _, want := range []string{
		`func (v Reaching) LogValue() slog.Value {`,
		`func (v Nested) LogValue() slog.Value {`,
		`func (v Credentials) LogValue() slog.Value {`,
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the chain is broken; %q is missing:\n%s", want, held)
		}
	}

	compiles(t, held)
}

// A secret behind an unexported field is reached, because a handler is.
//
// slog cannot read an unexported field, so it formats the value it was handed
// with %+v — which reads every field there is and everything under them. A
// struct whose only route to a secret runs through one therefore prints it, and
// needs a method for the same reason any other does: what the method does about
// the field is leave it out, along with the secret beneath it.
func TestASecretBehindAnUnexportedField(t *testing.T) {
	held := source(t, written(t, "Reaches"))

	if !strings.Contains(held, "func (v Session) LogValue() slog.Value {") {
		t.Errorf("the struct reaching the secret was not written for:\n%s", held)
	}
	if strings.Contains(held, "v.creds") {
		t.Errorf("the unexported field reached the log value:\n%s", held)
	}

	compiles(t, held)
}

// A pointer is resolved by slog like the value it points at, so a secret behind
// one can be masked.
func TestASecretBehindAPointer(t *testing.T) {
	held := source(t, written(t, "Indirect"))

	if !strings.Contains(held, `func (v Indirect) LogValue() slog.Value {`) {
		t.Errorf("a secret behind a pointer was not written for:\n%s", held)
	}
	if !strings.Contains(held, `func (v Credentials) LogValue() slog.Value {`) {
		t.Errorf("what the pointer points at was not written for:\n%s", held)
	}

	compiles(t, held)
}

// A struct that reaches itself terminates, and is written once.
//
// An ordinary thing for an author to write, and the shape a single pass over
// the closure answers whichever way it happened to start.
func TestASubjectThatReachesItself(t *testing.T) {
	held := source(t, written(t, "Cyclic"))

	if got := strings.Count(held, `func (v Cyclic) LogValue()`); got != 1 {
		t.Errorf("the log value is written %d times, want once:\n%s", got, held)
	}
	if !strings.Contains(held, `slog.String("Token", "[redacted]")`) {
		t.Errorf("the secret is not masked:\n%s", held)
	}

	compiles(t, held)
}

// A secret behind a slice, an array or a map is refused rather than half
// masked.
//
// slog resolves a log value for the value of an attribute and for what a
// pointer points at, and stops there — it does not walk into a collection and
// resolve each element. A value that masked everything else and left that alone
// would be redacted everywhere the author looked and open where they did not,
// which is worse than one that never claimed to be safe.
func TestASecretBehindSomethingThatCannotBeMasked(t *testing.T) {
	for _, name := range []string{"Collected", "Keyed", "KeyedBy", "Arrayed", "Named"} {
		t.Run(name, func(t *testing.T) {
			err := refused(t, name)

			held, is := diag.From(err)
			if !is {
				t.Fatalf("the refusal is not a diagnostic: %v", err)
			}
			if got := held.Code.String(); got != "FRG2026" {
				t.Errorf("code is %s, want FRG2026", got)
			}
			if !strings.Contains(held.Message, "Creds") {
				t.Errorf("the complaint does not name the field:\n%s", held.Message)
			}

			// And says what to do, which is to tag the field itself: that
			// replaces the whole of it and is what marking its contents secret
			// meant.
			if !strings.Contains(held.Hint, "tag this field itself") {
				t.Errorf("the complaint does not say what to do:\n  hint: %s", held.Hint)
			}
		})
	}
}

// A secret behind something that cannot carry a method is refused too.
//
// The same refusal as a slice, arrived at from the other side: there a log
// value exists and slog will not ask for it, here there is nowhere to put one.
// A struct written in place has no name to declare on. A type from another
// package cannot be declared on from here at all. And an instantiation of a
// generic is the dangerous one — a method written for Holder[Credentials]
// attaches to Holder, because the argument reads as a receiver type parameter,
// so every other instantiation would quietly start logging through it.
//
// A tag written inside an in-place struct is the same refusal by a shorter
// route, and is the only place such a tag is read at all: everything else is
// settled under a name, and that one has none.
func TestASecretBehindSomethingThatCannotCarryAMethod(t *testing.T) {
	// Each is told which of the reasons it is, because they are not one reason.
	// A message about slog not walking into collections, given about a field
	// holding no collection, sends its author looking for one.
	for name, want := range map[string]string{
		"Anonymous":    "struct written in place",
		"AnonymousTag": "struct written in place",
		"Instantiated": "instantiation of a generic",
		"Foreign":      "declared in another package",

		// The author's own method behind a collection, which slog will not ask
		// for any more than it would ask for one written here — so this one is
		// the collection reason after all.
		"Listed": "behind a slice, an array or a map",
	} {
		t.Run(name, func(t *testing.T) {
			err := refused(t, name)

			held, is := diag.From(err)
			if !is {
				t.Fatalf("the refusal is not a diagnostic: %v", err)
			}
			if got := held.Code.String(); got != "FRG2026" {
				t.Errorf("code is %s, want FRG2026", got)
			}
			if !strings.Contains(held.Message, want) {
				t.Errorf("the complaint does not say %q:\n%s", want, held.Message)
			}
		})
	}
}

// A type with nowhere to put a method that nothing prints is written around,
// not refused.
//
// The other side of the refusal above, and the side that decides whether the
// remedy the refusal recommends actually works. Tagging the field that holds an
// unreachable secret is what the layer tells an author to do, so a tag on one
// has to be an answer rather than a second complaint — and a secret behind a
// field nothing prints was never a secret at large.
//
// What each writes is a legal method that names the unreachable type nowhere:
// the masked field is replaced whole, and the unexported one is left out along
// with everything under it.
func TestNothingIsWrittenForATypeNothingPrints(t *testing.T) {
	for name, want := range map[string]string{
		"MaskedForeign":     `slog.String("Creds", "[redacted]")`,
		"MaskedGeneric":     `slog.String("Box", "[redacted]")`,
		"UnexportedForeign": `slog.String("Token", "[redacted]")`,
	} {
		t.Run(name, func(t *testing.T) {
			held := source(t, written(t, name))

			if !strings.Contains(held, want) {
				t.Errorf("the field is not replaced whole:\n%s", held)
			}

			// And the type it holds is named nowhere, which is what keeps the
			// file compiling: a method on it could not be declared here, and an
			// import of it would be one nothing uses.
			for _, unwanted := range []string{"other.", "Holder["} {
				if strings.Contains(held, unwanted) {
					t.Errorf("the unreachable type is named through %q:\n%s", unwanted, held)
				}
			}

			compiles(t, held)
		})
	}
}

// A subject with nowhere to put a method is refused, and says which reason.
//
// The one type nothing else speaks for. A field reaching one is refused where
// the field is, and a closure member nothing prints is passed over — so a
// subject, which no field points at, would otherwise fall through to the
// complaint about nothing being tagged and be told to add a tag it has.
//
// Reachable rather than defensive: a subject in a neighbouring package of the
// same module is one forge can name and cannot declare on, so nothing before
// this stops it.
func TestASubjectWithNowhereToPutAMethod(t *testing.T) {
	_, err := asking(t, "redactfixture/other", "Credentials")
	if err == nil {
		t.Fatal("a subject this package cannot declare on was written for")
	}

	held, is := diag.From(err)
	if !is {
		t.Fatalf("the refusal is not a diagnostic: %v", err)
	}
	if got := held.Code.String(); got != "FRG2026" {
		t.Errorf("code is %s, want FRG2026 — it was not refused for having nowhere to go", got)
	}
	if !strings.Contains(held.Message, "declared in another package") {
		t.Errorf("the complaint does not say why:\n%s", held.Message)
	}

	// And points at the type, which for a package of the same module is a file
	// the author can open and the most precise place to say this. The caret
	// moves to the declaration only for a type from outside the module, where
	// the type's own position is somewhere in a module cache and nothing can be
	// done at it.
	if !strings.HasSuffix(held.Pos.Filename, "other.go") {
		t.Errorf("it points at %s rather than at the type it is about", held.Pos)
	}

	// The remedy is one that exists. Moving a declaration into a package of the
	// same module is something an author can do, so it is offered here — and is
	// not offered for a dependency's type, where it cannot be taken.
	if !strings.Contains(held.Hint, "declare the stack in the package that declares the type") {
		t.Errorf("the complaint does not offer the remedy that exists:\n  hint: %s", held.Hint)
	}
}

// A method the author wrote is the one that is kept.
//
// The override every closure layer offers, and silent for the reason theirs are
// silent: somebody who wrote it meant to, and a complaint would be a complaint
// about doing the thing the design invites. Whatever it prints is what a
// handler sees, so nothing above it needs a method on its account either.
func TestAMethodTheAuthorWroteIsKept(t *testing.T) {
	// Over the subject itself: nothing is written, and it is not reported as a
	// subject with nothing to hide, because it has something to hide and the
	// author already dealt with it.
	unit, err := generating(t, "Handwritten")
	if err != nil {
		t.Fatalf("a subject whose author wrote the method was refused: %v", err)
	}
	if len(unit.Provides) != 0 {
		t.Errorf("a second log value was written beside the author's:\n%s", source(t, unit))
	}

	// And over a subject that reaches one. The outer type is written for and the
	// inner is not, which is the whole distinction: slog resolves an attribute,
	// so the author's method is reached only once something hands their value
	// over as one — and a handler formatting the outer struct with no method of
	// its own prints the inner struct's fields rather than asking it anything.
	beside := source(t, written(t, "Delegating"))

	if !strings.Contains(beside, "func (v Delegating) LogValue() slog.Value {") {
		t.Errorf("nothing hands the author's value over as an attribute:\n%s", beside)
	}
	if !strings.Contains(beside, `slog.Any("Held", v.Held)`) {
		t.Errorf("the value is not handed over for slog to resolve:\n%s", beside)
	}
	if strings.Contains(beside, "func (v Handwritten) LogValue()") {
		t.Errorf("a second log value was written beside the author's:\n%s", beside)
	}
}

// A tag written as "-" is an author opting out, not asking in.
//
// It is what the ignore form means everywhere else a tag is read, and what the
// log value a tag earns on its own already honours. Reading it the other way
// would mask a field somebody said to leave alone — and would refuse the whole
// declaration over one behind a slice that never needed masking.
func TestTheIgnoreFormIsNotARequest(t *testing.T) {
	err := refused(t, "OptedOut")

	held, is := diag.From(err)
	if !is {
		t.Fatalf("the refusal is not a diagnostic: %v", err)
	}
	if got := held.Code.String(); got != "FRG2025" {
		t.Errorf("code is %s, want FRG2025 — the opted-out field was read as a request", got)
	}
}

// Asking for redaction over a value with nothing to hide is refused.
//
// What would be written says exactly what slog says without it, and has to be
// regenerated every time a field is added. Refused rather than written, because
// a layer that quietly did nothing would leave an author believing a value is
// protected.
func TestASubjectWithNothingToHide(t *testing.T) {
	err := refused(t, "Clean")

	held, is := diag.From(err)
	if !is {
		t.Fatalf("the refusal is not a diagnostic: %v", err)
	}
	if got := held.Code.String(); got != "FRG2025" {
		t.Errorf("code is %s, want FRG2025", got)
	}
	if !strings.Contains(held.Hint, "already logs correctly without this layer") {
		t.Errorf("the complaint does not say the value is fine as it is:\n  hint: %s", held.Hint)
	}
}

// The layer names the one package its output imports.
//
// Everything else a log value names comes from the subject's own fields, which
// the spelling finds without being told.
func TestWhatTheLayerBinds(t *testing.T) {
	held := redact.New().Binds()

	if len(held) != 1 || held[0].Path != "log/slog" {
		t.Errorf("the layer binds %v, want log/slog alone", held)
	}
}

// A stack with nothing structured beneath it is refused, because a log value is
// written out of the subject's fields.
func TestWhatTheLayerAcceptsBeneathIt(t *testing.T) {
	if err := redact.New().Accepts(shape.Shape{}); err == nil {
		t.Error("the layer accepted a stack with no subject beneath it")
	}
	if err := redact.New().Accepts(shape.Shape{Caps: shape.Set(shape.Structured)}); err != nil {
		t.Errorf("the layer refused a structured stack: %v", err)
	}
}

// Nothing is added to the shape, because what this layer writes goes on the
// subject rather than on the declared type.
func TestTheLayerChangesNothingAboveIt(t *testing.T) {
	below := shape.Shape{Caps: shape.Set(shape.Structured, shape.Sized)}

	if got := redact.New().Shape(nil, below); got.Caps != below.Caps {
		t.Errorf("the layer exposes %s, want the shape beneath it unchanged", got.Caps)
	}
	if got := redact.New().OptionSchema(); got != nil {
		t.Errorf("the layer declares options: %v", got)
	}
}

// Asked to generate with no declaration, the layer says so rather than
// panicking.
//
// Not a diagnostic: a diagnostic points at a declaration, and the declaration
// is what is missing. Reaching it is forge calling itself wrongly rather than
// anybody writing anything.
func TestGeneratingWithNoDeclaration(t *testing.T) {
	for name, ctx := range map[string]*layer.Context{
		"no context":     nil,
		"no declaration": {},
		"no subject":     {Model: &model.Model{Name: "Persons"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := redact.New().Generate(ctx, shape.Shape{}); err == nil {
				t.Error("the layer generated without a declaration")
			}
		})
	}
}
