// Package redact generates the value a subject may be logged as.
//
// What it writes is a LogValue method — [log/slog.LogValuer] — for the subject
// and for everything the subject reaches, with the fields tagged redact
// replaced by a fixed string:
//
//	type Account struct {
//		Name  string
//		Token string `redact:""`
//	}
//
//	slog.Info("saving", "account", held) // account.Name=ada account.Token=[redacted]
//
// Implementing that method is what keeps a field out of a log. slog reaches for
// a value's fields when the value does not say otherwise, so a type with a
// secret in it and no LogValue prints the secret. A handler can be given a hook
// that rewrites attributes as they pass, but that is a property of the handler:
// every place the value is logged from has to have installed it, and one that
// has not prints what the type offers.
//
// # Why the whole closure, and why that is the point
//
// A type's own LogValue is not enough, because slog resolves one value at a
// time. Given an Account holding a Credentials, a LogValue on Account that
// hands the credentials over as they are has done nothing at all: slog reaches
// into the struct it was given and prints what is in it, tag or no tag.
//
// So every struct the subject reaches that has anything to hide is written one
// of its own, and the outer method hands the inner value over knowing that the
// inner method will be asked. That is the difference between this layer and the
// LogValue a redact tag earns on its own, which covers the subject and stops
// there.
//
// # What is refused, and why refusing is the safe answer
//
// A secret behind a slice, an array or a map is refused rather than logged.
// slog resolves a LogValuer for the value of an attribute; it does not walk
// into a slice of them and resolve each element, so a []Credentials is printed
// by the handler as the struct it is, LogValue and all. Writing the method
// anyway would produce a value that is redacted everywhere the author looks and
// leaks where they do not, which is worse than one that never claimed to be
// safe: a partial redaction is read as a complete one.
//
// The way out is to say so at the outer field. A slice tagged redact is
// replaced whole, which is both writable and what somebody who marked its
// contents secret meant.
//
// A secret held in a type that cannot carry a method is refused for the
// neighbouring reason: there a log value exists and slog will not ask for it,
// here there is none to ask for. Three types cannot carry one — a struct
// written in place, which has no name; a type from another package, which Go
// lets only its own package declare on; and an instantiation of a generic,
// which has nowhere to put a method at all. The last is the one worth naming:
// what looks like a method on Holder[Credentials] is a method on Holder, so
// writing it would change how every other instantiation logs.
//
// An interface holding something with a log value of its own is not refused and
// does not need to be: slog resolves what the interface holds, so the value
// speaks for itself exactly as it would anywhere else. A channel and a function
// are not refused either, and print as the addresses they are.
//
// # What a redacted field logs
//
// A fixed string, and the same one for every field. Not the value shortened,
// starred, or hashed: a length is something, a prefix is more, and two records
// holding one secret are told apart by any hash of it. A field marked as not
// for logs was marked by somebody who did not want to work out which of those
// is safe.
//
// # Asking for it with nothing to hide is refused
//
// A subject that reaches no redact tag is one whose LogValue would say exactly
// what slog says without it — a method that has to be regenerated every time a
// field is added, in exchange for nothing. The declaration is refused rather
// than written, because a layer that quietly did nothing would leave an author
// believing a value is protected.
package redact
