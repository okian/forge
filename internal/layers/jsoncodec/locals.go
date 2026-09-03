package jsoncodec

import (
	"github.com/okian/forge/plugin"
)

// locals are the identifiers the generated codec binds.
//
// Allocated rather than written down, because each of them is in scope over a
// body that also spells a type. A subject called dec gave the decoder
// `func(dec *jsontext.Decoder, v *dec) error`, and one called c gave the
// container's method `func (c *Held)` over a body naming c — both files this
// layer wrote without complaint and the compiler then refused, and neither is
// a file the author can edit.
//
// The subject's own name is the one a layer cannot know in advance, so these
// are the names that move and the subject keeps the spelling its author gave
// it. The other direction would leave the author's own code naming something
// forge had renamed underneath it.
type locals struct {
	// block holds every name the codec's bodies may not take, and given what
	// each base name became — asked once and answered the same way after, so
	// that the line binding a name and every line reading it agree.
	block *plugin.Block
	given map[string]string
}

// naming allocates the identifiers a codec binds, out of the way of every name
// its bodies also have to spell.
//
// Seeded with the spellings rather than only with what the file imports. A type
// from another package arrives qualified and its qualifier is an import; one
// declared in the package being generated into arrives bare, so its name
// reaches this only through the spelling. Type arguments come with it, since a
// codec for Box[dec] names dec as surely as one for dec does.
func naming(spellings ...string) locals {
	taken := make([]string, 0, len(spellings))
	for _, one := range spellings {
		taken = append(taken, plugin.Mentioned(one)...)
	}

	return locals{block: plugin.Locals(taken...), given: make(map[string]string)}
}

// name returns what one of the codec's identifiers is called, moving it where a
// type the bodies spell already holds the name.
func (l locals) name(base string) string {
	if held, ok := l.given[base]; ok {
		return held
	}

	out := l.block.Declare(base)
	l.given[base] = out
	return out
}

// spelled returns every type spelling a form's codec writes down, which is what
// [naming] is seeded with.
//
// Reached through the parts rather than only the whole: a struct's codec spells
// each member's type, and a composite's spells its key and its element.
//
// The walk records where it has been, because a type can contain itself. A tree
// whose node holds a pointer to a node is an ordinary shape and has no finite
// expansion, so a walk that did not remember would not return. Recorded by form
// rather than by spelling, which is the same reason [writer.asking] is: two
// distinct types can be spelled the same way in different packages.
func spelled(of *form) []string {
	return walked(of, make(map[*form]bool))
}

// walked is [spelled] with the set of forms already visited.
func walked(of *form, seen map[*form]bool) []string {
	if of == nil || seen[of] {
		return nil
	}
	seen[of] = true

	held := []string{of.spelled.Text}

	held = append(held, walked(of.key, seen)...)
	held = append(held, walked(of.elem, seen)...)

	for at := range of.members {
		held = append(held, walked(&of.members[at].of, seen)...)

		// And what a member reached through an embedded pointer is guarded by,
		// which arrives here through no other route: the members are flattened,
		// so the embedded struct is not a member of its own and its element is
		// spelled only in the line that allocates it.
		for _, guard := range of.members[at].guards {
			held = append(held, guard.elem)
		}
	}

	return held
}
