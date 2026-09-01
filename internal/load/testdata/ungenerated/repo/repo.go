// Package repo names a type from the package next door that only generation
// declares.
//
// A repository over a model is the ordinary arrangement, and a method returning
// the view forge writes is the ordinary thing to put on one — so the name that
// cannot be found is in the neighbouring package rather than in this one. A
// suggestion that only ever asked about the package holding the error would
// leave this unexplained, which is the layout most likely to produce it.
package repo

import (
	"ungeneratedfixture/asked"

	aliased "ungeneratedfixture/asked"
)

// Repo hands back the collection's view, which nothing has written yet.
type Repo struct{}

// People names the view type, in a signature that outlives the stripped body.
func (r *Repo) People() asked.PersonsSeq { return asked.PersonsSeq{} }

// Aliased reaches the same package under a name of the author's choosing.
//
// A qualifier is what somebody wrote, not what the package calls itself, so
// resolving one by asking the imported packages their names answers for every
// import except the ones an alias was written for.
func (r *Repo) Aliased() aliased.PersonsSeq { return aliased.PersonsSeq{} }

// Counted calls a generated method on a type from the other package, which is
// the member form of the same question with the receiver qualified.
var Counted = asked.Persons(nil).Len()
