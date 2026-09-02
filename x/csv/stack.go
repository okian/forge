package csv

import (
	"strings"

	"github.com/okian/forge/plugin"
)

// The methods of the streaming contract a document is written over.
//
// Named here rather than at each use, because they are one contract: a layer
// that renames its walk stops being something a transport can be written over,
// and the place that says so should be the place that spells it.
const (
	walkMethod   = "All"
	appendMethod = "AppendSeq"
	resetMethod  = "Reset"
	capMethod    = "Cap"
)

// The methods this layer puts on the declared type.
//
// Qualified, and always. A stack may hold more than one thing that turns into
// bytes, and the plain names belong to whichever of them the author designated
// — which nobody has, so this layer takes the qualified ones rather than
// taking a name that depends on what else is in the stack.
const (
	headerMethod = "CSVHeader"
	writeMethod  = "WriteCSVTo"
	readMethod   = "ReadCSVFrom"
)

// What the contract's methods hand over and answer with, as they are written.
//
// A sequence is matched by its opening rather than whole, because what it
// yields is spelled for a person reading a table and not for the file this
// writes: the element in it is the subject's bare name, which is not always
// what this file would have to call it. What can be compared is that the walk
// hands over a sequence at all, and that a sink which answers answers with an
// error.
const (
	sequenceOpens = "iter.Seq["
	errorResult   = "error"
	countResult   = "int"
)

// stack is the document the declared type carries, described in the terms the
// two halves are written in.
//
// It is a description of the container rather than of the subject, and that is
// why it exists apart from the table: what the subject becomes is decided by
// its fields, and what the container becomes is decided by the methods the
// layers beneath this one turned out to expose.
type stack struct {
	// declared is the type the methods go on, and elem is how one of its
	// elements is spelled in the file being generated into.
	declared string
	elem     string

	// encode and decode name the two functions one row goes through, columns is
	// how many cells a record holds, and literal is the header as the source
	// that declares it.
	encode  string
	decode  string
	columns int
	literal string

	// blank names the one column whose emptiness would be written as a blank
	// line and read back as nothing, or is empty where the table cannot produce
	// one. [table.blank] is where the shape is described.
	blank string

	// text records that some cell holds text rather than a number, and so could
	// hold a line ending. It decides a sentence in the reader's documentation
	// and nothing else — see [table.text].
	text bool

	// pointer records that the walk is reached through a pointer, which decides
	// the receiver the writing half takes. It follows the walk rather than
	// being chosen: a container whose methods take a pointer is one whose
	// values are held by pointer, and a writer that took a copy of it would be
	// the one method in the file that did.
	pointer bool

	// writes and reads record which halves the stack can support. They are
	// separate because the two need different things — a walk to write, a sink
	// to read — and a container offering one and not the other should get the
	// half it can have rather than neither.
	writes bool
	reads  bool

	// refuses records that adding elements reports a refusal, which is what a
	// bounded container asked to say so rather than to drop its oldest element
	// does.
	refuses bool

	// bounded records that the container reports how much it can hold, and so
	// can be asked whether it can hold anything at all.
	//
	// It is what keeps a document from deciding whether a program stops. A
	// bounded container that was never constructed holds nothing and refuses
	// every element, and a reader that found that out one row in would run into
	// it for a document with rows and not for a document without — which is the
	// shape of failure nobody's tests catch.
	bounded bool

	// comma is the delimiter as a rune literal, and header records whether the
	// document opens with a row naming the columns.
	comma  string
	header bool

	// names are the identifiers the bodies bind, allocated out of the way of
	// what the file already binds. See [locals].
	names locals
}

// receiver returns how the container is written in the receiver of a method
// that only reads it.
func (s stack) receiver() string {
	if s.pointer {
		return "*" + s.declared
	}
	return s.declared
}

// binding returns what takes the result of adding the elements, which is
// nothing where adding them cannot fail.
func (s stack) binding(names locals) string {
	if s.refuses {
		return names.refused + " := "
	}
	return ""
}

// The names of the two counting streams a declaration brings with it.
//
// Named after the declaration and unexported, like every other helper one
// brings: they are plumbing for one type's methods rather than something a
// caller reaches.
func (s stack) writing() string { return plugin.Lower(s.declared) + "CSVWritten" }

func (s stack) reading() string { return plugin.Lower(s.declared) + "CSVRead" }

