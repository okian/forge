package people_test

import (
	"bytes"
	"encoding/json/v2"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/okian/forge/examples/people"
)

// redacted is what the layer writes in place of a tagged field, and the one
// string in a log that says the generated method ran.
const redacted = "[redacted]"

// credential is one with something worth hiding in it.
func credential() people.Credential {
	return people.Credential{
		Owner:  7,
		State:  people.StatusActive,
		Secret: &people.Secret{Issued: "2026-01-01", Token: "hunter2"},
	}
}

// The token is a token everywhere except in a log.
//
// The pairing the declaration exists to show. A codec and a log value disagree
// about a secret on purpose: the token has to go over the wire or the
// credential is useless, and it must not go into a log or the credential is
// compromised. Nothing about the subject says which of the two a caller is
// doing, so each layer answers for its own channel.
func TestWhatACredentialWritesAndWhatItLogs(t *testing.T) {
	// Over the wire, in full, and compared whole rather than searched: a
	// credential nobody can authenticate with is not a credential, and what
	// else the document holds is worth pinning while the token is being looked
	// for.
	written, err := json.Marshal(credential())
	if err != nil {
		t.Fatalf("encoding a credential: %v", err)
	}

	const want = `{"Owner":7,"State":"active","Secret":{"Issued":"2026-01-01","Token":"hunter2"}}`
	if got := string(written); got != want {
		t.Errorf("a credential encodes as\n  %s\nwant\n  %s", got, want)
	}

	// Into a log, never.
	logged := logging(t, credential())

	if strings.Contains(logged, "hunter2") {
		t.Errorf("the token reached a log: %s", logged)
	}

	// Every other field, spelled as the handler spells it, so that a field
	// dropped from the log value fails here rather than passing for redaction.
	for _, want := range []string{
		"held.Owner=7",
		"held.State=active",
		"held.Secret.Issued=2026-01-01",
		"held.Secret.Token=" + redacted,
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log does not carry %q: %s", want, logged)
		}
	}
}

// A secret one struct down is still kept out, which is what the layer walks the
// subject's reach for.
//
// slog resolves one value at a time. A Credential that handed its Secret over
// unchanged would have the handler reach into the struct and print the token,
// so the method on the outer type is not enough on its own — the inner type
// needs one too, and gets it.
func TestASecretOneStructDownIsStillKeptOut(t *testing.T) {
	// The inner value on its own, which is how it reaches a log when somebody
	// logs the part rather than the whole.
	logged := logging(t, people.Secret{Issued: "2026-01-01", Token: "hunter2"})

	if strings.Contains(logged, "hunter2") {
		t.Errorf("the token reached a log through the inner value: %s", logged)
	}
	if !strings.Contains(logged, "held.Token="+redacted) {
		t.Errorf("the inner value has no log value of its own: %s", logged)
	}
}

// A credential with no secret logs the rest of itself rather than a stack
// trace.
//
// The guard the generated log value opens with. slog recovers a panic raised
// inside a LogValue and writes the panic where the field should have been, so a
// method that dereferenced a nil secret would not crash the program — it would
// quietly replace one log line with something nobody can read, in the value a
// caller reached for precisely because it was careful.
func TestACredentialWithNoSecret(t *testing.T) {
	logged := logging(t, people.Credential{Owner: 7, State: people.StatusRevoked})

	// slog spells a recovered panic with this word, and it is the whole reason
	// the guard is written.
	if strings.Contains(strings.ToLower(logged), "panic") {
		t.Errorf("a nil secret panicked inside the log value: %s", logged)
	}
	if !strings.Contains(logged, "held.Owner=7") || !strings.Contains(logged, "held.State=revoked") {
		t.Errorf("the fields beside the missing secret did not survive it: %s", logged)
	}
}

