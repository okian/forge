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
// over, and the two it adds beside the codec's own pair.
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

	// encodes and decodes are the calls that write and read one element, each
	// with one verb left for the value's own name. A subject whose codec this
	// layer writes is reached through the function beside it; one that declares
	// a codec of its own is reached through its own method, which is what makes
	// a hand-written codec authoritative here as well as inside a struct.
	encodes string
	decodes string

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

	// names are the identifiers the bodies bind. Held here because the calls
	// above are written with two of them already in place, so the writer has to
	// be given the same set rather than allocating its own. See [locals].
	names locals

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

// counting names the writer that counts what this container's WriteTo wrote.
//
// Named after the declaration and unexported, like every other helper a
// declaration brings with it: it is plumbing for one type's method rather than
// something a caller reaches.
func (s stack) counting() string { return plugin.Around(false, "", s.declared, "counting") }

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
	out := stack{
		declared: ctx.Declared(),
		elem:     of.spelled.Text,
		imports:  of.spelled.Imports,
		names:    naming(ctx.Declared(), of.spelled.Text),
	}

	switch of.how {
	case writtenStruct:
		out.encodes = encoderFor(of.typ) + "(" + out.names.encoder + ", %s)"
		out.decodes = decoderFor(of.typ) + "(" + out.names.decoder + ", &%s)"

	case writtenDelegate:
		out.encodes = "%s." + marshalMethod + "(" + out.names.encoder + ")"
		out.decodes = "%s." + unmarshalMethod + "(" + out.names.decoder + ")"

	default:
		// The subject is a struct — the layer refuses a stack that is not
		// structured — so it is written as one or delegated to. Anything else
		// is this file having drifted from the one that decides forms.
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
		w.containerMarshal(held)
		w.containerCounter(held)
		w.containerWriteTo(held)
	}
	if held.reads {
		w.containerUnmarshal(held)
		w.containerReadFrom(held)
	}
}

// containerMarshal writes the method that puts a container on the wire.
func (w *writer) containerMarshal(held stack) {
	w.line("// %s writes the container as a JSON array.", marshalMethod)
	w.line("//")
	w.line("// One pass over the elements and straight into the encoder. No part of the")
	w.line("// document is assembled anywhere else first, so what it costs to write a")
	w.line("// container is what it costs to write its elements and nothing besides.")
	w.line("func (%s %s) %s(%s *jsontext.Encoder) error {",
		w.names.receiver, held.receiver(), marshalMethod, w.names.encoder)
	w.checked("%s.WriteToken(jsontext.BeginArray)", w.names.encoder)
	w.line("for %s := range %s.%s() {", w.names.value, w.names.receiver, walkMethod)
	w.checked(held.encodes, w.names.value)
	w.line("}")
	w.line("return %s.WriteToken(jsontext.EndArray)", w.names.encoder)
	w.line("}")
	w.blank()
}

// containerCounter writes the writer that counts what reached the caller's.
//
// The encoder buffers, so what it has been given and what has reached the
// writer are two numbers, and io.WriterTo is a contract about the second: a
// write that fails part way through has to report what got out rather than what
// was composed. The encoder offers only the first, so the second is counted
// here.
func (w *writer) containerCounter(held stack) {
	name := held.counting()

	w.line("// %s counts the bytes a writer accepted.", name)
	w.line("//")
	w.line("// The encoder buffers, so what it has written and what the writer beneath it")
	w.line("// has taken are two numbers. This is the second, which is the one a caller")
	w.line("// copying from the container is entitled to.")
	w.line("type %s struct {", name)
	w.line("to io.Writer")
	w.line("n  int64")
	w.line("}")
	w.blank()

	w.line("// Write passes the bytes on and counts the ones that were taken.")
	w.line("func (w *%s) Write(p []byte) (int, error) {", name)
	w.line("n, err := w.to.Write(p)")
	w.line("w.n += int64(n)")
	w.line("return n, err")
	w.line("}")
	w.blank()
}

