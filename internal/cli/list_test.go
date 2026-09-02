package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// listing drives the list verb and returns what it wrote.
func listing(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	out, errs := &bytes.Buffer{}, &bytes.Buffer{}
	env := &environment{stdout: out, stderr: errs, pipeline: over(&stack{})}

	cmd, ok := lookup("list")
	if !ok {
		t.Fatal("list is not a command")
	}

	err := cmd.run(env, cmd, args)
	return out.String(), errs.String(), err
}

// The listing names every layer this build knows, with what each one is for.
//
// The answer to "what else could I have written", which is the question
// somebody has the moment a stack is refused — so it is worth being complete
// about the layers that are not finished as well as the ones that are.
func TestWhatTheListingHolds(t *testing.T) {
	shown, said, err := listing(t)
	if err != nil {
		t.Fatalf("listing: %v\n%s", err, said)
	}

	for _, want := range []string{
		// One of each stage, so that a listing which quietly dropped the
		// unfinished ones would be a listing of what forge has got round to.
		"Slice", "Ring", "Collection", // written
		"Json", "Guarded", // stubs
		"LRU", "Csv", // staged

		// And the columns, which are what make the rows worth reading.
		"Kind", "Stage", "Declare", "Requires", "Adds", "Masks", "Effect",
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("the listing does not hold %q:\n%s", want, shown)
		}
	}

	// The listing is the answer, so it goes to stdout and nothing goes with it.
	if said != "" {
		t.Errorf("listing wrote to stderr:\n%s", said)
	}
}

// The options every layer accepts are listed, because an option nobody can
// discover is one nobody writes.
func TestTheListingHoldsTheOptions(t *testing.T) {
	shown, _, err := listing(t)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	for _, want := range []string{
		"Options",
		"overflow=overwrite|error",
		"sort=<fields>",

		// What an option is worth when nobody writes it, which is half of what
		// deciding whether to write it needs.
		"overwrite",
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("the listing does not hold %q:\n%s", want, shown)
		}
	}
}

// The document form is what a program reads, and is a document rather than the
// table with the spaces taken out.
func TestTheListingAsADocument(t *testing.T) {
	shown, _, err := listing(t, "--json")
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	var got listedCatalog
	if err := json.Unmarshal([]byte(shown), &got); err != nil {
		t.Fatalf("reading it back: %v\n%s", err, shown)
	}

	if len(got.Layers) == 0 {
		t.Fatal("the document holds no layers")
	}

	found := false
	for _, one := range got.Layers {
		if one.Name != "Ring" {
			continue
		}
		found = true

		if one.Kind != "storage" || one.Stage != "ready" {
			t.Errorf("a ring is a %s at stage %s", one.Kind, one.Stage)
		}
		if len(one.Options) != 2 {
			t.Errorf("a ring accepts %d options", len(one.Options))
		}
	}
	if !found {
		t.Error("the document does not hold Ring")
	}
}

// What a program reads back out of the listing, named rather than written into
// the test that reads it: the fields are an interface, and a test that spelled
// them inline would spell them again the next time somebody read one.
type listedCatalog struct {
	Layers []listedLayer `json:"layers"`
}

type listedLayer struct {
	Name    string         `json:"name"`
	Kind    string         `json:"kind"`
	Stage   string         `json:"stage"`
	Options []listedOption `json:"options"`
}

type listedOption struct {
	Key string `json:"key"`
}

// The verb takes no arguments, and says so rather than ignoring one.
//
// A pattern written after it is somebody expecting the listing to be about
// their code, and quietly listing the whole catalog would answer a question
// they did not ask.
func TestListingWithAnArgument(t *testing.T) {
	_, said, err := listing(t, "./...")

	if err == nil {
		t.Fatal("an argument was accepted")
	}
	if !strings.Contains(err.Error(), "takes no arguments") {
		t.Errorf("it was refused as %q", err)
	}
	if strings.Contains(said, "Slice") {
		t.Errorf("a refused run listed the catalog anyway:\n%s", said)
	}
}
