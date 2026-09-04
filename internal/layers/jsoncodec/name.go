package jsoncodec

import (
	"go/types"
	"strconv"
	"strings"

	"github.com/okian/forge/plugin"
)

// jsonKey is the struct tag key the wire names are read from.
const jsonKey = "json"

// wireName returns the name a field is written under, and whether the field is
// written at all.
//
// The tag wins wherever there is one, because the tag is what the rest of the
// ecosystem reads: a field named differently by forge than by encoding/json is
// a field that goes out under one name and is looked for under another, and
// nothing downstream recovers from that. Where there is no tag the declaration
// option decides, and its default leaves the field's own name alone — which is
// what encoding/json/v2 does with an untagged field.
//
// An unexported field is not written. Generated code could read one from inside
// the package, but the moment the same subject is reached from anywhere else it
// could not, and a codec whose output depended on where it was generated is
// worse than one that leaves the field out everywhere.
func wireName(field plugin.Field, style string) (string, bool) {
	if !field.Exported {
		return "", false
	}

	if tag, ok := field.Tag(jsonKey); ok {
		if tag.Ignored {
			return "", false
		}
		if tag.Name != "" {
			return tag.Name, true
		}
	}

	return restyled(field.Name, style), true
}

// Styles a field's own name may be written in when no tag renames it.
const (
	styleAsIs  = "asis"
	styleSnake = "snake"
	styleCamel = "camel"
)

// restyled writes a Go field name in the style the declaration asked for.
func restyled(name, style string) string {
	switch style {
	case styleSnake:
		return snake(name)
	case styleCamel:
		return plugin.Camel(name)
	default:
		return name
	}
}

// snake writes a Go name in lower_snake_case.
//
// The boundaries are [plugin.Words]', which are the ones a reader would draw
// rather than the ones a naive scan finds: a run of capitals is one word, so
// UserID is user_id rather than user_i_d, and the last capital of such a run
// starts the next word when a lower-case letter follows, so JSONValue is
// json_value rather than jsonv_alue. A digit continues whatever word it is in,
// and an initialism made plural keeps its s, so UserIDs is user_ids.
//
// Shared rather than derived here, because a codec deciding where a word ends
// and an enumeration deciding it somewhere else is one library with two
// opinions about a name — and the name in question is one that goes out on a
// wire, where the two disagreeing is a document member spelled two ways.
func snake(name string) string {
	return strings.ToLower(strings.Join(plugin.Words(name), "_"))
}

// identifier returns the name a type's pair of codec functions is built from.
//
// Two types never share one, which is the whole requirement: the name is what
// the functions are declared as, and two types given one name is a package that
// does not compile. Composites spell out their parts for that reason — an
// array's length included, since [2]int and [3]int are different types — and a
// named type is spelled with its package, since two packages may each declare a
// Person.
//
// It reads as something a person would have written, because it appears in the
// output. A caller who wants to know what encodeDomainPersonJSONTo encodes
// should not have to look it up.
func identifier(t types.Type) string {
	switch held := types.Unalias(t).(type) {
	case *types.Basic:
		return plugin.Upper(held.Name())

	case *types.Named:
		return qualified(held)

	case *types.Pointer:
		return "PointerTo" + identifier(held.Elem())

	case *types.Slice:
		return "SliceOf" + identifier(held.Elem())

	case *types.Array:
		return "Array" + strconv.FormatInt(held.Len(), 10) + "Of" + identifier(held.Elem())

	case *types.Map:
		return "MapOf" + identifier(held.Key()) + "To" + identifier(held.Elem())

	default:
		// Anything reaching here is a type this layer refused to write a codec
		// for, and a refusal names the field rather than the function that was
		// never written. A spelling is still owed, because the refusal is
		// reported after the plan is built and the plan holds a name for
		// everything in it.
		return "Unwritable"
	}
}

