package plugin

import (
	"go/token"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/tags"
)

// Code is a diagnostic's stable identifier, the FRG number an author sees and
// searches for.
//
// Ask it Code.Ours to tell one of forge's from one a layer raised, which is
// what says whose documentation to look in.
type Code = diag.Code

// Register reserves a code and the one-line summary that goes with it, and
// returns the code.
//
// Called at package scope, so that the whole set is known without generating
// anything — which is what lets the codes be listed and what catches two
// diagnostics claiming one number as a panic at start-up rather than as two
// reports an author cannot tell apart.
//
// Take codes above 6000. Everything below is forge's, and the ranges are in the
// package documentation.
func Register(code Code, summary string) Code { return diag.Register(code, summary) }

// Diagnostic is one thing wrong, with the position of the declaration it is
// about.
//
// The declaration and not the generated file. An author cannot edit generated
// code, so a report pointing there tells them where the consequence landed
// rather than where the cause is.
type Diagnostic = diag.Diagnostic

// New builds a diagnostic against a code, at a position, with a message.
//
// Add a hint with Diagnostic.WithHint. The message says what is wrong and the
// hint says what to do about it, and a report without the second is one an
// author has to guess from.
func New(code Code, pos token.Position, format string, args ...any) Diagnostic {
	return diag.New(code, pos, format, args...)
}

// Diagnostics collects them, so that a layer reports everything wrong with a
// declaration rather than the first thing.
//
// Worth the trouble: an author who has three fields a codec cannot write should
// learn that in one run, not in three.
//
// Named for what it holds rather than for what it is, since a set of
// capabilities is the other set in this package and a bare Set would say
// neither.
type Diagnostics = diag.Set

// From returns the diagnostic an error carries, and whether it was one.
//
// A layer returning a diagnostic from Layer.Generate returns it as an error,
// and this is how the receiving end gets it back with its code and hint intact.
func From(err error) (Diagnostic, bool) { return diag.From(err) }

// Tag is one parsed struct tag: its key, the name it gives, and its options.
type Tag = tags.Tag

// TagOption is one option written in a struct tag, after the name.
//
// Named for where it comes from, because a directive above a declaration
// carries options too and they are a different type — see [DirectiveOption].
// Reading a tag and reading a directive are different jobs with the same word
// for the thing being read, and a layer doing both should not have to work out
// which it has.
type TagOption = tags.Option

// DirectiveOption is one option written on a directive: its key, its value, and
// where it was written.
//
// A layer reading its own options usually wants Options.Get or Options.List
// instead, which answer with strings and are what the schema was checked
// against. This is for a layer that wants the position too, which is what a
// diagnostic about one option rather than about the declaration needs.
type DirectiveOption = model.Option

// Problem is one thing wrong with a struct tag's literal.
//
// Not a diagnostic: a tag is read where a layer happens to be looking rather
// than at a position forge can point at, so what comes back says what is wrong
// and leaves reporting it to whoever knows which field it was on.
type Problem = tags.Problem

// ParseTag parses a struct tag's whole literal into the tags it holds, and what
// was wrong with it.
//
// A layer reading a tag it declared usually wants Field.Tag instead, which
// answers about one key on one field. This is for a layer that has a raw tag
// and no field to ask.
func ParseTag(raw string) ([]Tag, []Problem) { return tags.Parse(raw) }
