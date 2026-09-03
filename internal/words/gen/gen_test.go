package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okian/forge/internal/words"
)

// A few lines of infl.txt, chosen for what each of them makes the converter do.
const inflections = `person N: people, persons 0.1
child N: children
epoch N: epochs
box N: boxes {container}, box {shrub}, boxes 0.1 {shrub}
sibling N: sibling, siblings~ 1
sheep N: sheep
alias N: aliases
Anser N: anseres
series N: serieses?
run V: ran | run | running | runs
open V: opened | opening | opens
marshal V: marshaled, marshalled 1 | marshaling, marshalling 1 | marshals
quick A: quicker | quickest
not an entry at all
`

// archive writes the fixture the converter reads.
func archive(t *testing.T, text string) string {
	t.Helper()

	var body bytes.Buffer
	zipped := gzip.NewWriter(&body)
	held := tar.NewWriter(zipped)

	if err := held.WriteHeader(&tar.Header{Name: "agid-2016.01.19/README", Size: 2, Mode: 0o644}); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	if _, err := held.Write([]byte("hi")); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	if err := held.WriteHeader(&tar.Header{
		Name: "agid-2016.01.19/infl.txt", Size: int64(len(text)), Mode: 0o644,
	}); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	if _, err := held.Write([]byte(text)); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	for _, one := range []func() error{held.Close, zipped.Close} {
		if err := one(); err != nil {
			t.Fatalf("writing the fixture: %v", err)
		}
	}

	at := filepath.Join(t.TempDir(), "agid.tar.gz")
	if err := os.WriteFile(at, body.Bytes(), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	return at
}

func TestWhatSurvivesTheConversion(t *testing.T) {
	out := filepath.Join(t.TempDir(), "english.bin")
	if err := run(archive(t, inflections), out, ""); err != nil {
		t.Fatalf("run: %v", err)
	}

	asset, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading the asset: %v", err)
	}

	line, _, _ := bytes.Cut(asset, []byte{'\n'})
	if !strings.Contains(string(line), "agid=2016.01.19") {
		t.Errorf("the provenance %q does not name the release", line)
	}

	held, err := words.Load(asset)
	if err != nil {
		t.Fatalf("loading the asset: %v", err)
	}

	for _, one := range []struct {
		what   string
		word   string
		want   string
		wanted bool
	}{
		{"an irregular plural", "person", "people", true},
		{"another", "child", "children", true},

		// A pair the regular rules already reach is bytes spent to say what
		// they say.
		{"a regular pair", "box", "", false},
		{"another regular pair", "alias", "", false},

		// A pair the rules get wrong is kept even where it looks regular: the
		// ch in epoch is the sound in monarch.
		{"a pair the rules get wrong", "epoch", "epochs", true},

		// Upstream's zero plural, believed only where it lists no s form.
		{"a doubted zero plural", "sibling", "", false},
		{"a real one", "sheep", "sheep", true},

		// An override, applied while converting.
		{"an override", "series", "series", true},

		// A proper noun can only be reached by accident, and taking Anser for
		// Anseres is the accident.
		{"a proper noun", "anser", "", false},
	} {
		got, is := held.Plural(one.word)
		if is != one.wanted || got != one.want {
			t.Errorf("%s: Plural(%q) = %q, %v; want %q, %v",
				one.what, one.word, got, is, one.want, one.wanted)
		}
	}

	if got, is := held.Singular("people"); !is || got != "person" {
		t.Errorf("Singular(people) = %q, %v; want person", got, is)
	}

	// A verb whose -ing form doubles is recorded; one whose does not is left to
	// the rules, and one ending in l is left to them on purpose.
	if got, is := held.Agent("run"); !is || got != "runner" {
		t.Errorf("Agent(run) = %q, %v; want runner", got, is)
	}
	if _, is := held.Agent("open"); is {
		t.Error("Agent(open) was recorded, and the rules already have it")
	}
	if _, is := held.Agent("marshal"); is {
		t.Error("Agent(marshal) was recorded, and its ending is a dialect rather than a fact")
	}

	// The vocabulary is what tells a singular noun ending in s from a plural.
	if !held.Known("alias") {
		t.Error("the vocabulary does not hold alias, so Aliases would read as a word to pluralise")
	}
}