// qualified names a defined type by its package and its own name.
//
// The package's name rather than its path: a path holds slashes and dots and
// would have to be folded into an identifier anyway, and the fold is what
// introduces collisions. Two packages of one name in one codec is possible and
// rare, and it produces a redeclaration in the output rather than a silently
// wrong encoding.
func qualified(named *types.Named) string {
	ref := plugin.RefOf(named)

	out := plugin.Upper(ref.Name)
	if obj := named.Obj(); obj != nil && obj.Pkg() != nil {
		out = plugin.Upper(obj.Pkg().Name()) + out
	}
	return out + arguments(named)
}

// arguments folds a generic type's arguments into its name, so that two
// instantiations of one generic do not reach one function.
func arguments(named *types.Named) string {
	args := named.TypeArgs()
	if args == nil {
		return ""
	}

	var out strings.Builder
	for i := range args.Len() {
		out.WriteString(identifier(args.At(i)))
	}
	return out.String()
}

// encoderFor and decoderFor name the two halves of one type's codec.
//
// The subject's own halves are its methods, and these are the functions
// everything else calls. Where the type can carry a method the function
// forwards to it; where it cannot the function is the whole codec. Callers name
// one thing either way, which is what keeps a subject moved into another
// package from changing anything that reads it.
func encoderFor(t types.Type) string { return "append" + identifier(t) + "JSON" }

func decoderFor(t types.Type) string { return "decode" + identifier(t) + "JSON" }

// Methods the codec declares. AppendJSON is the implementation and the two
// beside it are what the standard library dispatches to, in either of its
// generations; the borrowing reader is the sharp variant a caller asks for by
// name.
const (
	appendJSONMethod = "AppendJSON"
	marshalMethod    = "MarshalJSON"
	unmarshalMethod  = "UnmarshalJSON"
	borrowedMethod   = "UnmarshalJSONBorrowed"

	// And the text codec's, which this layer never writes and does call.
	textMarshalMethod   = "MarshalText"
	textAppendMethod    = "AppendText"
	textUnmarshalMethod = "UnmarshalText"

	// And the streaming codec of encoding/json/v2, which this layer neither
	// writes nor calls: calling it needs a jsontext encoder, and generated
	// output carries none. A type declaring that pair is written through the
	// reflective boundary, where the standard library dispatches to it.
	streamMarshalMethod   = "MarshalJSONTo"
	streamUnmarshalMethod = "UnmarshalJSONFrom"
)

// declaresCodec reports whether a type has a codec of its own.
//
// Declared on the type itself, rather than satisfied however. The two differ for
// a struct that embeds something with a codec, and the difference is the whole
// question: a type whose author wrote the methods is an author overriding what
// forge would do, and a type that merely inherited them from a field is nobody
// deciding anything. Go lets a method declared on the type shadow one it
// embeds, so writing a codec for the second is legal, and is what the author of
// the enclosing struct almost certainly meant.
func (p *planner) declaresCodec(t types.Type) bool {
	writes, reads := p.codecHalves(t)
	return writes != "" && reads != ""
}

// codecHalves returns the methods a type writes and reads JSON through, or
// empty strings where it declares none.
//
// Either generation of the contract counts, because either is a type whose
// author said what its wire form is. What differs is only how it is called:
// AppendJSON is called straight, since appending is what generated code does
// anyway; MarshalJSON and the streaming pair are reached through the standard
// library, which knows how to call each and validates what comes back.
//
// Asked of the type where the model never built one: a field may be a named
// integer in another module, and what matters is only whether the method is
// there. A type that declares one half and not the other is not treated as
// having a codec — that is reported where it is found, because generating the
// missing half would redeclare the one that is there under a reader that never
// consults it.
func (p *planner) codecHalves(t types.Type) (writes, reads string) {
	switch {
	case p.declares(t, appendJSONMethod):
		writes = appendJSONMethod
	case p.declares(t, marshalMethod):
		writes = marshalMethod
	case p.declares(t, streamMarshalMethod):
		writes = streamMarshalMethod
	}

	switch {
	case p.declares(t, unmarshalMethod):
		reads = unmarshalMethod
	case p.declares(t, streamUnmarshalMethod):
		reads = streamUnmarshalMethod
	}

	return writes, reads
}

