package model

// Whether a marker is written through a dot import or through a package name is
// a fact about the file and not about the declaration, and resolution must not
// be able to tell.
import . "github.com/okian/forge"

// Guests is People written the other way round.
type Guests Collection[Person]
