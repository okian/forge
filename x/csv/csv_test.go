package csv_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/okian/forge/plugin"
	"github.com/okian/forge/x/csv"
)

// The layer claims forge's own Csv marker.
//
// Its own package would do as well and would be safer — nothing forge ships can
// then collide with it — and this one claims forge's on purpose, because that is
// the arrangement worth proving: a marker published without a generator behind
// it is a declaration an author can write today and have generate the moment
// the layer is linked.
func TestWhatTheLayerClaims(t *testing.T) {
	held := csv.New()

	origin := held.Origin()
	if origin.Pkg != plugin.MarkerPkg {
		t.Errorf("the marker is declared in %q, want forge's own %q", origin.Pkg, plugin.MarkerPkg)
	}
	if origin.Name != "Csv" {
		t.Errorf("the marker is %q, want Csv", origin.Name)
	}
	if origin.Args != "" {
		t.Errorf("the marker carries the arguments %q; a layer claims the generic", origin.Args)
	}

	if got := held.Kind(); got != plugin.KindTransport {
		t.Errorf("the layer is a %v, want a transport", got)
	}

	// A layer outside forge has one answer about how far along it is, and
	// answering rather than staying silent is what gives the list command
	// something to print.
	if got := held.Stage(); got != plugin.StageReady {
		t.Errorf("the layer reports the stage %v, want ready", got)
	}
	if held.Doc() == "" {
		t.Error("the layer says nothing about what it is for")
	}
}

// A transport carries what is beneath it and puts nothing on the subject.
func TestTheLayerWritesNothingOnTheSubject(t *testing.T) {
	if got := csv.New().Writes(); got != nil {
		t.Errorf("the layer says it writes %q on the subject, want nothing", got)
	}
}

// Every package the generated code names is reserved, and the reserved set is
// returned by copy.
//
// The copy matters: a caller that sorted what it was handed would be sorting
// what every other caller of the same layer value is still holding, and the
// fault would be a package spelled two ways in one file.
func TestWhatTheOutputBinds(t *testing.T) {
	held := csv.New()

	bound := held.Binds()
	for _, want := range []string{"encoding/csv", "errors", "fmt", "io", "slices", "strconv"} {
		if !slices.ContainsFunc(bound, func(one plugin.Import) bool { return one.Path == want }) {
			t.Errorf("%s is not reserved, and the output names it", want)
		}
	}

	// Every entry binds a name, since a file naming a package under no name
	// does not compile.
	for _, one := range bound {
		if one.Name == "" {
			t.Errorf("%s is reserved under no name", one.Path)
		}
	}

	bound[0].Path = "rewritten"
	if again := held.Binds(); again[0].Path == "rewritten" {
		t.Error("the reserved set is the layer's own, and a caller can rewrite it")
	}
}

// The options are the two a document has: whether it is headed, and what
// separates two fields.
func TestTheOptionSchema(t *testing.T) {
	schema := csv.New().OptionSchema()

	want := map[string]plugin.ValueKind{
		"header": plugin.ValueBool,
		"comma":  plugin.ValueString,
	}

	if len(schema) != len(want) {
		t.Fatalf("the layer accepts %d options, want %d", len(schema), len(want))
	}

	// Every property of every option, rather than the first that fails. They
	// are independent — an option with the wrong kind may also have no
	// summary — so reporting one and stopping would send somebody back for the
	// next after each fix.
	for _, one := range schema {
		kind, declared := want[one.Key]
		if !declared {
			t.Errorf("the layer accepts %q, which is not written down here", one.Key)
			continue
		}

		if one.Value != kind {
			t.Errorf("%s takes a %v, want a %v", one.Key, one.Value, kind)
		}
		if one.Doc == "" {
			t.Errorf("%s says nothing about what it is for", one.Key)
		}
		if one.Default == "" {
			t.Errorf("%s has no default, and every option here has one", one.Key)
		}
		if one.Scope != plugin.ScopeDeclaration {
			t.Errorf("%s is written about a %v, want about the declaration", one.Key, one.Scope)
		}
	}
}

// A stack with nothing to tabulate or nothing to walk is refused, and the
// refusal says which.
//
// Both halves matter and they fail for different reasons. A stack with no
// subject has no columns to make; one with no walk has rows it cannot reach.
// Reporting the first missing thing rather than both is deliberate — the second
// is usually a consequence of the first — so each case here offers everything
// except the one capability under test.
func TestWhatTheLayerRefusesToSitOn(t *testing.T) {
	held := csv.New()

	cases := map[string]struct {
		caps plugin.CapSet
		says string
	}{
		"nothing at all":      {caps: 0, says: "subject"},
		"a container of what": {caps: plugin.Every().Without(plugin.Structured), says: "subject"},
		"a subject nobody can walk": {
			caps: plugin.Every().Without(plugin.Streamable),
			says: "walked",
		},
	}

	for name, one := range cases {
		t.Run(name, func(t *testing.T) {
			err := held.Accepts(plugin.Shape{Caps: one.caps})
			if err == nil {
				t.Fatal("the layer accepted a stack it cannot write")
			}
			if !strings.Contains(err.Error(), one.says) {
				t.Errorf("the refusal %q does not say what is missing", err)
			}
		})
	}

	// And a container of a subject with fields is accepted.
	if err := held.Accepts(plugin.Shape{Caps: plugin.Caps(plugin.Structured, plugin.Streamable)}); err != nil {
		t.Errorf("the layer refused a walkable container of a subject: %v", err)
	}
}

// A transport adds a wire form and changes nothing else.
//
// It takes nothing away, and there is nothing above it to take anything away
// from: a transport terminates a stack. What it does not do is put its own
// methods on the surface, because which of them it writes is decided by what
// the stack beneath turns out to expose and a surface is asked for before that
// is known.
func TestWhatTheLayerExposes(t *testing.T) {
	below := plugin.Shape{
		Caps:    plugin.Caps(plugin.Structured, plugin.Streamable, plugin.Sized),
		Surface: []plugin.Method{{Name: "Len", Signature: "() int"}},
	}

	above := csv.New().Shape(nil, below)

	if !above.Caps.Has(plugin.Encodable) {
		t.Error("the stack is not encodable after a transport was written over it")
	}
	if !above.Caps.Has(plugin.Structured, plugin.Streamable, plugin.Sized) {
		t.Errorf("the transport took something away: %v became %v", below.Caps, above.Caps)
	}
	if got := above.Names(); !slices.Equal(got, []string{"Len"}) {
		t.Errorf("the surface became %q, want the one beneath unchanged", got)
	}
}

// Asked to generate without a declaration, the layer says so as an ordinary
// error rather than as a diagnostic.
//
// A diagnostic points at a declaration, and the declaration is what is missing.
// Reaching here is forge calling the layer wrongly rather than anybody writing
// anything, so the report is for whoever is holding the debugger.
func TestGeneratingWithoutADeclaration(t *testing.T) {
	held := csv.New()

	for name, ctx := range map[string]*plugin.Context{
		"no context":     nil,
		"no declaration": {},
		"no subject":     {Model: &plugin.Model{Name: "Rows"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := held.Generate(ctx, plugin.Shape{})
			if err == nil {
				t.Fatal("the layer generated for nothing")
			}
			if _, is := plugin.From(err); is {
				t.Errorf("the failure is a diagnostic about a declaration that is missing: %v", err)
			}
		})
	}
}
