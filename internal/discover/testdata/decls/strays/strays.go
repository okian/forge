// Package strays holds directives that land on nothing, one of each shape an
// author can produce.
package strays

import "declsfixture/markers"

// Item is the subject.
type Item struct{ SKU string }

// Loose is not an instantiation, so the directive above it applies to nothing.
//
//forge:collection index=SKU
type Loose []Item

// Aliased keeps the methods it already has, so the directive is dead.
//
//forge:collection index=SKU
type Aliased = markers.Collection[Item]

//forge:ring cap=8
type (
	// The directive above this group sits above both declarations and could not
	// say which one it means.
	First  markers.Ring[Item]
	Second markers.Ring[Item]
)

// Detached has a directive separated from it by a blank line, so the parser
// never attaches it.

//forge:collection index=SKU

// Detached is the declaration the directive above was meant for.
type Detached markers.Collection[Item]

// Spaced carries the likeliest typo of all, which gofmt will not fix because it
// cannot know the intent.
//
// forge:collection index=SKU
type Spaced markers.Collection[Item]

/*forge:collection index=SKU*/

// Blocked carries a directive written as a block comment.
type Blocked markers.Collection[Item]
