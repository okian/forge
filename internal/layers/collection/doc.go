// Package collection is the refining layer that adds a query surface built from
// the subject's own fields.
//
// It is the first layer whose output cannot be a template. A template is
// generic over its element and a type parameter has no fields, so nothing
// written once can say Ages or ByName — those are the subject's, and the
// subject is known only when a declaration is resolved. What a template can
// still hold is everything those methods do once they have the field in hand,
// which turns out to be all of it: the loops, the sorting and the map building
// live in the package beside this one, compiled by the ordinary build, and what
// is built per field is a single expression handing one field to one of them.
//
// That division is the answer to the objection a builder invites. Generated
// code assembled by hand is code nothing checks until somebody builds it; here
// the part that could be wrong in an interesting way is checked where it was
// written, and the part that is built is short enough to read.
//
// The surface it adds is a lazy view over the elements, a projection per
// exported field, a sorted view per field named in sort, and a lookup per field
// named in index. The last two are options because they are choices — an order
// and a key are things a declaration means rather than things a struct implies
// — and the projections are not, because a collection you cannot take a column
// out of is half a collection.
package collection