// Logging a slice does not reach the log value of what is in it.
//
// The boundary of what this layer can do, witnessed rather than described. slog
// resolves the value it is handed and the fields of a group, and stops: handed
// a slice it formats the slice, and the method each element carries is never
// called. It is the same limit the redaction layer refuses a secret behind a
// slice field for. The difference is that a declared container cannot be
// refused — being a slice of the subject is what [people.Credentials] is for.
//
// What that costs depends on how the element holds its secret, which is why
// both are here. A Credential keeps its Secret behind a pointer, and formatting
// prints the address, so nothing legible comes out. A Secret is a value, and
// formatting prints its fields, so the token comes out entire. Neither owes
// anything to the layer: the first is luck and the second is what the luck is
// worth.
func TestLoggingASliceDoesNotReachTheElement(t *testing.T) {
	// Not called, which is the fact under both halves: the mask this layer
	// writes is the one string that cannot appear unless the method ran.
	directory := logging(t, people.NewCredentials(credential()))

	if strings.Contains(directory, redacted) {
		t.Errorf("the element's log value was consulted after all, so this is no longer a boundary:\n%s", directory)
	}

	// And something was logged, so the line above is the method going
	// unconsulted rather than the whole attribute going missing. The fields are
	// spelled the way formatting spells them and not the way a log value would.
	if !strings.Contains(directory, "held=\"[{Owner:7") {
		t.Errorf("the directory logged something other than its formatted elements:\n%s", directory)
	}

	// Nothing legible comes out of it all the same, which is the pointer's
	// doing and not the layer's: formatting prints an address where a value
	// would have printed its fields. Held here so that a Secret moved back into
	// the struct fails rather than quietly starts logging tokens.
	if strings.Contains(directory, "hunter2") {
		t.Errorf("the directory printed the token, so the luck it runs on has run out:\n%s", directory)
	}

	// And where the secret is held by value, that is a token in a log.
	leaked := logging(t, []people.Secret{{Issued: "2026-01-01", Token: "hunter2"}})

	if !strings.Contains(leaked, "hunter2") {
		t.Errorf("a slice of secrets no longer prints them, so the warning about it is stale:\n%s", leaked)
	}

	// The same secret logged as itself, which is what makes the line above a
	// limit of slog rather than a failure of the layer.
	one := logging(t, people.Secret{Issued: "2026-01-01", Token: "hunter2"})

	if strings.Contains(one, "hunter2") {
		t.Errorf("the element leaked when logged as itself: %s", one)
	}
}

// A status says its own name, takes one back, and knows how many of it there
// are.
//
// The list is compared whole. A ValuesStatus that returned one member would
// make a loop over it pass by having nothing to disagree with, which is the
// shape of bug a generated list is most likely to have — and the order is part
// of what it promises, since the constants are counted by iota and the names
// are all a reader sees.
func TestWhatAStatusCanDo(t *testing.T) {
	want := []people.Status{people.StatusPending, people.StatusActive, people.StatusRevoked}

	if got := people.ValuesStatus(); !slices.Equal(got, want) {
		t.Fatalf("the members are %v, want %v", got, want)
	}

	for _, one := range want {
		back, err := people.ParseStatus(one.String())
		if err != nil || back != one {
			t.Errorf("%s did not survive being written and read: %v, %v", one, back, err)
		}
		if !one.Valid() {
			t.Errorf("%s is a declared member and says it is not", one)
		}
	}

	// Each renders as its own name, so a Valid that answered for the wrong
	// member, or a String whose cases fell together, fails here.
	for one, want := range map[people.Status]string{
		people.StatusPending: "pending",
		people.StatusActive:  "active",
		people.StatusRevoked: "revoked",
	} {
		if got := one.String(); got != want {
			t.Errorf("a member renders as %q, want %q", got, want)
		}
	}
}