// declaresText reports whether a type carries a text codec, so that its value
// goes into JSON as the string that codec writes.
//
// Both halves, for the reason [planner.declaresCodec] wants both: a value
// written through one half and read through nothing is a document that cannot
// be loaded back. A type with only one half is left alone rather than
// reported, because unlike a JSON codec this layer was never going to write
// either half — there is nothing to collide with and nothing the author has to
// resolve. Their type is written as whatever it is made of, which is what
// happened before this question was asked at all.
//
// Asked of what the run will write as well as of what the author wrote, and the
// two are separate questions asked by [planner.has]. What a neighbour
// declaration's layers will write is in neither the package nor the model on
// the first run, and is in the package on every run after — so believing the
// package would write the number into an empty checkout and the name into a
// full one, from one unchanged declaration. Forge's own output is kept out of
// the answer for that reason, and what the run will write is asked of the
// layers instead.
func (p *planner) declaresText(t types.Type) bool {
	// A JSON codec is not asked about here, because it was asked about first:
	// [planner.owned] resolves a type carrying any half of one before a text
	// codec is considered, which is the standard library's own precedence.
	return p.textWriter(t) != "" && p.has(t, textUnmarshalMethod)
}

// textWriter names the half of a text codec that writes, or nothing where the
// type has neither.
//
// AppendText where the type has it, which is the standard library's own
// preference and now for the same reason: there is a buffer to append into.
// Generated code writes the text straight into the document where the JSON
// string's content belongs and settles it afterwards — see jsonCloseText in
// the wire runtime — so the ordinary value costs no allocation, where
// MarshalText hands back a slice made just to be copied from. MarshalText
// remains for the type that declares only it.
func (p *planner) textWriter(t types.Type) string {
	switch {
	case p.has(t, textAppendMethod):
		return textAppendMethod
	case p.has(t, textMarshalMethod):
		return textMarshalMethod
	default:
		return ""
	}
}

// has reports whether a type will carry a method with the signature this layer
// would call it by: one the author wrote, or one this run is about to write.
//
// The signature and not only the name. A method's name is what a package holds
// it under and says nothing about how it may be called, so a MarshalText
// returning one value rather than two is a method by that name and not a text
// codec's half. Reading only the name would generate a call with the wrong
// number of results into a file the author cannot edit — a package that does
// not build, from a run that reported nothing wrong.
//
// The tree is asked through [plugin.Context.Authored], which keeps forge's own
// output out of the answer, and the run through [plugin.Context.Writes]. What
// this run will write is taken on trust, because there is no signature to read
// yet: a method a layer named is one it is generating, and a layer that
// generated a MarshalText of some other shape has broken its own contract
// rather than misled this.
func (p *planner) has(t types.Type, method string) bool {
	if p.willWrite != nil && p.willWrite(t, method) {
		return true
	}
	return p.authored != nil && p.authored(t, method) && signed(t, method)
}

// signed reports whether the method a type declares has the signature this
// layer calls it by.
//
// [shaped] is where each of the four shapes is written down.
func signed(t types.Type, method string) bool {
	named, is := types.Unalias(t).(*types.Named)
	if !is {
		return false
	}

	for i := range named.NumMethods() {
		one := named.Method(i)
		if one.Name() != method {
			continue
		}

		sig, is := one.Type().(*types.Signature)
		if !is {
			return false
		}
		return shaped(sig, method)
	}

	return false
}

// shaped reports whether a signature is the one its name is standardised with.
//
// A writer takes nothing and answers with bytes and an error. An appender takes
// the buffer to write into and answers the same way. A reader takes bytes and
// answers with an error alone. Each is spelled out rather than derived, because
// there are four of them and each is a shape somebody standardised.
func shaped(sig *types.Signature, method string) bool {
	// A variadic parameter is a slice to the function and one value to every
	// caller, so a MarshalText taking ...byte reads as taking []byte here and
	// is called with a single byte. It satisfies none of the interfaces these
	// names belong to either, so refusing it agrees with the standard library
	// as well as with the compiler.
	if sig.Variadic() {
		return false
	}

	switch method {
	case textMarshalMethod, marshalMethod:
		return sig.Params().Len() == 0 && bytesThenError(sig.Results())

	case textAppendMethod, appendJSONMethod:
		return isBytes(only(sig.Params())) && bytesThenError(sig.Results())

	case textUnmarshalMethod, unmarshalMethod, borrowedMethod:
		return isBytes(only(sig.Params())) &&
			sig.Results().Len() == 1 && isError(sig.Results().At(0).Type())

	default:
		return false
	}
}

