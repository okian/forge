// Package forge declares the marker types that turn a Go type declaration
// into a code generation request.
//
// A declaration whose right-hand side instantiates one or more markers asks
// the forge command for an implementation:
//
//	type Persons Collection[Ring[Json[Person]]]
//
// The command resolves that instantiation into a layer stack — Collection over
// Ring over Json over Person — and emits a concrete type with the combined
// API: ring-buffer storage, query methods specialised to Person's fields, a
// reflection-free JSON codec, and the standard library interfaces those layers
// imply.
//
// Markers exist only to be named in declarations. They carry no behavior, and
// generated code never refers to them: it imports the standard library and the
// subject's own dependencies, nothing more.
//
// # Running the generator
//
// Generation is driven by the forge command, usually through go generate:
//
//	//go:generate go run github.com/okian/forge/cmd/forge generate ./...
//
// Put that directive in a file with no build constraints, such as a package's
// doc.go. The go generate tool honors build constraints, so a directive placed
// in a file guarded by //go:build forgespec is skipped without a word.
package forge