// Both halves of the text codec answer, and both refuse a value nobody
// declared.
//
// Called directly, because nothing else calls MarshalText. A caller of either
// library gets AppendText — json/v2 and slog both prefer the appender, since it
// writes into a buffer they already hold rather than allocating a slice to be
// copied out of — so the method beside it is reached only by a caller who names
// it, and is generated public API all the same.
func TestBothHalvesOfTheTextCodec(t *testing.T) {
	written, err := people.StatusActive.MarshalText()
	if err != nil {
		t.Fatalf("writing a member: %v", err)
	}
	if got := string(written); got != "active" {
		t.Errorf("a member writes as %q, want %q", got, "active")
	}

	// Onto the end of what is already there, which is the whole difference
	// between the two.
	appended, err := people.StatusRevoked.AppendText([]byte("state="))
	if err != nil {
		t.Fatalf("appending a member: %v", err)
	}
	if got := string(appended); got != "state=revoked" {
		t.Errorf("appending gives %q, want %q", got, "state=revoked")
	}

	// Neither writes a value the set has no name for, since a document holding
	// one is a document nothing can read back.
	loose := people.Status(42)
	if _, err := loose.MarshalText(); err == nil {
		t.Error("a value nobody declared was written by MarshalText")
	}
	if _, err := loose.AppendText(nil); err == nil {
		t.Error("a value nobody declared was written by AppendText")
	}

	// And the reader takes back what each of them wrote, which is what says the
	// two write the same thing.
	for _, one := range [][]byte{written, appended[len("state="):]} {
		var back people.Status
		if err := back.UnmarshalText(one); err != nil {
			t.Errorf("reading back %q: %v", one, err)
		} else if back.String() != string(one) {
			t.Errorf("%q read back as %s", one, back)
		}
	}
}

// A value nobody declared is not a member, and does not go over the standard
// library's wire.
//
// The zero of this set is an ordinary member, so there is nothing to compare
// against and Valid is how a caller asks. What a document must not carry is a
// number the receiver has no name for: it would decode into whichever member
// happens to share it, or into none.
func TestAStatusNobodyDeclared(t *testing.T) {
	loose := people.Status(42)

	if loose.Valid() {
		t.Error("a value nobody declared says it is a member")
	}
	if got := loose.String(); !strings.Contains(got, "42") {
		t.Errorf("it renders as %q, which does not say what it holds", got)
	}

	if _, err := json.Marshal(struct{ S people.Status }{loose}); err == nil {
		t.Error("a value nobody declared went onto a wire")
	}
}

// A member goes over the standard library's wire under its name, and comes
// back.
//
// What the text codec buys. A document holding 1 says nothing to a reader and
// breaks the day somebody inserts a member above it; one holding "active" says
// what it means and survives the insertion. Both halves are exercised, since a
// marshaler with no reader is a document that can be written and not read.
func TestAStatusTravelsAsItsName(t *testing.T) {
	written, err := json.Marshal(struct{ S people.Status }{people.StatusActive})
	if err != nil {
		t.Fatalf("encoding a status: %v", err)
	}
	if got, want := string(written), `{"S":"active"}`; got != want {
		t.Errorf("a status encodes as %s, want %s", got, want)
	}

	var back struct{ S people.Status }
	if err := json.Unmarshal(written, &back); err != nil {
		t.Fatalf("reading a status back: %v", err)
	}
	if back.S != people.StatusActive {
		t.Errorf("it came back as %s, want active", back.S)
	}
}

// A misspelt name does not decode into the zero member.
//
// The failure a closed set exists to prevent. The zero here is pending, which
// is an ordinary status, so a document saying "activ" would otherwise arrive as
// a credential nobody had said anything about — and nothing would report it.
// The name that is spelt right is decoded alongside it, so that a decoder which
// refused everything would not pass for one that refuses this.
func TestAMisspeltStatus(t *testing.T) {
	var good struct{ S people.Status }
	if err := json.Unmarshal([]byte(`{"S":"active"}`), &good); err != nil {
		t.Fatalf("a name spelt right did not decode: %v", err)
	}
	if good.S != people.StatusActive {
		t.Fatalf("it decoded as %s, want active", good.S)
	}

	var bad struct{ S people.Status }
	err := json.Unmarshal([]byte(`{"S":"activ"}`), &bad)
	if err == nil {
		t.Fatalf("a misspelt name decoded, as %s", bad.S)
	}
	if !strings.Contains(err.Error(), "activ") {
		t.Errorf("the complaint does not name what it refused: %v", err)
	}
}

