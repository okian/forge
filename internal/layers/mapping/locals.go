package mapping

import (
	"github.com/okian/forge/plugin"
)

// locals are the identifiers the generated constructor binds.
//
// Allocated rather than written down, because each of them is in scope over a
// body that also spells two types and the hint's own expressions. A source
// type called src would collide with the parameter, and a hint that names a
// package-level held would collide with the local holding the result — both
// files this layer would write without complaint and the compiler would then
// refuse, and neither is a file the author can edit.
type locals struct {
	// block holds every name the constructor's body may not take, and given
	// what each base name became — asked once and answered the same way after,
	// so that the line binding a name and every line reading it agree.
	block *plugin.Block
	given map[string]string
}

// naming allocates the identifiers the constructor binds, out of the way of
// every name its body also has to spell.
func naming(spellings ...string) locals {
	taken := make([]string, 0, len(spellings))
	for _, one := range spellings {
		taken = append(taken, plugin.Mentioned(one)...)
	}

	return locals{block: plugin.Locals(taken...), given: make(map[string]string)}
}

// name returns what one of the constructor's identifiers is called, moving it
// where a name the body spells already holds it.
func (l locals) name(base string) string {
	if held, ok := l.given[base]; ok {
		return held
	}

	out := l.block.Declare(base)
	l.given[base] = out
	return out
}
