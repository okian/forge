// Package view writes the type a decorator hands a caller inside a scope.
//
// A decorator that owns something a caller must not hold — a lock, a
// transaction, a borrowed buffer — cannot let them reach the stack beneath it
// directly. What it offers instead is scoped access: a function that runs with
// the thing held, and a value passed to that function which reaches everything
// below and nothing above.
//
// That value is what this writes. It is the surface of the stack beneath the
// decorator, and a method whose signature names the view or the type it guards
// is refused rather than forwarded — so the way out that names either of them
// cannot be written.
//
// # What that is worth, and what it is not
//
// It is worth having, and it is smaller than it sounds. What it removes is the
// path a caller takes by accident: reaching for the value they were just handed
// and finding a way back in. Four paths it does not remove.
//
// The one taken deliberately. The decorated value is still in scope inside the
// closure, and calling a method on it is ordinary Go:
//
//	s.Do(func(v SessionsView) { s.Len() }) // still deadlocks
//
// The one that outlives the scope. Go has no way to say a value may not leave
// the call it was obtained in, so a view kept past the call — or a sequence
// taken from one and walked afterwards — reaches the same data with nothing
// held. This is not a gap in the check: All returns a sequence, and handing one
// out is the whole of what the scope is for.
//
// The one spelled some other way. What is refused is an identifier, so a method
// handing back an interface the decorated type satisfies, or an alias for it, is
// a way out that no syntactic check would see.
//
// And the one that goes round the methods entirely. A view is generated into
// the author's own package, so its field is reachable by hand and its zero
// value exists — an unexported name is a convention there, not a boundary.
//
// A design that claimed more would be worse than one that claimed less. The
// deadlock this removes is real and common; the four it does not need saying
// plainly, so that whoever reads the generated type knows what is still
// theirs.
package view
