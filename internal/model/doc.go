// Package model holds the shared description of a generation request: the
// declaration that asked for it, the layer stack it names, the subject that
// stack is specialised to, and the options attached to the declaration.
//
// Every stage of the pipeline reads and writes these types, and every layer
// programs against them, which is why they are deliberately inert. They
// describe; they do not decide. Classification, validation and generation live
// in the packages that do that work, so that changing a rule never means
// changing the vocabulary the whole pipeline speaks.
//
// Two properties are load-bearing and worth stating once:
//
// Stacks read outermost first. Stack[0] is the layer the declaration names
// first, which is the layer that determines the public API and the generated
// type's name, and the subject is held separately in [Model.Subject] rather
// than as a final stack entry.
//
// Nothing here may leak iteration order into generated output, because
// generating twice from identical inputs has to produce identical bytes. Every
// collection in these types is therefore an ordered slice with a linear
// lookup, never a map — including the option sets and struct tags, which are
// small enough that the scan costs nothing and which carry the author's own
// order, so a diagnostic can report them the way they were written.
//
// One hazard is left, and it is worth naming rather than pretending away:
// [Model.Pkg] is a *packages.Package, whose Imports and TypesInfo fields are
// maps. Anything that walks them owes the result a sort before it reaches the
// emitter.
package model
