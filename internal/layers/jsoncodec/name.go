package jsoncodec

import (
	"go/types"
	"strconv"
	"strings"
	"unicode"

	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/tags"
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
func wireName(field model.Field, style string) (string, bool) {
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
		return camel(name)
	default:
		return name
	}
}

// camel writes a Go name with its first word in lower case.
//
// The first word rather than the first letter, because an exported Go name
// often opens with an initialism and lowering one letter of it produces
// jSONValue — a name nobody would write and no reader would recognise. The run
// of capitals a name opens with is one word, except that a capital immediately
// before a lower-case letter has already started the next one.
func camel(name string) string {
	runes := []rune(name)

	end := 0
	for end < len(runes) && unicode.IsUpper(runes[end]) {
		end++
	}

	switch {
	case end == 0:
		return name
	case end < len(runes) && end > 1:
		// The last capital of the run opens the next word: JSONValue is json
		// and Value, not jsonv and alue.
		end--
	}

	for i := range end {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}

// snake writes a Go name in lower_snake_case.
//
// The boundaries are the ones a reader would draw rather than the ones a naive
// scan finds. A run of capitals is one word, so UserID is user_id rather than
// user_i_d; the last capital of such a run starts the next word when a
// lower-case letter follows it, so JSONValue is json_value rather than
// jsonv_alue. A digit continues whatever word it is in.
func snake(name string) string {
	var out strings.Builder
	runes := []rune(name)

	for i, r := range runes {
		if !unicode.IsUpper(r) {
			out.WriteRune(r)
			continue
		}

		previous := i > 0 && !unicode.IsUpper(runes[i-1]) && runes[i-1] != '_'
		trailing := i > 0 && i+1 < len(runes) && unicode.IsUpper(runes[i-1]) && unicode.IsLower(runes[i+1])
		if previous || trailing {
			out.WriteByte('_')
		}
		out.WriteRune(unicode.ToLower(r))
	}

	return out.String()
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
		return model.Upper(held.Name())

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
	ref := model.RefOf(named)

	out := model.Upper(ref.Name)
	if obj := named.Obj(); obj != nil && obj.Pkg() != nil {
		out = model.Upper(obj.Pkg().Name()) + out
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
func encoderFor(t types.Type) string { return "encode" + identifier(t) + "JSONTo" }

func decoderFor(t types.Type) string { return "decode" + identifier(t) + "JSONFrom" }

// Methods the codec declares, which are the interfaces encoding/json/v2
// dispatches to and the names an author's own codec would already have taken.
const (
	marshalMethod   = "MarshalJSONTo"
	unmarshalMethod = "UnmarshalJSONFrom"
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
//
// Asked of the type rather than of the model, because it is asked about types
// the model never built: a field may be a named integer in another module, and
// what matters is only whether the method is there. A type that declares one
// half and not the other is not treated as having a codec — half a codec is a
// mistake worth a compile error in the output rather than a silent decision
// here.
func declaresCodec(t types.Type) bool {
	return hasMethod(t, marshalMethod) && hasMethod(t, unmarshalMethod)
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
func halfCodec(t types.Type) string {
	writes, reads := hasMethod(t, marshalMethod), hasMethod(t, unmarshalMethod)

	switch {
	case writes && !reads:
		return marshalMethod
	case reads && !writes:
		return unmarshalMethod
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
func tagOption(field model.Field, name string) (tags.Option, bool) {
	tag, ok := field.Tag(jsonKey)
	if !ok {
		return tags.Option{}, false
	}
	return tag.Lookup(name)
}