// A status inside a generated codec travels as its name, the same as it does
// anywhere else.
//
// The composition worth a test of its own, because the two halves of it are
// decided by two declarations. [people.Statuses] gives [people.Status] a text
// codec; [people.Credentials] gives [people.Credential] a codec of forge's own,
// which writes the field through it. Neither declaration mentions the other,
// and the run is what puts the two together.
//
// What the field must not travel as is the number behind it. A document holding
// 2 says nothing to a reader, cannot be checked by one, and changes meaning the
// day somebody inserts a member above revoked — and a receiver reading it has
// no way to notice any of that.
func TestAStatusInsideAGeneratedCodecTravelsAsItsName(t *testing.T) {
	written, err := json.Marshal(people.Credential{Owner: 1, State: people.StatusRevoked})
	if err != nil {
		t.Fatalf("encoding a credential: %v", err)
	}
	if got, want := string(written), `{"Owner":1,"State":"revoked","Secret":null}`; got != want {
		t.Errorf("a credential encodes as\n  %s\nwant\n  %s", got, want)
	}

	var back people.Credential
	if err := json.Unmarshal(written, &back); err != nil {
		t.Fatalf("reading a credential back: %v", err)
	}
	if back.State != people.StatusRevoked {
		t.Errorf("the state came back as %s, want revoked", back.State)
	}
}

// A number no member stands for does not get into a credential.
//
// The other half of what writing the name buys, and the half that matters when
// the document came from somewhere else. The text codec refuses a value the set
// has no name for, so a document carrying one is refused where it is read
// rather than accepted and carried around — and a status that got in would go
// on to spoil every log line holding it, since a log value asks the same closed
// set to write the same member it has no name for.
func TestANumberNoMemberStandsForIsRefused(t *testing.T) {
	var back people.Credential

	// The old format, which is also what a hand-rolled writer would produce.
	if err := json.Unmarshal([]byte(`{"Owner":1,"State":2}`), &back); err == nil {
		t.Errorf("a number was read into a closed set, as %s", back.State)
	} else if !strings.Contains(err.Error(), "Status") {
		t.Errorf("the complaint does not name the type that refused it: %v", err)
	}

	// And a name the set does not hold, which is the same refusal reached the
	// other way.
	if err := json.Unmarshal([]byte(`{"Owner":1,"State":"lapsed"}`), &back); err == nil {
		t.Errorf("an undeclared name was read into a closed set, as %s", back.State)
	}

	// Null is the zero, as it is for every other field: a document saying a
	// member is absent is not one the reader should be made to parse.
	back.State = people.StatusRevoked
	if err := json.Unmarshal([]byte(`{"Owner":1,"State":null}`), &back); err != nil {
		t.Fatalf("null did not read as the zero: %v", err)
	}
	if back.State != people.StatusPending {
		t.Errorf("null read as %s, want the zero member", back.State)
	}
}

// The directory walks, projects, sorts and looks up its elements.
//
// One test over the whole generated surface rather than one per method: what
// each of them does is the same fact read a different way, and a directory
// whose projection disagreed with its own elements is the failure worth
// catching.
func TestWhatADirectoryOfCredentialsCanDo(t *testing.T) {
	dir := people.NewCredentials(
		people.Credential{Owner: 3, State: people.StatusRevoked},
		people.Credential{Owner: 1, State: people.StatusActive},
		people.Credential{Owner: 2, State: people.StatusPending},
	)

	if got := dir.Len(); got != 3 {
		t.Errorf("the directory holds %d, want 3", got)
	}
	if got, want := dir.Owners(), []int{3, 1, 2}; !slices.Equal(got, want) {
		t.Errorf("the owners are %v, want %v", got, want)
	}
	if got := dir.States(); !slices.Equal(got, []people.Status{
		people.StatusRevoked, people.StatusActive, people.StatusPending,
	}) {
		t.Errorf("the states are %v, want revoked, active, pending", got)
	}

	// Every credential here was built without one, so the column is three nils
	// rather than empty: a projection answers for every element.
	if got := dir.Secrets(); len(got) != 3 {
		t.Errorf("the secrets column holds %d, want one entry per element", len(got))
	}

	// The sorted view is a copy in key order, and the directory keeps the order
	// it was built in.
	if got, want := owners(dir.SortedByOwner()), []int{1, 2, 3}; !slices.Equal(got, want) {
		t.Errorf("sorted by owner gives %v, want %v", got, want)
	}
	if got, want := dir.Owners(), []int{3, 1, 2}; !slices.Equal(got, want) {
		t.Errorf("sorting reordered the directory itself, to %v", got)
	}

	found, ok := dir.ByOwner()[2]
	if !ok || found.State != people.StatusPending {
		t.Errorf("the lookup for owner 2 gave %v, %v", found, ok)
	}

	// Walking forwards and backwards, which is the storage beneath the
	// projections rather than the projections themselves.
	if got, want := owners(slices.Collect(dir.All())), []int{3, 1, 2}; !slices.Equal(got, want) {
		t.Errorf("walking gives %v, want %v", got, want)
	}
	if got, want := owners(slices.Collect(dir.Backward())), []int{2, 1, 3}; !slices.Equal(got, want) {
		t.Errorf("walking backward gives %v, want %v", got, want)
	}
}

