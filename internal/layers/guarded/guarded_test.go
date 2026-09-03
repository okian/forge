package guarded_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/internal/layers/guarded"
	"github.com/okian/forge/plugin"
)

// What the layer says about itself, which is what every stage that is not
// generation reads.
func TestWhatTheLayerSaysItIs(t *testing.T) {
	held := guarded.New()

	if got, want := held.Origin(), marker("Guarded"); got != want {
		t.Errorf("claims %s, want %s", got, want)
	}
	if got := held.Kind(); got != plugin.KindDecorator {
		t.Errorf("kind is %s, want %s", got, plugin.KindDecorator)
	}
	if got := held.Stage(); got != plugin.StageReady {
		t.Errorf("stage is %s, want %s", got, plugin.StageReady)
	}
	if held.Doc() == "" {
		t.Error("the layer says nothing about what it is for")
	}
}

// A lock needs something it can walk, and says so against a stack that offers
// no walk.
//
// The one capability it cannot do without. What it takes away is replaced by
// scoped access and by a copy, and a copy is a walk collected — so a stack that
// cannot be walked is one the lock would take iteration from and offer nothing
// back for.
func TestALockOverSomethingItCannotWalk(t *testing.T) {
	below := walking("Person")
	below.Caps = below.Caps.Without(plugin.Streamable)

	err := guarded.New().Accepts(below)
	if err == nil {
		t.Fatal("a stack with nothing to walk was accepted")
	}
	if !strings.Contains(err.Error(), plugin.Streamable.String()) {
		t.Errorf("the refusal does not name what is missing: %v", err)
	}

	if err := guarded.New().Accepts(walking("Person")); err != nil {
		t.Errorf("a stack that can be walked was refused: %v", err)
	}
}

// What the lock exposes upward is what was beneath it, minus the reach it took
// away, plus what it put in its place.
func TestWhatALockExposes(t *testing.T) {
	above := guarded.New().Shape(asked("Persons"), walking("Person"))

	if !above.Caps.Has(plugin.Concurrent) {
		t.Errorf("a lock exposes %s, and nothing about it says it is safe to share", above.Caps)
	}
	for _, gone := range []plugin.Cap{plugin.Streamable, plugin.Indexed} {
		if above.Caps.Has(gone) {
			t.Errorf("a lock still exposes %s, which is the reach it exists to take away", gone)
		}
	}

	// The methods, which is the half a capability cannot express: Streamable
	// going missing tells a layer above what it may not be written against, and
	// tells nothing at all to a reader looking at a list that still holds All.
	for _, gone := range []string{"All", "Backward", "AppendSeq"} {
		if _, has := above.Method(gone); has {
			t.Errorf("%s is still on the surface of a locked stack", gone)
		}
	}
	for _, want := range []string{"Do", "RDo", "Snapshot", "Len"} {
		if _, has := above.Method(want); !has {
			t.Errorf("a locked stack offers no %s, only %v", want, above.Names())
		}
	}
}

// A scope is handed a view named after the type it is a scope over.
func TestWhatAScopeHandsOver(t *testing.T) {
	above := guarded.New().Shape(asked("Persons"), walking("Person"))

	scope, has := above.Method("Do")
	if !has {
		t.Fatalf("a locked stack offers no scope, only %v", above.Names())
	}
	if !strings.Contains(scope.Signature, "PersonsView") {
		t.Errorf("a scope hands over %s, and the view over Persons is PersonsView", scope.Signature)
	}
}

// A stack that cannot be counted gets no count, rather than one that does not
// compile.
//
// The count is the one method reached without a scope, and it is reached that
// way because it is one number read and handed back. What it forwards to is the
// stack's own, so a stack with none has nothing to forward to.
func TestALockOverSomethingThatCannotBeCounted(t *testing.T) {
	below := walking("Person")
	below.Caps = below.Caps.Without(plugin.Sized)

	above := guarded.New().Shape(asked("Persons"), below)
	if _, has := above.Method("Len"); has {
		t.Error("a stack that cannot be counted was given a count")
	}
}

// The lock itself is written only where the declaration asked for it.
//
// It is what the rest of the type exists to make unnecessary, so it is not
// there by default: a caller holding the lock directly reaches nothing it
// guards, which makes exporting it an invitation to the misuse the layer is
// for.
func TestTheLockItself(t *testing.T) {
	for _, one := range []struct {
		name    string
		options []string
		want    bool
	}{
		{name: "not asked for", want: false},
		{name: "asked for", options: []string{"expose=locker"}, want: true},
	} {
		t.Run(one.name, func(t *testing.T) {
			above := guarded.New().Shape(asked("Persons", one.options...), walking("Person"))

			_, has := above.Method("Lock")
			if has != one.want {
				t.Errorf("Lock is on the surface: %v, want %v — %v", has, one.want, above.Names())
			}

			if _, unlocks := above.Method("Unlock"); unlocks != has {
				t.Error("the two halves of a sync.Locker did not arrive together")
			}
		})
	}
}

