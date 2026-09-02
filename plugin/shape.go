package plugin

import (
	"github.com/okian/forge/internal/shape"
)

// Shape is what a stack offers at one point in it: the capabilities everything
// up to here has, the element type the layers above see, and the methods that
// have landed on the declared type.
//
// It is both halves of composition. Capabilities answer Layer.Accepts, which
// is a question about whether a stack makes sense; the method surface is what
// lets a decorator wrap or withdraw what is beneath it, since a lock cannot
// wrap what it cannot enumerate.
type Shape = shape.Shape

// Cap is one capability a stack may have.
type Cap = shape.Cap

// CapSet is a set of capabilities.
type CapSet = shape.CapSet

// Method is one method on the declared type, as the layers above it see it.
type Method = shape.Method

// The capabilities a layer may require of what is beneath it or add to what is
// above.
//
// Sized is a length. Ordered is a stable position, which is not the same as
// sorted — it is the promise that walking twice walks the same way. Indexed is
// reachable by position. Keyed is reachable by a key. Streamable is walkable as
// a sequence, which is what a codec over a container needs and what a lock
// withdraws. Structured is a subject with fields, which every layer reading the
// subject rather than the container requires. Encodable is a wire form.
// Comparable is a stable identity, which is what lets a subject with no
// comparable form be a set member. Bounded is a limit on how much is held.
// Concurrent is safe to use from more than one goroutine.
const (
	Sized      = shape.Sized
	Ordered    = shape.Ordered
	Indexed    = shape.Indexed
	Keyed      = shape.Keyed
	Streamable = shape.Streamable
	Structured = shape.Structured
	Encodable  = shape.Encodable
	Comparable = shape.Comparable
	Bounded    = shape.Bounded
	Concurrent = shape.Concurrent
)

// Caps returns a capability set holding these.
//
// Named for what it builds rather than for the operation, because the word set
// is taken twice over in one package: a set of capabilities is one thing and a
// set of diagnostics is another, and a reader of a layer should not have to
// work out which from the arguments.
func Caps(members ...Cap) CapSet { return shape.Set(members...) }

// Every returns the set of every capability, which is what a test asking what a
// layer requires holds a stack against.
func Every() CapSet { return shape.Every() }