// A lookup answers with one credential per owner, and an owner holding several
// keeps the last.
//
// Not a defect to work around but the meaning of an index, and the reason the
// declaration asks for a sorted view as well: a caller wanting every credential
// an owner holds walks the order rather than reading the map.
func TestADirectoryWhereOneOwnerHoldsTwo(t *testing.T) {
	dir := people.NewCredentials(
		people.Credential{Owner: 1, State: people.StatusRevoked},
		people.Credential{Owner: 1, State: people.StatusActive},
	)

	if got := len(dir.ByOwner()); got != 1 {
		t.Errorf("the lookup holds %d entries for one owner, want 1", got)
	}
	if got := dir.ByOwner()[1].State; got != people.StatusActive {
		t.Errorf("the lookup kept %s, want the last of the two", got)
	}
}

// The directory encodes and decodes as one document, in one pass.
//
// The composition Json under Collection exists to make: the codec is over the
// container rather than over a slice somebody has to build first, so the
// document never exists as a []Credential in between. The round trip is what
// says the two halves agree.
func TestADirectoryOfCredentialsOverTheWire(t *testing.T) {
	dir := people.NewCredentials(credential(), people.Credential{Owner: 9, State: people.StatusPending})

	var out bytes.Buffer
	if _, err := dir.WriteTo(&out); err != nil {
		t.Fatalf("writing the directory: %v", err)
	}
	if !strings.HasPrefix(out.String(), "[") {
		t.Errorf("the directory did not encode as an array: %s", out.String())
	}
	if !strings.Contains(out.String(), "hunter2") {
		t.Errorf("the token did not survive encoding: %s", out.String())
	}

	var back people.Credentials
	if _, err := back.ReadFrom(&out); err != nil {
		t.Fatalf("reading the directory: %v", err)
	}

	if got, want := back.Owners(), dir.Owners(); !slices.Equal(got, want) {
		t.Errorf("the owners came back %v, want %v", got, want)
	}
	if got, want := back.States(), dir.States(); !slices.Equal(got, want) {
		t.Errorf("the states came back %v, want %v", got, want)
	}

	// Through the projection rather than by indexing the directory, which is
	// how a test reads a spec-form declaration: under the marker tag the
	// declared type is forge's, and only the methods are spelled the same in
	// both builds.
	secrets := back.Secrets()
	if len(secrets) != 2 {
		t.Fatalf("%d secrets came back, want one column entry per element", len(secrets))
	}
	if secrets[0] == nil || secrets[0].Token != "hunter2" || secrets[0].Issued != "2026-01-01" {
		t.Errorf("the secret did not come back whole: %+v", secrets[0])
	}
	if secrets[1] != nil {
		t.Errorf("a credential with no secret came back with one: %+v", secrets[1])
	}
}