// A codec for the container is written where the elements have one, and not
// where they do not.
//
// It is written here rather than wrapped from beneath because a container's
// codec is written over a walk, and the walk is what this layer took away — so
// the layer that writes codecs was handed a stack with nothing to walk and
// wrote none.
func TestTheContainersCodec(t *testing.T) {
	with := walking("Person")
	with.Caps = with.Caps.With(plugin.Encodable)

	above := guarded.New().Shape(asked("Persons"), with)
	if _, has := above.Method("MarshalJSON"); !has {
		t.Errorf("elements that can be encoded got no codec for the container, only %v", above.Names())
	}

	without := guarded.New().Shape(asked("Persons"), walking("Person"))
	if _, has := without.Method("MarshalJSON"); has {
		t.Error("elements that cannot be encoded were given a codec for the container")
	}
}

// What is beneath a lock moves to a type of its own, named for what the lock
// holds.
//
// A lock that left it where it was would be a lock anybody could walk around: a
// method on the declared type is reachable by whoever holds one, and the whole
// arrangement is that the unlocked methods are not.
func TestWhatALockEncloses(t *testing.T) {
	held := guarded.New()

	if got, want := held.Encloses("Persons"), "personsHeld"; got != want {
		t.Errorf("a lock over Persons holds %s, want %s", got, want)
	}
	if got := held.Encloses(""); got != "" {
		t.Errorf("a lock over nothing holds %q, want nothing", got)
	}

	// Composing, which is what makes the name a function of what is above
	// rather than of the declaration: a lock inside a lock encloses what the
	// outer one already enclosed.
	if got, want := held.Encloses(held.Encloses("Persons")), "personsHeldHeld"; got != want {
		t.Errorf("a lock inside a lock holds %s, want %s", got, want)
	}
}

// The options are the two the catalog documents, and nothing else.
func TestTheOptionsALockTakes(t *testing.T) {
	var named []string
	for _, one := range guarded.New().OptionSchema() {
		named = append(named, one.Key)

		if one.Doc == "" {
			t.Errorf("%s is an option nothing says the meaning of", one.Key)
		}
	}

	if want := []string{"encode", "expose"}; !slices.Equal(named, want) {
		t.Errorf("the layer takes %v, want %v", named, want)
	}
}

// A lock says how one of itself is made, so that a decorator holding one has an
// answer where it looks.
//
// The same forwarding one level up: a lock over a container that has to be made
// writes a way in, and what it says here is that way in described. A lock over
// a container that needs no making needs none itself, and says so.
func TestHowALockSaysItIsMade(t *testing.T) {
	cases := map[string]struct {
		declared string
		holds    *plugin.Constructor
		want     plugin.Constructor
		needs    bool
	}{
		"over a container that needs no making": {declared: "Persons"},
		"over one the caller sizes": {
			declared: "Persons",
			holds:    &plugin.Constructor{Name: "newPersonsHeld", Params: []string{"size int"}, Args: []string{"size"}, Pointer: true},
			want:     plugin.Constructor{Name: "NewPersons", Params: []string{"size int"}, Args: []string{"size"}, Pointer: true},
			needs:    true,
		},
		// The lock is itself enclosed, so what it declares onto is unexported
		// and its own way in takes that visibility with it.
		"enclosed by something else": {
			declared: "personsHeld",
			holds:    &plugin.Constructor{Name: "newPersonsHeldHeld", Pointer: true},
			want:     plugin.Constructor{Name: "newPersonsHeld", Pointer: true},
			needs:    true,
		},
	}

	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := asked("Persons").Declaring(held.declared).Holding(held.holds)

			made, needs := guarded.New().Constructor(ctx)
			if needs != held.needs {
				t.Fatalf("it says it needs making: %v, want %v", needs, held.needs)
			}
			if !needs {
				return
			}

			if made.Name != held.want.Name || made.Pointer != held.want.Pointer {
				t.Errorf("made by %s (pointer %v), want %s (pointer %v)",
					made.Name, made.Pointer, held.want.Name, held.want.Pointer)
			}
			if !slices.Equal(made.Params, held.want.Params) || !slices.Equal(made.Args, held.want.Args) {
				t.Errorf("takes %v passing %v, want %v passing %v",
					made.Params, made.Args, held.want.Params, held.want.Args)
			}
		})
	}
}

// A constructor answering with a value rather than a pointer is stored as it
// comes back.
//
// The lock holds a container, not a pointer to one, so a constructor answering
// with a pointer is dereferenced and one answering with a value is not. No
// layer in this build answers with a value — a container that has to be made is
// one big enough to be worth a pointer — so the branch is exercised here rather
// than by any stack.
func TestALockOverAContainerMadeByValue(t *testing.T) {
	ctx := asked("Persons").Holding(&plugin.Constructor{
		Name: "newPersonsHeld", Params: []string{"elems ...Person"}, Args: []string{"elems..."},
	})

	unit, err := guarded.New().Generate(ctx, walking("Person"))
	if err != nil {
		t.Fatalf("the layer refused to generate: %v", err)
	}

	held := printed(t, unit)
	for _, want := range []string{
		"func NewPersons(elems ...Person) *Persons {",
		"return &Persons{held: newPersonsHeld(elems...)}",
	} {
		if !strings.Contains(held, want) {
			t.Errorf("the output does not hold %q:\n%s", want, held)
		}
	}
}