// bytesThenError reports whether a result list is ([]byte, error).
func bytesThenError(held *types.Tuple) bool {
	return held.Len() == 2 && isBytes(held.At(0).Type()) && isError(held.At(1).Type())
}

// only returns the one type in a tuple, or nil where the tuple is not one long.
func only(held *types.Tuple) types.Type {
	if held.Len() != 1 {
		return nil
	}
	return held.At(0).Type()
}

// isBytes and isError name the two types every half of a text codec is spelled
// with.
//
// Through an alias, because an alias is the same type and satisfies the same
// interface: a MarshalText answering with a Bytes that is an alias for []byte
// is a text codec's writer, and one answering with a defined type over []byte
// is not — the compiler and the standard library agree on both, and so must
// this. A named type is not unwrapped for the same reason.
func isBytes(held types.Type) bool {
	slice, is := types.Unalias(held).(*types.Slice)
	if !is {
		return false
	}

	basic, is := types.Unalias(slice.Elem()).(*types.Basic)
	return is && basic.Kind() == types.Byte
}

func isError(held types.Type) bool {
	named, is := types.Unalias(held).(*types.Named)
	return is && named.Obj() != nil && named.Obj().Pkg() == nil && named.Obj().Name() == "error"
}

// declares reports whether the author declared a method on a type.
//
// The model where this run has one, and go/types otherwise, and the difference
// between them is forge's own output. A generated file is part of the package
// and is loaded with it — it has to be, or a call site naming a generated type
// would stop the load — so go/types reports the codec this layer wrote last
// time exactly as it reports one somebody typed. Believing it would make the
// second run delegate to what the second run has stopped writing, in a package
// that then names a method nothing declares.
//
// The model is built from what the author wrote, which is the question being
// asked, and it holds every type this run is writing a codec for: the subject
// and everything reachable from it. Everything else is a type forge has never
// written anything for, so what go/types says about it is the author's.
func (p *planner) declares(t types.Type, name string) bool {
	if held, ours := p.known[key(t)]; ours {
		return held.HasMethod(name)
	}
	return hasMethod(t, name)
}

// valueMethod reports whether a method can be called on a value of the type,
// rather than only on a pointer to one.
//
// The distinction decides what generated code may write. A method on the value
// can be called on anything; a method on the pointer can be called only on
// something addressable, and not everything a codec holds a value in is — a
// map's element has no address at all.
func valueMethod(t types.Type, name string) bool {
	return types.NewMethodSet(t).Lookup(nil, name) != nil
}

// halfCodec returns the one codec method a type declares, when it declares one
// and not the other.
//
// Two halves is a codec and none is a type this layer writes one for. One is
// neither, and it is worth naming: whichever half is there is the one a
// generated pair would redeclare.
func (p *planner) halfCodec(t types.Type) string {
	writes, reads := p.codecHalves(t)

	switch {
	case writes != "" && reads == "":
		return writes
	case reads != "" && writes == "":
		return reads
	default:
		return ""
	}
}

// hasMethod reports whether a named type explicitly declares a method.
//
// Whichever receiver it is declared with. The two halves conventionally differ —
// marshaling reads and is written on the value, unmarshaling writes and must be
// written on the pointer — and go/types records both against the named type, so
// neither has to be asked for separately.
func hasMethod(t types.Type, name string) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}

	for i := range named.NumMethods() {
		if named.Method(i).Name() == name {
			return true
		}
	}
	return false
}

// tagOption returns a json tag option written on a field, and whether it is
// there.
func tagOption(field plugin.Field, name string) (plugin.TagOption, bool) {
	tag, ok := field.Tag(jsonKey)
	if !ok {
		return plugin.TagOption{}, false
	}
	return tag.Lookup(name)
}
