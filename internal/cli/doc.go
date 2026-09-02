// Package cli is forge's command line: the verbs, the flags, and the walk from
// a pattern to the files a run writes.
//
// It is a package rather than a main because a binary cannot be imported. A
// layer forge does not ship is code, so a binary that knows about one is a
// binary somebody linked it into — and what they call to get forge's own
// commands over their own catalog is here, reached through
// [github.com/okian/forge/driver]. Forge's own binary calls the same thing with
// the same catalog every time.
//
// The verbs are one file each and share three things. An environment carries
// the streams to write to, the directory patterns resolve from, and the
// catalog the run knows; a pipeline carries the stages from a load to a
// resolved declaration, held rather than reached for so that a test can hand a
// verb declarations that were never on disk; and a report turns a set of
// diagnostics into what a person reads and a status a script can act on.
//
// Nothing here decides what a layer generates. What a verb does is find the
// declarations, ask the stages about them, and put what came back where it
// goes — so a verb that grew an opinion about a subject would be the wrong
// place for it twice over.
package cli
