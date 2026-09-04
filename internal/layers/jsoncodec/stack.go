package jsoncodec

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/okian/forge/plugin"
)

// codeNotTheContract reports a layer whose streaming methods are not the ones a
// codec over them is written against.
//
// A 4xxx, because it is found while deciding what to emit and is about the
// output rather than about the declaration: nothing the author wrote is wrong,
// and nothing they can write would fix it.
var codeNotTheContract = plugin.Register(4009,
	"a layer's streaming method is not the one a codec is written against")

// contractHint says what to do about one, which is not much: the layer is
// forge's or somebody else's, and either way the declaration is innocent.
const contractHint = "this is a fault in the layer beneath rather than in the declaration; " +
	"report it with the declaration that produced it"

// The methods of the streaming contract this layer writes a container's codec
// over, and the two it adds beside the codec's own entry points.
//
// Named here rather than at each use, because they are one contract: a layer
// that renames its walk stops being something a codec can be written over, and
// the place that says so should be the place that spells it.
const (
	walkMethod   = "All"
	appendMethod = "AppendSeq"
	resetMethod  = "Reset"
	capMethod    = "Cap"

	writeToMethod  = "WriteTo"
	readFromMethod = "ReadFrom"
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

// stack is the codec for the declared type, described in the terms the two
// halves are written in.
//
// It is a description of the container rather than of the subject, and that is
// the whole reason it exists apart from the plan: what the subject becomes is
// decided by its fields, and what the container becomes is decided by the
// methods the layers above this one turned out to expose.
type stack struct {
	// declared is the type the methods go on, and elem is how one of its
	// elements is spelled in the file being generated into. imports are what
	// that spelling binds, since a file naming a package it does not import
	// does not compile.
	declared string
	elem     string
	imports  []plugin.Import

	// subject is how one element is written and read: by the functions this
	// layer also writes where the subject is a struct, and through the codec
	// the subject declares for itself where it brought one.
	subject *form

	// names are the identifiers the container's codec binds, allocated out of
	// the way of the declared type and its element. See [locals].
	names locals

	// pointer records that the walk is reached through a pointer, which is what
	// decides the receiver the writing half takes. It follows the walk rather
	// than being chosen: a container whose methods take a pointer is one whose
	// values are held by pointer, and a codec that took a copy of it would be
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
	// It is what keeps a document from deciding whether a program panics. A
	// bounded container that was never constructed holds nothing and refuses
	// every element, and a reader that found that out one element in would run
	// into it for a document with elements and not for a document without —
	// which is the shape of failure nobody's tests catch.
	bounded bool
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
func (s stack) binding() string {
	if s.refuses {
		return "refused := "
	}
	return ""
}

// reading names the function both reading entry points share, which holds the
// body they differ from each other by one flag over.
//
// Named after the declaration and unexported, like every other helper a
// declaration brings with it: it is plumbing for one type's methods rather
// than something a caller reaches.
func (s stack) reading() string { return plugin.Around(false, "", s.declared, "read", "JSON") }

// unconstructed is the sentence a bounded container that was never constructed
// refuses every document with.
func (s stack) unconstructed() string {
	return s.declared + " holds nothing until it is constructed, so nothing can be read into it"
}

// stopped is the sentence a container that stopped taking elements leaves.
func (s stack) stopped() string {
	return s.declared + " stopped taking elements before the JSON array ended"
}

// streaming works out what codec the stack the declaration composed to can
// carry.
//
// It reads the shape the whole stack exposes rather than the one beneath this
// layer, because a codec for the container is written over methods this layer
// sits underneath. Nothing else in the layer looks up, and nothing else needs
// to: everything about the subject is decided by the subject.
//
// A stack that offers no walk gets no codec of its own, and that is a decision
// rather than an omission. It is what a decorator that withdrew the walk asked
// for — a lock hands out no sequence, so nothing may be written that walks one
// — and the decorator owns whatever replaces it.
func streaming(ctx *plugin.Context, of *form) (stack, error) {
	out := stack{declared: ctx.Declared(), elem: of.spelled.Text, imports: of.spelled.Imports, subject: of}
	out.names = naming(append(spelled(of), out.declared)...)

	switch of.how {
	case writtenStruct, writtenDelegate:
		// Either the codec this layer writes or the one the subject declared:
		// both are reachable from a container's methods.
	default:
		// The subject is a struct — the layer refuses a stack that is not
		// structured — so it is written as one or delegated to. Anything else
		// is this file having drifted from the one that decides forms. A
		// subject that is time.Time itself lands here too, and lands where it
		// landed before times were first-class: a subject has to be a type
		// methods can be attached to, and time.Time never was one.
		return stack{}, fmt.Errorf("json: %s is written in no form a container can call", of.spelled.Text)
	}

	if walk, walks := ctx.Exposed.Method(walkMethod); walks {
		if err := walking(ctx, walk); err != nil {
			return stack{}, err
		}
		out.writes, out.pointer = true, walk.Pointer
	}

	if held, bounded := ctx.Exposed.Method(capMethod); bounded {
		if err := measuring(ctx, held); err != nil {
			return stack{}, err
		}
		out.bounded = true
	}

	add, adds := ctx.Exposed.Method(appendMethod)
	reset, resets := ctx.Exposed.Method(resetMethod)
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
		"%s cannot be given a JSON codec: the %s layer offers %s%s, and a codec is written over %s",
		ctx.Declared(), one.Owner.Name, one.Name, one.Signature, want).
		WithHint("%s", contractHint)
}

// container writes the codec for the declared type, which is the whole stack as
// one JSON array.
func (w *writer) container(held stack) {
	if held.writes {
		w.containerAppend(held)
		w.containerMarshal(held)
		w.containerWriteTo(held)
	}
	if held.reads {
		w.containerReading(held)
		w.containerUnmarshal(held)
		w.containerReadFrom(held)
	}
}

// containerAppend writes the method that puts a container on the wire, which
// everything else in its writing half reaches.
func (w *writer) containerAppend(held stack) {
	c, dst := w.n("c"), w.n("dst")

	w.line("// %s appends the container to %s as a JSON array, and returns the", appendJSONMethod, dst)
	w.line("// extended buffer.")
	w.line("//")
	w.line("// One pass over the elements and straight into the buffer. No part of the")
	w.line("// document is assembled anywhere else first, so what it costs to write a")
	w.line("// container is what it costs to write its elements and nothing besides.")
	w.line("func (%s %s) %s(%s []byte) ([]byte, error) {",
		c, held.receiver(), appendJSONMethod, dst)
	if held.subject.how == writtenStruct || held.subject.writes == appendJSONMethod {
		w.line("var %s error", w.n("err"))
	}
	w.line("%s := len(%s)", w.n("open"), dst)
	w.line("for %s := range %s.%s() {", w.n("v"), c, walkMethod)
	w.line("%s = append(%s, ',')", dst, dst)
	w.appendValue(w.n("v"), held.subject, 0)
	w.line("}")
	w.line("if len(%s) == %s {", dst, w.n("open"))
	w.line("return append(%s, '[', ']'), nil", dst)
	w.line("}")
	w.line("%s[%s] = '['", dst, w.n("open"))
	w.line("return append(%s, ']'), nil", dst)
	w.line("}")
	w.blank()
}

// containerMarshal writes the method the standard library dispatches to.
func (w *writer) containerMarshal(held stack) {
	c := w.n("c")

	w.line("// %s writes the container as a compact JSON array.", marshalMethod)
	w.line("//")
	w.line("// The document is assembled in a borrowed buffer and copied out, so the")
	w.line("// cost over %s is one exactly-sized allocation — the slice being", appendJSONMethod)
	w.line("// returned.")
	w.line("func (%s %s) %s() ([]byte, error) {", c, held.receiver(), marshalMethod)
	w.line("%s := jsonTakeScratch()", w.n("scratch"))
	w.line("%s, %s := %s.%s((*%s)[:0])", w.n("held"), w.n("err"), c, appendJSONMethod, w.n("scratch"))
	w.line("return jsonFinish(%s, %s, %s)", w.n("scratch"), w.n("held"), w.n("err"))
	w.line("}")
	w.blank()
}

// containerWriteTo writes the method that sends a container to a writer.
func (w *writer) containerWriteTo(held stack) {
	c, into := w.n("c"), w.n("w")
	dst := w.n("dst")

	w.line("// %s writes the container to %s as a JSON array, and reports how many bytes", writeToMethod, into)
	w.line("// reached %s.", into)
	w.line("//")
	w.line("// The document is assembled in a borrowed window and handed over a flush at")
	w.line("// a time, so a container of any size costs one window and no more. What is")
	w.line("// reported is what %s accepted rather than what was composed, which is the", into)
	w.line("// count a caller copying from the container is entitled to. The document")
	w.line("// ends with a newline, which is what a writer of a stream of them leaves")
	w.line("// between two.")
	w.line("func (%s %s) %s(%s io.Writer) (int64, error) {",
		c, held.receiver(), writeToMethod, into)
	w.line("%s := jsonTakeScratch()", w.n("scratch"))
	w.line("%s := append((*%s)[:0], '[')", dst, w.n("scratch"))
	w.line("var (")
	w.line("%s int64", w.n("counted"))
	w.line("%s  error", w.n("failed"))
	w.line(")")
	w.writeToLoop(held)
	w.line("if %s != nil {", w.n("failed"))
	w.line("*%s = %s", w.n("scratch"), dst)
	w.line("jsonDropScratch(%s)", w.n("scratch"))
	w.line("return %s, %s", w.n("counted"), w.n("failed"))
	w.line("}")
	w.line("%s = append(%s, ']', '\\n')", dst, dst)
	w.line("%s, %s := %s.Write(%s)", w.n("n"), w.n("err"), into, dst)
	w.line("%s += int64(%s)", w.n("counted"), w.n("n"))
	w.line("*%s = %s", w.n("scratch"), dst)
	w.line("jsonDropScratch(%s)", w.n("scratch"))
	w.line("return %s, %s", w.n("counted"), w.n("err"))
	w.line("}")
	w.blank()
}

// writeToLoop writes the walk that fills the window and flushes it at the
// threshold, recording rather than returning a failure: the window has to go
// back and the count has to be reported whatever went wrong.
func (w *writer) writeToLoop(held stack) {
	dst, first := w.n("dst"), w.n("first")

	w.line("%s := true", first)
	w.line("for %s := range %s.%s() {", w.n("v"), w.n("c"), walkMethod)
	w.line("if !%s {", first)
	w.line("%s = append(%s, ',')", dst, dst)
	w.line("}")
	w.line("%s = false", first)
	w.windowedElement(held)
	w.line("if len(%s) < jsonFlushWindow {", dst)
	w.line("continue")
	w.line("}")
	w.line("%s, %s := %s.Write(%s)", w.n("n"), w.n("err"), w.n("w"), dst)
	w.line("%s += int64(%s)", w.n("counted"), w.n("n"))
	w.line("if %s != nil {", w.n("err"))
	w.line("%s = %s", w.n("failed"), w.n("err"))
	w.line("break")
	w.line("}")
	w.line("%s = %s[:0]", dst, dst)
	w.line("}")
}

// windowedElement writes one element into the window, recording rather than
// returning a failure: the window has to be handed back and the count reported
// whatever went wrong.
func (w *writer) windowedElement(held stack) {
	of := held.subject
	dst, v, failed := w.n("dst"), w.n("v"), w.n("failed")

	switch {
	case of.how == writtenStruct:
		w.line("if %s, %s = %s(%s, %s); %s != nil {", dst, failed, encoderFor(of.typ), dst, v, failed)
		w.line("break")
		w.line("}")

	case of.writes == appendJSONMethod:
		w.line("if %s, %s = %s.%s(%s); %s != nil {", dst, failed, v, appendJSONMethod, dst, failed)
		w.line("break")
		w.line("}")

	default:
		// A subject whose codec speaks another interface is reached through
		// the standard library, which knows how to call it and validates what
		// it answers. Deterministically, because everything this codec writes
		// is.
		w.line("%s, %s := json.Marshal(%s, json.Deterministic(true))", w.n("spliced"), w.n("bad"), v)
		w.line("if %s != nil {", w.n("bad"))
		w.line("%s = %s", failed, w.n("bad"))
		w.line("break")
		w.line("}")
		w.line("%s = append(%s, %s...)", dst, dst, w.n("spliced"))
	}
}

// containerReading writes the function both reading entry points share.
func (w *writer) containerReading(held stack) {
	name := held.reading()
	c, i := w.n("c"), w.n("i")

	w.line("// %s reads a JSON array into the container, borrowing or copying as", name)
	w.line("// the flag says.")
	w.line("//")
	w.line("// What the container held is dropped first, so reading into one twice leaves")
	w.line("// the second document rather than both — which is what reading a document")
	w.line("// into a value means everywhere else. A JSON null empties it and reads")
	w.line("// nothing else: a container is empty or holds elements, so null and an empty")
	w.line("// array are read alike and both are written back as an empty array.")
	w.line("//")
	w.line("// The elements are handed over one at a time as they are read, so the")
	w.line("// document is never assembled anywhere beside the container being filled")
	w.line("// from it.")
	w.line("func %s(%s *%s, %s []byte, %s bool) error {", name, c, held.declared, w.n("data"), w.n("borrow"))

	w.containerRoom(held)

	w.line("%s := jsonSkipSpace(%s, 0)", i, w.n("data"))
	w.line("if %s, %s := jsonScanNull(%s, %s); %s {", w.n("next"), w.n("ok"), w.n("data"), i, w.n("ok"))
	w.line("if %s := jsonAtEnd(%s, %s); %s != nil {", w.n("err"), w.n("data"), w.n("next"), w.n("err"))
	w.line("return %s", w.n("err"))
	w.line("}")
	w.line("%s.%s()", c, resetMethod)
	w.line("return nil")
	w.line("}")

	w.line("if %s >= len(%s) || %s[%s] != '[' {", i, w.n("data"), w.n("data"), i)
	w.line("return jsonCannotRead(%s, %s, %s)", strconv.Quote(held.declared), w.n("data"), i)
	w.line("}")
	w.line("%s++", i)
	w.line("%s.%s()", c, resetMethod)
	w.blank()

	w.readingElements(held)
	w.readingEpilogue(held)
	w.line("}")
	w.blank()
}

// readingEpilogue writes what happens once the elements have been handed over:
// the failures in the order worth reporting them, the container that stopped
// early, and the check that the document was one array and nothing more.
func (w *writer) readingEpilogue(held stack) {
	i := w.n("i")

	w.containerEnd(held)
	w.line("if !%s {", w.n("ended"))
	w.line("%s, %s, %s := jsonElementNext(%s, %s, %s)", w.n("next"), w.n("done"), w.n("err"), w.n("data"), i, w.n("first"))
	w.line("if %s != nil {", w.n("err"))
	w.line("return %s", w.n("err"))
	w.line("}")
	w.line("if !%s {", w.n("done"))
	w.line("return errors.New(%s)", strconv.Quote(held.stopped()))
	w.line("}")
	w.line("%s = %s", i, w.n("next"))
	w.line("}")
	w.line("return jsonAtEnd(%s, %s)", w.n("data"), i)
}

// readingElements writes the sequence the elements reach the container
// through, decoding each from the document in place.
//
// A sequence rather than a call per element, because a sequence is what the
// contract's sink takes — and it is what lets the container decide how to take
// them: a bounded one drops or refuses, and neither answer is the reader's to
// make.
func (w *writer) readingElements(held stack) {
	i, first := w.n("i"), w.n("first")
	failed, ended := w.n("failed"), w.n("ended")

	w.line("var %s error", failed)
	w.line("%s := true", first)
	w.line("%s := false", ended)
	w.line("%s%s.%s(func(%s func(%s) bool) {", held.binding(), w.n("c"), appendMethod, w.n("yield"), held.elem)
	w.line("for {")
	w.line("%s, %s, %s := jsonElementNext(%s, %s, %s)", w.n("next"), w.n("done"), w.n("err"), w.n("data"), i, first)
	w.line("if %s != nil {", w.n("err"))
	w.line("%s = %s", failed, w.n("err"))
	w.line("return")
	w.line("}")
	w.line("if %s {", w.n("done"))
	w.line("%s, %s = %s, true", i, ended, w.n("next"))
	w.line("return")
	w.line("}")
	w.line("%s = false", first)
	w.line("%s = %s", i, w.n("next"))
	w.line("var %s %s", w.n("v"), held.elem)
	w.spannedElement(held)
	w.line("if !%s(%s) {", w.n("yield"), w.n("v"))
	w.line("return")
	w.line("}")
	w.line("}")
	w.line("})")
	w.blank()
}

// spannedElement writes the statements that read one element out of data into
// the loop's value, recording a failure rather than returning it: the sequence
// the elements travel through answers with nothing.
func (w *writer) spannedElement(held stack) {
	of := held.subject
	i, v, failed := w.n("i"), w.n("v"), w.n("failed")

	if of.how == writtenStruct {
		w.line("%s, %s := %s(%s, %s, 1, &%s, %s)", w.n("n"), w.n("err"), decoderFor(of.typ), w.n("data"), i, v, w.n("borrow"))
		w.line("if %s != nil {", w.n("err"))
		w.line("%s = %s", failed, w.n("err"))
		w.line("return")
		w.line("}")
		w.line("%s = %s", i, w.n("n"))
		return
	}

	w.line("%s := %s", w.n("start"), i)
	w.line("%s, %s := jsonSkipValue(%s, %s, 1)", w.n("n"), w.n("err"), w.n("data"), i)
	w.line("if %s != nil {", w.n("err"))
	w.line("%s = %s", failed, w.n("err"))
	w.line("return")
	w.line("}")

	switch {
	case of.reads != unmarshalMethod:
		w.line("if %s := json.Unmarshal(%s[%s:%s], &%s); %s != nil {", w.n("err"), w.n("data"), w.n("start"), w.n("n"), v, w.n("err"))
		w.line("%s = %s", failed, w.n("err"))
		w.line("return")
		w.line("}")

	case of.borrows:
		w.line("%s := %s.%s", w.n("read"), v, unmarshalMethod)
		w.line("if %s {", w.n("borrow"))
		w.line("%s = %s.%s", w.n("read"), v, borrowedMethod)
		w.line("}")
		w.line("if %s := %s(%s[%s:%s]); %s != nil {", w.n("err"), w.n("read"), w.n("data"), w.n("start"), w.n("n"), w.n("err"))
		w.line("%s = %s", failed, w.n("err"))
		w.line("return")
		w.line("}")

	default:
		w.line("if %s := %s.%s(%s[%s:%s]); %s != nil {", w.n("err"), v, unmarshalMethod, w.n("data"), w.n("start"), w.n("n"), w.n("err"))
		w.line("%s = %s", failed, w.n("err"))
		w.line("return")
		w.line("}")
	}
	w.line("%s = %s", i, w.n("n"))
}

// containerUnmarshal writes the two reading entry points, which differ by one
// flag over the shared body.
func (w *writer) containerUnmarshal(held stack) {
	name := held.reading()
	c := w.n("c")

	w.line("// %s reads one JSON array into the container. Everything read out", unmarshalMethod)
	w.line("// of data is copied, so data is the caller's again the moment this returns.")
	w.line("func (%s *%s) %s(%s []byte) error {", c, held.declared, unmarshalMethod, w.n("data"))
	w.line("return %s(%s, %s, false)", name, c, w.n("data"))
	w.line("}")
	w.blank()

	w.line("// %s reads one JSON array into the container, with the elements'", borrowedMethod)
	w.line("// strings pointing into data rather than copied out of it. It is the")
	w.line("// quickest way in and the sharpest: data must outlive the container and")
	w.line("// must not be modified, or the elements change underneath it. Where that")
	w.line("// cannot be promised, %s copies.", unmarshalMethod)
	w.line("func (%s *%s) %s(%s []byte) error {", c, held.declared, borrowedMethod, w.n("data"))
	w.line("return %s(%s, %s, true)", name, c, w.n("data"))
	w.line("}")
	w.blank()
}

// containerRoom writes the check that a bounded container can hold anything at
// all, which is what keeps the document from deciding whether the program
// stops.
//
// Asked before a byte is read, so the answer does not depend on what arrived.
// A container that was never constructed refuses an empty document exactly as
// it refuses a full one, which is the difference between a mistake somebody's
// tests find and one their users do.
func (w *writer) containerRoom(held stack) {
	if !held.bounded {
		return
	}

	w.line("if %s.%s() == 0 {", w.n("c"), capMethod)
	w.line("return errors.New(%s)", strconv.Quote(held.unconstructed()))
	w.line("}")
}

// containerEnd writes the two ways handing the elements over can have gone
// wrong: the reading failure first, because a container that refused an
// element refused it because the element arrived, and the reason it arrived
// wrongly is the one worth reporting.
func (w *writer) containerEnd(held stack) {
	w.line("if %s != nil {", w.n("failed"))
	w.line("return %s", w.n("failed"))
	w.line("}")
	if held.refuses {
		w.line("if %s != nil {", w.n("refused"))
		w.line("return %s", w.n("refused"))
		w.line("}")
	}
}

// containerReadFrom writes the method that fills a container from a reader.
func (w *writer) containerReadFrom(held stack) {
	c, r := w.n("c"), w.n("r")

	w.line("// %s reads one JSON array from %s into the container, and reports how many", readFromMethod, r)
	w.line("// bytes that array took.")
	w.line("//")
	w.line("// One array rather than everything %s holds, because a JSON document ends", r)
	w.line("// where it ends. The window is refilled in blocks, so bytes following the")
	w.line("// array may already have been taken out of %s and are dropped with it: this", r)
	w.line("// reads a reader holding one document. What it holds at a time is bounded")
	w.line("// by the largest single element rather than by the document, which is what")
	w.line("// a streaming reader is for. A reader holding nothing reports io.EOF.")
	w.line("func (%s *%s) %s(%s io.Reader) (int64, error) {",
		c, held.declared, readFromMethod, r)

	if held.bounded {
		w.line("if %s.%s() == 0 {", c, capMethod)
		w.line("return 0, errors.New(%s)", strconv.Quote(held.unconstructed()))
		w.line("}")
	}

	w.readFromPrologue(held)
	w.readFromElements(held)
	w.readFromEpilogue(held)
	w.line("}")
	w.blank()
}

// readFromPrologue writes what a streaming read decides before the elements:
// the null that means the container empties, and the wrong kind refused by
// name.
func (w *writer) readFromPrologue(held stack) {
	c, feed := w.n("c"), w.n("feed")
	kind, err := w.n("kind"), w.n("err")

	w.line("%s := jsonNewFeed(%s)", feed, w.n("r"))
	w.line("defer %s.close()", feed)

	w.line("%s, %s := %s.peek()", kind, err, feed)
	w.line("if %s != nil {", err)
	w.line("return %s.offset(), %s", feed, err)
	w.line("}")
	w.line("if %s == 'n' {", kind)
	w.line("%s, %s := %s.null()", w.n("ok"), err, feed)
	w.line("if %s != nil {", err)
	w.line("return %s.offset(), %s", feed, err)
	w.line("}")
	w.line("if %s {", w.n("ok"))
	w.line("%s.%s()", c, resetMethod)
	w.line("return %s.offset(), nil", feed)
	w.line("}")
	w.line("}")
	w.line("if %s != '[' {", kind)
	w.line("return %s.offset(), %s.cannotRead(%s)", feed, feed, strconv.Quote(held.declared))
	w.line("}")
	w.line("%s.take()", feed)
	w.line("%s.%s()", c, resetMethod)
	w.blank()
}

// readFromElements writes the sequence the elements travel through, each
// decoded from the feed's window as it completes.
func (w *writer) readFromElements(held stack) {
	feed, first := w.n("feed"), w.n("first")
	failed, ended := w.n("failed"), w.n("ended")

	w.line("var %s error", failed)
	w.line("%s := true", first)
	w.line("%s := false", ended)
	w.line("%s%s.%s(func(%s func(%s) bool) {", held.binding(), w.n("c"), appendMethod, w.n("yield"), held.elem)
	w.line("for {")
	w.line("%s, %s := %s.peek()", w.n("kind"), w.n("err"), feed)
	w.line("if %s != nil {", w.n("err"))
	w.line("%s = %s", failed, w.n("err"))
	w.line("return")
	w.line("}")
	w.line("switch {")
	w.line("case %s == ']':", w.n("kind"))
	w.line("%s.take()", feed)
	w.line("%s = true", ended)
	w.line("return")
	w.line("case %s:", first)
	w.line("case %s != ',':", w.n("kind"))
	w.line("%s = errJSONSyntax", failed)
	w.line("return")
	w.line("default:")
	w.line("%s.take()", feed)
	w.line("}")
	w.line("%s = false", first)
	w.line("%s, %s := %s.element()", w.n("held"), w.n("err2"), feed)
	w.line("if %s != nil {", w.n("err2"))
	w.line("%s = %s", failed, w.n("err2"))
	w.line("return")
	w.line("}")
	w.line("var %s %s", w.n("v"), held.elem)
	w.fedElement(held)
	w.line("if !%s(%s) {", w.n("yield"), w.n("v"))
	w.line("return")
	w.line("}")
	w.line("}")
	w.line("})")
	w.blank()
}

// readFromEpilogue writes what happens once the elements have been handed
// over: the failures in the order worth reporting them, the container that
// stopped early, and the bracket that closes the document.
func (w *writer) readFromEpilogue(held stack) {
	feed := w.n("feed")

	w.line("if %s != nil {", w.n("failed"))
	w.line("return %s.offset(), %s", feed, w.n("failed"))
	w.line("}")
	if held.refuses {
		w.line("if %s != nil {", w.n("refused"))
		w.line("return %s.offset(), %s", feed, w.n("refused"))
		w.line("}")
	}
	w.line("if !%s {", w.n("ended"))
	w.line("%s, %s := %s.peek()", w.n("kind"), w.n("err"), feed)
	w.line("if %s != nil {", w.n("err"))
	w.line("return %s.offset(), %s", feed, w.n("err"))
	w.line("}")
	w.line("if %s != ']' {", w.n("kind"))
	w.line("return %s.offset(), errors.New(%s)", feed, strconv.Quote(held.stopped()))
	w.line("}")
	w.line("%s.take()", feed)
	w.line("}")
	w.line("return %s.offset(), nil", feed)
}

// fedElement writes the statements that read one element out of the feed's
// window into the loop's value.
//
// Never borrowing, because the window is recycled underneath every element:
// the bytes an element was read from are gone by the time the next one
// arrives, which is the price of not holding the document.
func (w *writer) fedElement(of stack) {
	subject := of.subject
	v, span, failed := w.n("v"), w.n("held"), w.n("failed")

	if subject.how == writtenStruct {
		w.line("%s, %s := %s(%s, 0, 1, &%s, false)", w.n("n"), w.n("err3"), decoderFor(subject.typ), span, v)
		w.line("if %s != nil {", w.n("err3"))
		w.line("%s = %s", failed, w.n("err3"))
		w.line("return")
		w.line("}")
		w.line("if %s := jsonAtEnd(%s, %s); %s != nil {", w.n("err3"), span, w.n("n"), w.n("err3"))
		w.line("%s = %s", failed, w.n("err3"))
		w.line("return")
		w.line("}")
		return
	}

	if subject.reads != unmarshalMethod {
		w.line("if %s := json.Unmarshal(%s, &%s); %s != nil {", w.n("err3"), span, v, w.n("err3"))
		w.line("%s = %s", failed, w.n("err3"))
		w.line("return")
		w.line("}")
		return
	}

	w.line("if %s := %s.%s(%s); %s != nil {", w.n("err3"), v, unmarshalMethod, span, w.n("err3"))
	w.line("%s = %s", failed, w.n("err3"))
	w.line("return")
	w.line("}")
}