// The directory sorts in place, and sorts by the one key the declaration names.
//
// A different thing from the sorted view beside it, and the reason both exist:
// this rearranges the directory, where [people.Credentials.SortedByOwner]
// answers with a copy and leaves it alone. It is the only type in this package
// with the three methods sort.Sort wants, since a declaration naming several
// keys has no reason to prefer one of them and gets none.
func TestADirectorySortsInPlace(t *testing.T) {
	dir := people.NewCredentials(
		people.Credential{Owner: 3},
		people.Credential{Owner: 1},
		people.Credential{Owner: 2},
	)

	// Through sort.Sort and not slices.SortFunc, which is the whole of what is
	// being checked: the generated Less and Swap exist so that a value of this
	// type can be handed to something that sorts by the interface, and sorting
	// it any other way would leave both methods uncalled.
	sort.Sort(dir) //nolint:revive // the interface is the subject of the test

	if got, want := dir.Owners(), []int{1, 2, 3}; !slices.Equal(got, want) {
		t.Errorf("sorting in place gives %v, want %v", got, want)
	}

	// And the pair on their own, since sort.Sort is free to reach a sorted
	// answer without calling either on a list this short.
	pair := people.NewCredentials(people.Credential{Owner: 2}, people.Credential{Owner: 1})

	if !pair.Less(1, 0) || pair.Less(0, 1) {
		t.Error("Less does not order by the key the declaration names")
	}

	pair.Swap(0, 1)
	if got, want := pair.Owners(), []int{1, 2}; !slices.Equal(got, want) {
		t.Errorf("Swap left %v, want %v", got, want)
	}
}

// A lazy view runs its combinators over the elements without building a slice
// at each step.
//
// What the collection layer gives a caller who wants more than a column: the
// view is one value with the element type in it, so filtering and mapping read
// as what they do rather than as a loop somebody wrote again.
func TestALazyViewOverTheDirectory(t *testing.T) {
	dir := people.NewCredentials(
		people.Credential{Owner: 3, State: people.StatusRevoked},
		people.Credential{Owner: 1, State: people.StatusActive},
		people.Credential{Owner: 2, State: people.StatusActive},
	)

	live := dir.Seq().Filter(func(c people.Credential) bool {
		return c.State == people.StatusActive
	}).Collect()

	if got, want := owners(live), []int{1, 2}; !slices.Equal(got, want) {
		t.Errorf("the active credentials are %v, want %v", got, want)
	}

	first, ok := dir.Seq().First()
	if !ok || first.Owner != 3 {
		t.Errorf("the first element is %v, %v, want the one the directory opens with", first, ok)
	}
}

// A directory is built from a copy of what it was given, and reading into one
// replaces what was there.
//
// Two promises the generated code makes in passing and nothing else here reads.
// The first is what keeps a caller's slice from being a second handle on the
// directory; the second is what stops a decoder from appending one document
// onto the last.
func TestWhatBuildingAndReadingADirectoryLeaveBehind(t *testing.T) {
	from := []people.Credential{{Owner: 1}, {Owner: 2}}
	dir := people.NewCredentials(from...)

	from[0].Owner = 99
	if got := dir.Owners()[0]; got != 1 {
		t.Errorf("writing to the slice it was built from changed the directory, to %d", got)
	}

	var out bytes.Buffer
	if _, err := dir.WriteTo(&out); err != nil {
		t.Fatalf("writing the directory: %v", err)
	}
	document := out.String()

	// Read into one that already holds something, twice, which is where an
	// unreset decoder shows.
	into := people.NewCredentials(people.Credential{Owner: 7})
	for range 2 {
		if _, err := into.ReadFrom(strings.NewReader(document)); err != nil {
			t.Fatalf("reading the directory: %v", err)
		}
	}

	if got, want := into.Owners(), []int{1, 2}; !slices.Equal(got, want) {
		t.Errorf("reading twice left %v, want the last document alone", got)
	}
}

