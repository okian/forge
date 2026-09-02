package words_test

import (
	"testing"

	"github.com/okian/forge/internal/words"
)

func TestNamingAnInterfaceAfterItsMethod(t *testing.T) {
	for verb, want := range map[string]string{
		// The -er ending is not a suffix anybody can append, which is the whole
		// reason this is here rather than in a layer.
		"Validate": "Validator",
		"Notify":   "Notifier",
		"Marshal":  "Marshaller",
		"Encode":   "Encoder",
		"Read":     "Reader",
		"Write":    "Writer",
		"Handle":   "Handler",
		"Serve":    "Server",
		"Scan":     "Scanner",

		// Doubling, where the dictionary knows and the spelling does not say.
		"Run":    "Runner",
		"Get":    "Getter",
		"Begin":  "Beginner",
		"Commit": "Committer",
		"Open":   "Opener",
		"Log":    "Logger",

		// A final l doubles however many syllables there are, which is the
		// spelling this repository's own prose is written in.
		"Control": "Controller",
		"Install": "Installer",

		// -ate takes -ator, except where the ate is the end of a word rather
		// than a suffix.
		"Iterate":  "Iterator",
		"Generate": "Generator",
		"Update":   "Updater",

		// A y after a vowel is not one, and a domain verb the dictionary has
		// never heard of takes the plain ending.
		"Play": "Player",
		"Frob": "Frobber",
		"":     "",
	} {
		if got := words.Agent(verb); got != want {
			t.Errorf("Agent(%q) = %q, want %q", verb, got, want)
		}
	}

	if got := words.Agent(); got != "" {
		t.Errorf("Agent() = %q, want nothing", got)
	}
	if got := words.Agent("person", "validate"); got != "PersonValidator" {
		t.Errorf("Agent(person, validate) = %q, want PersonValidator", got)
	}
	if got := words.Agent("_"); got != "" {
		t.Errorf("Agent(_) = %q, want nothing", got)
	}
}