// containerWriteTo writes the method that sends a container to a writer.
func (w *writer) containerWriteTo(held stack) {
	w.line("// %s writes the container to w as a JSON array, and reports how many bytes", writeToMethod)
	w.line("// reached w.")
	w.line("//")
	w.line("// Straight to w rather than into a buffer this then copies, so a container of")
	w.line("// any size costs the encoder's own buffer and no more. The document ends with")
	w.line("// a newline, which is what an encoder leaves between the values of a stream.")
	w.line("func (%s %s) %s(w io.Writer) (int64, error) {",
		w.names.receiver, held.receiver(), writeToMethod)
	w.line("counted := %s{to: w}", held.counting())

	// Without the encoder's duplicate-name bookkeeping, which is a quarter of
	// what writing a document costs and is checking something already known.
	// An encoder tracks every name it writes in an object so it can refuse a
	// repeated one, and to compare them it unquotes each one back out again.
	// The names here are the ones this codec was generated from: a subject with
	// two members under one JSON name is refused when the codec is written, and
	// one whose name is decided twice — a tag against a field, an embedded
	// member against an outer one — is settled there too, so exactly one member
	// carries it. There is nothing left for the encoder to catch.
	//
	// Only where this builds the encoder. A caller who brings their own keeps
	// whatever they set on it, which is theirs to decide.
	//
	// The reading half is the opposite case and keeps the check: the names in a
	// document arrive from outside, so refusing a repeated one is the encoder
	// protecting a caller from their input rather than from this codec.
	w.line("%s := jsontext.NewEncoder(&counted, jsontext.AllowDuplicateNames(true))", w.names.encoder)
	w.line("if err := %s.%s(%s); err != nil {", w.names.receiver, marshalMethod, w.names.encoder)
	w.line("return counted.n, err")
	w.line("}")
	w.line("return counted.n, nil")
	w.line("}")
	w.blank()
}