// What the directory says when the writer stops taking bytes, or the document
// stops halfway.
//
// The half of a codec that is only reached when something goes wrong, which is
// where a generated one is most worth checking: a caller who has to guess how
// many bytes reached the wire cannot retry, and a decoder that reported nothing
// on a truncated document would leave a half-filled directory looking whole.
func TestADirectoryThatCannotFinish(t *testing.T) {
	dir := people.NewCredentials(credential(), people.Credential{Owner: 9})

	stops := &shortWriter{after: 20}
	n, err := dir.WriteTo(stops)

	if err == nil {
		t.Fatal("a writer that refused the bytes reported no error")
	}
	if n != int64(stops.written) {
		t.Errorf("WriteTo reported %d bytes and the writer took %d", n, stops.written)
	}

	// And the other direction, from a document that stops inside an element.
	var back people.Credentials
	if _, err := back.ReadFrom(strings.NewReader(`[{"Owner":7,"Sec`)); err == nil {
		t.Error("a truncated document was read without complaint")
	}

	// A document of the wrong shape entirely, which is the other way a decoder
	// is asked for a directory and given something else.
	if err := json.Unmarshal([]byte(`{"Owner":7}`), &back); err == nil {
		t.Error("an object was read into a directory")
	}
}

// Walking backward stops when the caller stops.
//
// A generated iterator hands each element to a function that may say it has
// seen enough, and one that ignored the answer would keep calling after a break
// — which for a loop with a side effect is the loop running twice.
func TestWalkingBackwardStopsEarly(t *testing.T) {
	dir := people.NewCredentials(
		people.Credential{Owner: 1},
		people.Credential{Owner: 2},
		people.Credential{Owner: 3},
	)

	var seen []int
	for one := range dir.Backward() {
		seen = append(seen, one.Owner)
		break
	}

	if got, want := seen, []int{3}; !slices.Equal(got, want) {
		t.Errorf("breaking out of the walk saw %v, want %v", got, want)
	}

	var statuses []people.Status
	for one := range people.NewStatuses(people.ValuesStatus()...).Backward() {
		statuses = append(statuses, one)
		break
	}

	if got, want := statuses, []people.Status{people.StatusRevoked}; !slices.Equal(got, want) {
		t.Errorf("breaking out of the list's walk saw %v, want %v", got, want)
	}
}

// A list of statuses is the storage a declaration naming no container falls
// back to, and it works like one.
//
// Worth a test because it is generated and nothing else calls it: the closed
// set is what the declaration is written for, and this is what the same line
// gives the declared type on the way past.
func TestWhatAListOfStatusesCanDo(t *testing.T) {
	list := people.NewStatuses(people.ValuesStatus()...)

	if got := list.Len(); got != 3 {
		t.Errorf("the list holds %d, want 3", got)
	}
	if got := slices.Collect(list.All()); !slices.Equal(got, people.ValuesStatus()) {
		t.Errorf("walking gives %v, want the members in order", got)
	}
	if got, want := slices.Collect(list.Backward())[0], people.StatusRevoked; got != want {
		t.Errorf("walking backward starts at %s, want %s", got, want)
	}

	list.AppendSeq(slices.Values([]people.Status{people.StatusActive}))
	if got := list.Len(); got != 4 {
		t.Errorf("appending left %d, want 4", got)
	}

	list.Reset()
	if got := list.Len(); got != 0 {
		t.Errorf("resetting left %d, want none", got)
	}

	// Built from a copy, like the directory: a caller holding the slice it was
	// made from does not hold a second handle on the list.
	from := []people.Status{people.StatusPending}
	made := people.NewStatuses(from...)
	from[0] = people.StatusRevoked

	if got := slices.Collect(made.All())[0]; got != people.StatusPending {
		t.Errorf("writing to the slice it was built from changed the list, to %s", got)
	}
}

// owners reads the owner out of each credential, so that an ordering can be
// compared as the numbers it put things in.
func owners(of []people.Credential) []int {
	out := make([]int, len(of))
	for i, one := range of {
		out[i] = one.Owner
	}
	return out
}

// logging renders a value through a real handler and returns what came out.
//
// Through a handler rather than by reading the slog.Value, because what is
// being tested is what reaches the output: a log value that built the right
// answer and was never consulted would pass the narrower test and fail the only
// one that matters.
func logging(t *testing.T, one any) string {
	t.Helper()

	var out bytes.Buffer

	slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{
		// The time and level are noise here, and the time makes the output
		// different on every run.
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		},
	})).Info("seen", "held", one)

	return out.String()
}
