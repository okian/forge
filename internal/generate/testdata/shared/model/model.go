// Package model holds two subjects that reach one struct between them.
//
// It is the arrangement the shared file exists for: whatever a layer writes
// about Address is written once however many declarations reach it, because a
// package holding one function twice does not compile.
package model

import "log/slog"

// Address is the struct both subjects reach, and the one whose helpers there
// must be exactly one of.
type Address struct {
	City  string `validate:"required"`
	Lines []string
}

// Person reaches it directly.
type Person struct {
	Name  string `validate:"required"`
	Home  Address
	Tags  []string
	Notes map[string]string
}

// Employer reaches the same Address, from a different subject.
type Employer struct {
	Title string
	Site  Address
}

// Account carries a redact tag, which earns a log value from the tag alone and
// is also what the redaction layer is asked for.
//
// Both write the same method on the same type, so a package that ran both would
// hold one of them twice and not compile. Which of the two gives way is the
// thing this subject exists to pin down.
type Account struct {
	Name  string
	Token string `redact:""`
}

// Holder reaches the secret rather than holding one, so a layer over it writes
// about Account without Account being anybody's subject.
//
// The case a check that only looked at a declaration's own subject misses: what
// the layer wrote is a method on Account, and a neighbouring declaration over
// Account would earn a second one from the tag unless the two are counted
// together.
type Holder struct {
	Name  string
	Creds Account
}

// Written declares the log value itself, which is the author overriding what
// the tag would otherwise earn them.
type Written struct {
	Name  string
	Token string `redact:""`
}

// LogValue is the author's own, and is the one a package holds.
func (w Written) LogValue() slog.Value { return slog.StringValue("theirs") }
