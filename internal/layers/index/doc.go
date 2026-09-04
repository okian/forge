// Package index generates keyed storage: elements held beside lookup maps
// over their declared fields, so that finding one is a map access rather than
// a scan.
//
// A declaration names one field as the key and may name more for secondary
// lookups:
//
//	//forge:index key=ID index=Name,Email
//	type Directory forge.Index[Person]
//
// The key is the primary dimension. Unique by default, it answers a lookup
// with a stable pointer to the one element held under a key, and it is what
// removal is by; declared unique=false it files every element sharing a key in
// one bucket and the pointer goes away, since there is no longer one element
// for it to name. What adding a held key does under a unique one is the
// declaration's choice — conflict=error refuses it, which is the default,
// because a key that has to be unique is a thing to check; conflict=replace
// swaps the element in place, for the caller whose adds are updates.
//
// Secondary lookups hold keys rather than elements and resolve through the
// primary map, which is what keeps removal from repairing more than the
// buckets the removed element was in — and is why they need the key to be
// unique: a bucket of keys that each reach several elements would walk one
// element as many times as its key was filed.
//
// The representation is an insertion-ordered slice of entries beside the maps,
// and every walk is over the slice: nothing this layer emits ranges a map, so
// what a walk or a codec produces does not depend on the order a map happens
// to come back in.
//
// The field-independent halves of the bodies live in the template package
// beside this one, compiled by the ordinary build; what is built per
// declaration is the container's own struct, the statements that hand fields
// to the template's helpers, and one lookup method per dimension. That is the
// split the collection layer established, and the choosing between compiled
// answers — both constructors, both appends, kept one and renamed — is the
// ring layer's.
//
// Two of the catalog's shape claims are deliberately not made. Keyed is not
// required of the stack beneath, because nothing adds it: the key comes from
// the declaration's own option, which is where R7 says keys come from. And
// Indexed is not added, because that capability means the language itself can
// reach an element by position — true of a slice underlying, not of a struct —
// and a layer above believing it would generate positional code over a type
// with no positions.
package index
