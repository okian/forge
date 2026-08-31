// Package generate turns resolved declarations into the files that are written
// for them.
//
// It is the stage where everything the earlier ones found comes together: a
// declaration's options are validated against the layers it names, the stack is
// composed, each layer is asked what it contributes, what they contributed is
// merged, and what is left over — the helpers a layer called into and did not
// declare — is emitted once for the package rather than once per declaration.
//
// A package at a time rather than a declaration at a time, and that is the
// whole reason this is a package of its own. Two declarations in one package
// share the helpers they require, and a name one of them generates is a name
// the other may not; neither is a question a declaration can answer about
// itself. The stage that can answer both is the one that sees the package.
//
// What it produces is bytes and a name, not a write. Generating and checking
// are the same work up to that point and differ only in what they do with the
// answer — one writes it where the other compares it — so the split is here,
// where the two verbs still agree about everything.
package generate
