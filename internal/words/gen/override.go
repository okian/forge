package main

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"
)

// overrides is the list of upstream entries forge deliberately disagrees with.
//
// Embedded rather than read from disk so that the converter is one binary with
// one answer, and so that a change to the list is a change to the program that
// applies it rather than to a file somebody may or may not have beside them.
//
//go:embed overrides.txt
var overrides string

// override applies the list to the tables the conversion built.
//
// This is the honest version of the hand-written table the collection layer
// used to carry. It is small because everything not in it came from the
// dictionary, and every line says why — a line without a reason is refused,
// because a disagreement with upstream that nobody wrote down is one nobody can
// review.
func override(nouns, agents map[string]string) error {
	return applyAll(overrides, nouns, agents)
}

// applyAll reads one override list, so that the committed one and a list a test
// writes are read by the same code.
func applyAll(list string, nouns, agents map[string]string) error {
	for at, text := range strings.Split(list, "\n") {
		rule, reason, _ := strings.Cut(text, "#")

		fields := strings.Fields(rule)
		if len(fields) == 0 {
			continue
		}
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("%w: overrides.txt line %d: %q has no reason after #", errOverride, at+1, rule)
		}

		if err := apply(nouns, agents, fields); err != nil {
			return fmt.Errorf("%w: overrides.txt line %d: %w", errOverride, at+1, err)
		}
	}
	return nil
}

// errOverride reports an overrides.txt the converter will not apply.
var errOverride = errors.New("override")

// apply carries out one directive.
func apply(nouns, agents map[string]string, fields []string) error {
	switch verb, rest := fields[0], fields[1:]; {
	case verb == "plural" && len(rest) == 2:
		nouns[rest[0]] = rest[1]

	case verb == "uncountable" && len(rest) == 1:
		nouns[rest[0]] = rest[0]

	case verb == "agent" && len(rest) == 2:
		agents[rest[0]] = rest[1]

	case verb == "drop" && len(rest) == 1:
		delete(nouns, rest[0])
		delete(agents, rest[0])

	default:
		return fmt.Errorf("%q is not a directive this converter knows", strings.Join(fields, " "))
	}
	return nil
}
