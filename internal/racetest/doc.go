// Package racetest writes the stress test a concurrent declaration is held to.
//
// A layer that says a stack is safe to share is making a claim the compiler
// cannot check and a reviewer cannot read off the output. What can check it is
// the race detector, and what the detector needs is code that actually shares
// the value: several goroutines writing, several reading, and enough rounds of
// each that they overlap.
//
// So the test is written rather than hand-kept. There is one concurrent layer
// today and there will be more, and a stress test somebody has to remember to
// write for each of them is one that will be missing for whichever lands on a
// busy week. What is written here is written from the declaration's own names,
// so the test that exists for a new layer is the test that already existed for
// the last one.
//
// # What it exercises
//
// The surface a concurrent layer offers, on both sides of the lock it stands
// for. Writers open a write scope and add elements through it, and go on doing
// so until the readers have finished — a writing round adds one element where a
// reading round reads the whole container, so matched counts would leave the
// readers running alone for most of the test.
//
// Readers take every route out of the container the declaration turned out to
// have: a count, a copy read element by element, a walk inside a read scope,
// and the whole container written as a document. Each of those holds the value
// for a different length of time, and a layer that is right about one of them
// is not thereby right about the rest. Which of them exist is a fact about the
// declaration rather than about this package, so the written test names what it
// takes rather than promising a fixed four.
//
// # What it asserts
//
// That the calls ran. A scope that took the lock and never called what it was
// given races nobody and passes every check the detector makes, so the written
// test counts what happened inside each scope and refuses a run where any of
// those counts is zero — which is what tells a container that is correctly
// locked from one that does nothing.
//
// Beyond that, almost nothing. How much a container holds at the end is the
// storage layer's business, so the only thing asked of it is that what the
// writers added did not all vanish. The failure worth catching here has no
// assertion, because it is a race, and the detector is what reports it.
//
// # What it needs
//
// The scoped-access contract, by name: a write scope, a read scope, and a way
// to walk and to add elements from inside one. A concurrent layer that offers
// something else is refused rather than skipped — a layer with no stress test
// is the thing this package exists to prevent, and a silent skip is how it
// would happen.
//
// A method that answers with nothing is left alone. What one does cannot be
// read off its signature — it changes something, or it blocks, or both, as an
// exposed lock does — so calling one because it took no arguments is as likely
// to deadlock the run as to stress anything.
package racetest