// codeNotTheContract reports a layer whose streaming methods are not the ones a
// document is written over.
//
// It is about the output rather than about the declaration: nothing the author
// wrote is wrong, and nothing they can write would fix it.
var codeNotTheContract = plugin.Register(6104,
	"a layer's streaming method is not the one a CSV document is written over")

// contractHint says what to do about one, which is not much: the layer beneath
// is forge's or somebody else's, and either way the declaration is innocent.
const contractHint = "this is a fault in the layer beneath rather than in the declaration; " +
	"report it with the declaration that produced it"

// streaming works out which halves of the document the stack beneath can carry.
//
// The shape beneath rather than the one the whole stack exposes, because a
// transport terminates a stack: what is under it is what it carries, and there
// is nothing above it whose methods could matter.
//
// A stack that offers no walk gets no writing half, and that is a decision
// rather than an omission. It is what a decorator that withdrew the walk asked
// for — a lock hands out no sequence, so nothing may be written that walks one
// — and the decorator owns whatever replaces it.
func streaming(ctx *plugin.Context, below plugin.Shape, of table) (stack, error) {
	out := stack{
		declared: ctx.Declared(),
		elem:     of.elem,
		encode:   of.encode,
		decode:   of.decode,
		columns:  len(of.columns),
		literal:  quotedNames(of.headings()),
	}

	if held, is := of.blank(); is {
		out.blank = held.name
	}
	out.text = of.text()

	if walk, walks := below.Method(walkMethod); walks {
		if err := walking(ctx, walk); err != nil {
			return stack{}, err
		}
		out.writes, out.pointer = true, walk.Pointer
	}

	if held, bounded := below.Method(capMethod); bounded {
		if err := measuring(ctx, held); err != nil {
			return stack{}, err
		}
		out.bounded = true
	}

	add, adds := below.Method(appendMethod)
	reset, resets := below.Method(resetMethod)
	if !adds || !resets {
		return out, nil
	}

	refuses, err := filling(ctx, add, reset)
	if err != nil {
		return stack{}, err
	}
	out.reads, out.refuses = true, refuses

	return out, nil
}

// walking checks the method a container is written out through.
func walking(ctx *plugin.Context, one plugin.Method) error {
	want := walkMethod + "() " + sequenceOpens + "E]"

	params, results, err := one.Rendered()
	if err != nil || len(params) != 0 || len(results) != 1 {
		return notTheContract(ctx, one, want)
	}
	if !strings.HasPrefix(results[0], sequenceOpens) {
		return notTheContract(ctx, one, want)
	}

	return nil
}

// measuring checks the method a container says how much it can hold through.
func measuring(ctx *plugin.Context, one plugin.Method) error {
	params, results, err := one.Rendered()
	if err != nil || len(params) != 0 || len(results) != 1 || results[0] != countResult {
		return notTheContract(ctx, one, capMethod+"() "+countResult)
	}

	return nil
}

// filling checks the two methods a container is read into through, and reports
// whether adding elements to it can be refused.
//
// Both are checked before either is used, so that a layer offering one of them
// in the wrong shape is reported rather than half-generated against.
func filling(ctx *plugin.Context, add, reset plugin.Method) (refuses bool, err error) {
	params, results, err := reset.Rendered()
	if err != nil || len(params) != 0 || len(results) != 0 {
		return false, notTheContract(ctx, reset, resetMethod+"()")
	}

	want := appendMethod + "(" + sequenceOpens + "E]), and an " + errorResult +
		" where it can refuse one"

	params, results, err = add.Rendered()
	switch {
	case err != nil, len(params) != 1, len(results) > 1:
		return false, notTheContract(ctx, add, want)
	case !strings.HasPrefix(params[0], sequenceOpens):
		return false, notTheContract(ctx, add, want)
	case len(results) == 1 && results[0] != errorResult:
		return false, notTheContract(ctx, add, want)
	}

	return len(results) == 1, nil
}

// notTheContract reports one such method against the declaration that ran into
// it.
//
// Against the declaration because that is the only position there is: a layer's
// surface is described in Go rather than written in it, so there is no file to
// point at. Naming the layer is what stands in for one.
func notTheContract(ctx *plugin.Context, one plugin.Method, want string) error {
	return plugin.New(codeNotTheContract, ctx.Model.Pos,
		"%s cannot be written as CSV: the %s layer offers %s%s, and a document is written over %s",
		ctx.Declared(), one.Owner.Name, one.Name, one.Signature, want).
		WithHint("%s", contractHint)
}
