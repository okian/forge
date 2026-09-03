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
	// receiver is what the container's own methods call it, and value what a
	// codec calls the value it is reading into or writing out of.
	receiver string
	value    string

	// encoder and decoder are the streams a codec is handed. Parameters, which
	// shadow as readily as a variable does: a parameter's scope is the function
	// body, and the body is where the members are spelled.
	encoder string
	decoder string
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

	block := plugin.Locals(taken...)

	return locals{
		receiver: block.Declare("c"),
		value:    block.Declare("v"),
		encoder:  block.Declare("enc"),
		decoder:  block.Declare("dec"),
	}
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