func TestARunWithNothingToConvert(t *testing.T) {
	if err := run("", "", ""); err == nil {
		t.Error("run with no archive succeeded")
	}
	if err := run(filepath.Join(t.TempDir(), "absent.tar.gz"), "", ""); err == nil {
		t.Error("run over an absent archive succeeded")
	}

	at := filepath.Join(t.TempDir(), "plain.tar.gz")
	if err := os.WriteFile(at, []byte("not a gzip"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	if err := run(at, "", ""); err == nil {
		t.Error("run over something that is not an archive succeeded")
	}

	if err := run(archive(t, "person N: people\n"), filepath.Join(t.TempDir(), "no", "such", "dir"), ""); err == nil {
		t.Error("run writing into a directory that does not exist succeeded")
	}
}

// An archive with no infl.txt in it is a release the converter cannot read, and
// saying so beats writing an empty dictionary.
func TestAnArchiveWithNoInflections(t *testing.T) {
	var body bytes.Buffer
	zipped := gzip.NewWriter(&body)
	held := tar.NewWriter(zipped)

	if err := held.WriteHeader(&tar.Header{Name: "agid/README", Size: 0, Mode: 0o644}); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	for _, one := range []func() error{held.Close, zipped.Close} {
		if err := one(); err != nil {
			t.Fatalf("writing the fixture: %v", err)
		}
	}

	at := filepath.Join(t.TempDir(), "empty.tar.gz")
	if err := os.WriteFile(at, body.Bytes(), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	if err := run(at, "", ""); err == nil {
		t.Error("run over an archive with no infl.txt succeeded")
	}
}

// The version on the command line wins, which is how a release that unpacks
// into a differently named directory is still recorded correctly.
func TestTheVersionACallerNames(t *testing.T) {
	out := filepath.Join(t.TempDir(), "english.bin")
	if err := run(archive(t, "person N: people\n"), out, "2020.01.01"); err != nil {
		t.Fatalf("run: %v", err)
	}

	asset, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading the asset: %v", err)
	}
	if !bytes.Contains(asset, []byte("agid=2020.01.01")) {
		t.Error("the provenance does not carry the version the caller named")
	}
}

// A disagreement with upstream that nobody wrote down is one nobody can review,
// so the converter refuses the line rather than applying it.
func TestAnOverrideWithNoReason(t *testing.T) {
	for _, one := range []struct {
		what string
		text string
	}{
		{"no reason", "uncountable series\n"},
		{"an empty reason", "uncountable series #   \n"},
		{"a directive nobody knows", "backwards series # because\n"},
		{"a directive with the wrong number of words", "plural series # because\n"},
	} {
		nouns, agents := map[string]string{}, map[string]string{}
		if err := applyAll(one.text, nouns, agents); err == nil {
			t.Errorf("%s: the override was accepted", one.what)
		}
	}

	nouns, agents := map[string]string{"person": "persons", "run": ""}, map[string]string{"run": "runner"}
	text := strings.Join([]string{
		"# a comment",
		"",
		"plural person people # upstream agrees, and this pins it",
		"uncountable series   # upstream gives serieses",
		"agent frob frobber   # because",
		"drop run             # because",
	}, "\n")

	if err := applyAll(text, nouns, agents); err != nil {
		t.Fatalf("applying: %v", err)
	}
	for key, want := range map[string]string{"person": "people", "series": "series"} {
		if nouns[key] != want {
			t.Errorf("after the overrides %s is %q, want %q", key, nouns[key], want)
		}
	}
	if agents["frob"] != "frobber" {
		t.Errorf("after the overrides frob is %q, want frobber", agents["frob"])
	}
	if _, held := nouns["run"]; held {
		t.Error("after the overrides run is still a noun")
	}
	if _, held := agents["run"]; held {
		t.Error("after the overrides run is still a verb")
	}
}
