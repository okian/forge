package scalars

import (
	"fmt"
	"go/types"

	"github.com/okian/forge/internal/model"
)

// kind is what this package knows how to turn into text and back, and how.
//
// A table rather than a switch at each of the three places that needs one. The
// three ask different questions of the same fact — how does this read, how is
// it appended, how does it parse — and asking them in three switches is three
// places for a type to be handled in two of them.
type kind struct {
	// string returns an expression rendering the value as a string, and appends
	// returns one appending it to a byte slice.
	string  func(from string) string
	appends func(into, from string) string

	// parses returns statements reading the value out of a byte slice and
	// leaving what it read in a variable called parsed, returning the error
	// where it does not read. It is nil for a type that needs no parsing.
	parses func(from string) string

	// from returns the expression giving the field its value: a conversion of
	// what parses left, or the bytes themselves where nothing was parsed.
	from func(bytes string) string

	// converts records that the rendering reaches for strconv, which is what
	// decides whether the file imports it.
	converts bool

	// logs returns an expression building the slog value for it.
	logs func(name, from string) string
}

// scalar returns how a predeclared type is written, and whether this knows.
//
// Only the basic kinds, and only the ones with a strconv: a rendering that
// reached for fmt would pull reflection into a binary to write an int, and
// costing what the handwritten code costs is the whole of what these emitters
// promise.
func scalar(held model.Classified) (kind, bool) {
	if held.Class != model.ClassBasic {
		return kind{}, false
	}

	basic, is := held.Type.Underlying().(*types.Basic)
	if !is {
		return kind{}, false
	}

	one, known := kinds[basic.Kind()]
	return one, known
}

// kinds is every predeclared type these emitters handle.
//
// The integers are one entry each rather than one shared entry, because the
// conversion in each is the type's own: strconv takes an int64 and a uint64 and
// the value has to be converted to it, and a conversion written for the wrong
// width is a silent truncation.
var kinds = integers(map[types.BasicKind]kind{
	types.String: {
		string:  func(from string) string { return from },
		appends: func(into, from string) string { return "append(" + into + ", " + from + "...)" },
		from:    func(bytes string) string { return "string(" + bytes + ")" },
		logs:    func(name, from string) string { return "slog.String(" + name + ", " + from + ")" },
	},
	types.Bool: {
		string:   func(from string) string { return "strconv.FormatBool(" + from + ")" },
		appends:  func(into, from string) string { return "strconv.AppendBool(" + into + ", " + from + ")" },
		parses:   parsing("strconv.ParseBool(string(%s))"),
		from:     converted(""),
		converts: true,
		logs:     func(name, from string) string { return "slog.Bool(" + name + ", " + from + ")" },
	},
	types.Float64: {
		string:   func(from string) string { return "strconv.FormatFloat(" + from + ", 'g', -1, 64)" },
		appends:  func(into, from string) string { return "strconv.AppendFloat(" + into + ", " + from + ", 'g', -1, 64)" },
		parses:   parsing("strconv.ParseFloat(string(%s), 64)"),
		from:     converted(""),
		converts: true,
		logs:     func(name, from string) string { return "slog.Float64(" + name + ", " + from + ")" },
	},
	types.Float32: {
		string: func(from string) string { return "strconv.FormatFloat(float64(" + from + "), 'g', -1, 32)" },
		appends: func(into, from string) string {
			return "strconv.AppendFloat(" + into + ", float64(" + from + "), 'g', -1, 32)"
		},
		parses:   parsing("strconv.ParseFloat(string(%s), 32)"),
		from:     converted("float32"),
		converts: true,
		logs:     func(name, from string) string { return "slog.Float64(" + name + ", float64(" + from + "))" },
	},
})

// integers adds the signed and unsigned widths to the table.
//
// Added rather than written out, since every one of them differs from the next
// only in its own name, and ten entries that differ in one word each are ten
// places for one of them to differ in two.
func integers(into map[types.BasicKind]kind) map[types.BasicKind]kind {
	for held, name := range map[types.BasicKind]string{
		types.Int: "int", types.Int8: "int8", types.Int16: "int16",
		types.Int32: "int32", types.Int64: "int64",
	} {
		into[held] = integer(name, true)
	}

	for held, name := range map[types.BasicKind]string{
		types.Uint: "uint", types.Uint8: "uint8", types.Uint16: "uint16",
		types.Uint32: "uint32", types.Uint64: "uint64",
	} {
		into[held] = integer(name, false)
	}

	return into
}

// integer returns how one width of integer is written.
//
// The conversion is written out even where it is the identity — int64 to int64
// — because what it costs is nothing and what it saves is a reader checking
// which of ten entries is the one that does not have it.
func integer(name string, signed bool) kind {
	wide, format, appends, parse := "uint64", "strconv.FormatUint", "strconv.AppendUint", "strconv.ParseUint"
	if signed {
		wide, format, appends, parse = "int64", "strconv.FormatInt", "strconv.AppendInt", "strconv.ParseInt"
	}

	return kind{
		string: func(from string) string {
			return format + "(" + wide + "(" + from + "), 10)"
		},
		appends: func(into, from string) string {
			return appends + "(" + into + ", " + wide + "(" + from + "), 10)"
		},
		parses:   parsing(parse + "(string(%s), 10, " + width(name) + ")"),
		from:     converted(name),
		converts: true,
		logs: func(held, from string) string {
			if signed {
				return "slog.Int64(" + held + ", int64(" + from + "))"
			}
			return "slog.Uint64(" + held + ", uint64(" + from + "))"
		},
	}
}

// width returns the bit size strconv is told to parse at.
//
// Told rather than left at zero, because zero means "the platform's int" and a
// value that does not fit an int32 has to be refused where the field is an
// int32. A parse that succeeded and truncated would be the worst answer here:
// the round trip would appear to work and the value would be wrong.
func width(name string) string {
	switch name {
	case "int8", "uint8":
		return "8"
	case "int16", "uint16":
		return "16"
	case "int32", "uint32":
		return "32"
	case "int64", "uint64":
		return "64"
	default:
		// int and uint, whose width is the platform's. Zero is what strconv
		// takes for exactly that, and the conversion below is then the
		// identity.
		return "0"
	}
}

// parsing returns a reader built from a strconv call.
//
// What it read is left in parsed rather than assigned, so that the caller
// decides where it goes and a parse that failed has assigned nothing.
func parsing(call string) func(from string) string {
	return func(from string) string {
		return "\tparsed, err := " + fmt.Sprintf(call, from) + "\n" +
			"\tif err != nil {\n\t\treturn err\n\t}\n"
	}
}

// converted returns the expression giving a field what strconv answered with.
//
// A conversion in every case, including the ones where it is the identity:
// strconv answers with an int64 and a uint64 whatever the field is, and an
// entry missing its conversion is a compile error at best and a truncation
// written by hand at worst.
func converted(to string) func(string) string {
	return func(string) string {
		if to == "" {
			return "parsed"
		}
		return to + "(parsed)"
	}
}