// containerUnmarshal writes the method that reads a container back off the
// wire.
func (w *writer) containerUnmarshal(held stack) {
	w.line("// %s reads a JSON array into the container.", unmarshalMethod)
	w.line("//")
	w.line("// What the container held is dropped first, so reading into one twice leaves")
	w.line("// the second document rather than both — which is what reading a document")
	w.line("// into a value means everywhere else. A JSON null empties it and reads")
	w.line("// nothing else: a container is empty or holds elements, so null and an empty")
	w.line("// array are read alike and both are written back as an empty array.")
	w.line("//")
	w.line("// The elements are handed over one at a time as they are read, so the")
	w.line("// document is never held in memory beside the container being filled from")
	w.line("// it.")
	w.line("func (%s *%s) %s(%s *jsontext.Decoder) error {",
		w.names.receiver, held.declared, unmarshalMethod, w.names.decoder)

	w.containerRoom(held)

	w.line("if %s.PeekKind() == 'n' {", w.names.decoder)
	w.line("if _, err := %s.ReadToken(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
	w.line("%s.%s()", w.names.receiver, resetMethod)
	w.line("return nil")
	w.line("}")

	// Checked rather than assumed, exactly as an object's reader checks for a
	// brace: reading elements out of something that is not an array would fill
	// the container from whatever the tokens happened to be.
	w.line("if kind := %s.PeekKind(); kind != '[' {", w.names.decoder)
	w.line("// A decoder that failed reports no kind at all, and reading is what")
	w.line("// says why; a document of the wrong shape reads fine and is wrong.")
	w.line("if _, err := %s.ReadToken(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
	w.line("return fmt.Errorf(%s, kind)",
		strconv.Quote("cannot read "+held.declared+" from a JSON %s"))
	w.line("}")
	w.line("if _, err := %s.ReadToken(); err != nil {", w.names.decoder)
	w.line("return err")
	w.line("}")
	w.line("%s.%s()", w.names.receiver, resetMethod)
	w.blank()

	w.containerElements(held)
	w.containerEnd(held)

	w.line("}")
	w.blank()
}

// containerRoom writes the check that a bounded container can hold anything at
// all, which is what keeps the document from deciding whether the program
// stops.
//
// Asked before a token is read, so the answer does not depend on what arrived.
// A container that was never constructed refuses an empty document exactly as
// it refuses a full one, which is the difference between a mistake somebody's
// tests find and one their users do.
func (w *writer) containerRoom(held stack) {
	if !held.bounded {
		return
	}

	w.line("if %s.%s() == 0 {", w.names.receiver, capMethod)
	w.line("return errors.New(%s)", strconv.Quote(held.declared+
		" holds nothing until it is constructed, so nothing can be read into it"))
	w.line("}")
}

// containerElements writes the sequence the elements reach the container
// through.
//
// A sequence rather than a call per element, because a sequence is what the
// contract's sink takes — and it is what lets the container decide how to take
// them: a bounded one drops or refuses, and neither answer is the reader's to
// make.
func (w *writer) containerElements(held stack) {
	w.line("var failed error")
	w.line("%s%s.%s(func(yield func(%s) bool) {", held.binding(), w.names.receiver, appendMethod, held.elem)
	w.line("for %s.PeekKind() != ']' {", w.names.decoder)
	w.line("var %s %s", w.names.value, held.elem)
	w.line("if err := "+held.decodes+"; err != nil {", w.names.value)
	w.line("failed = err")
	w.line("return")
	w.line("}")
	w.line("if !yield(%s) {", w.names.value)
	w.line("return")
	w.line("}")
	w.line("}")
	w.line("})")
	w.blank()
}

// containerEnd writes what happens once the elements have been handed over: the
// two ways it can have gone wrong, and the token that closes the array.
func (w *writer) containerEnd(held stack) {
	// The reading failure first: a container that refused an element refused it
	// because the element arrived, and the reason it arrived wrongly is the one
	// worth reporting.
	w.line("if failed != nil {")
	w.line("return failed")
	w.line("}")
	if held.refuses {
		w.line("if refused != nil {")
		w.line("return refused")
		w.line("}")
	}

	// A container that stopped taking elements without saying so leaves the
	// document half-read and the value short of it, and neither is visible from
	// anywhere else: the tokens left behind would be read as though they came
	// after the array. A decoder that failed reports no kind at all, and the
	// read below is what says why.
	w.line("if kind := %s.PeekKind(); kind != ']' && kind != 0 {", w.names.decoder)
	w.line("return errors.New(%s)",
		strconv.Quote(held.declared+" stopped taking elements before the JSON array ended"))
	w.line("}")
	w.line("_, err := %s.ReadToken()", w.names.decoder)
	w.line("return err")
}

// containerReadFrom writes the method that fills a container from a reader.
func (w *writer) containerReadFrom(held stack) {
	w.line("// %s reads one JSON array from r into the container, and reports how many", readFromMethod)
	w.line("// bytes that array took.")
	w.line("//")
	w.line("// One array rather than everything r holds, because a JSON document ends")
	w.line("// where it ends. The decoder reads r in blocks, so bytes following the array")
	w.line("// may already have been taken out of r and are dropped with it: this reads a")
	w.line("// reader holding one document, and a stream of them is read through a decoder")
	w.line("// that outlives each. A reader holding nothing reports io.EOF.")
	w.line("func (%s *%s) %s(r io.Reader) (int64, error) {",
		w.names.receiver, held.declared, readFromMethod)
	w.line("%s := jsontext.NewDecoder(r)", w.names.decoder)
	w.line("if err := %s.%s(%s); err != nil {", w.names.receiver, unmarshalMethod, w.names.decoder)
	w.line("return %s.InputOffset(), err", w.names.decoder)
	w.line("}")
	w.line("return %s.InputOffset(), nil", w.names.decoder)
	w.line("}")
	w.blank()
}
