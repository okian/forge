//go:build forgespec

// This file is the spec side of the declarations the pack generates for.
//
// A stack has no honest underlying type to write down, so forge owns the
// declaration and the author owns the description of it, under complementary
// build tags. Exactly one is ever in scope.
//
// What is written here is the shape rather than the stack, because the pack
// builds its requests directly: what this file is for is that the tagged build
// has the names the stub file declares methods on, so that half can be compiled
// at all.
package model

// People is the container the rows about many values are earned by.
type People []Person

// Crowd is a second declaration over the same subject, with no codec of its
// own.
type Crowd []Person

// Codes is the container over the wrapper, which earns nothing itself — what
// the wrapper earns is about one value and lands on the subject.
type Codes []Code

// Locked is the container behind a lock, which is the one declaration that
// asked for the lock to be exposed.
//
// A slice here as everywhere else in this file: the shape is what the tagged
// build needs, and the stack is what the pack builds its request from.
type Locked []Person
