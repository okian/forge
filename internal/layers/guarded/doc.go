// Package guarded generates a read-write lock around the stack beneath it.
//
// A lock is easy to add to a container and hard to add safely. The methods that
// hand a caller a sequence are the reason: one handed out from behind a lock is
// broken whichever side the caller walks it on — outside it races, and inside it
// holds the lock across whatever the caller does with each element, including a
// call back into the container that deadlocks against a lock which is not
// reentrant.
//
// So this layer does not lock the methods it was given. It moves all of them
// onto a type of its own — a method left on the declaration is a method
// somebody can call without the lock, whether or not it is one that hands out a
// sequence — and replaces the lot with scoped access: a function that runs with
// the lock held, handed a value that reaches the type underneath. What that
// value cannot do is open a second scope, because no method on it names the
// type it is a view of or the type it guards.
//
// # What is generated
//
// The declaration becomes a struct holding a lock and the stack beneath it,
// which is now a type of its own. Everything the stack below declared is on
// that inner type, so nothing reaches it without going through one of:
//
//	func (g *Sessions) Do(f func(v SessionsView))   // under the write lock
//	func (g *Sessions) RDo(f func(v SessionsView))  // under the read lock
//	func (g *Sessions) Snapshot() []Session         // a copy, then lock-free
//	func (g *Sessions) Len() int                    // one number, read under the lock
//
// Len is on the outside rather than reached through a scope because a length is
// one number read and handed back: there is nothing a caller can hold open, and
// making them write a closure for it would teach them that scopes are ceremony.
//
// # Making one
//
// The inner type is unexported and so is its constructor, which would leave a
// container that has to be made — a bounded one has to be told how much it
// holds — unreachable from outside the package it was generated into. So the
// layer beneath says how one of itself is made and this writes a call
// forwarding to it: NewSessions() where the size was written in the
// declaration, NewSessions(size int) where it is the caller's, and nothing at
// all over a container whose zero value is already one.
//
// # What a read scope does not enforce
//
// RDo holds the read lock, and what it hands over is the same view Do does — so
// a caller who changes the container inside a read scope has several readers
// changing it at once, which is a data race the compiler will not stop and this
// package cannot presently express. The generated method says so on the method.
//
// A read-only view is the answer and needs something the shape vocabulary does
// not have: which of a container's methods change it. The receiver is not it —
// a ring declares every method on the pointer, including its length — so
// telling them apart means every layer saying which of its methods write, and
// that is a change to what a layer describes rather than to what this one
// emits.
//
// # Encoding
//
// A codec on a guarded stack copies before it writes, so that a slow writer can
// never hold the lock for the length of a network round trip. That costs a copy
// of the elements per document, and a caller who owns their writer and would
// rather not pay it can say so — at which point the lock is held until the last
// byte is written, and whatever that writer does happens under it.
package guarded
