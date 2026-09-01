package validate

import (
	"go/types"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/okian/forge/internal/model"
)

// tagKey is the struct tag key the rules are read from.
const tagKey = "validate"

// The rules this package knows. The list is closed: a name that is not one of
// these is refused rather than ignored, because a tag that says something and
// does nothing leaves an author believing a value is checked.
const (
	ruleRequired = "required"
	ruleNonzero  = "nonzero"
	ruleMin      = "min"
	ruleMax      = "max"
	ruleLen      = "len"
	ruleOneOf    = "oneof"
	ruleRegexp   = "regexp"
)

// known lists every rule in the order the package documents them, which is the
// order a diagnostic lists them in when a tag names something else.
var known = []string{ruleRequired, ruleNonzero, ruleMin, ruleMax, ruleLen, ruleOneOf, ruleRegexp}

// rule is one rule written on a field.
type rule struct {
	// name is the rule, and written is the whole option as the author typed it
	// — "min=2" — which is what a failure reports so that the message and the
	// tag read alike.
	name    string
	written string

	// number is the value of min, max and len, and digits records whether it
	// was written as a whole number. A length is compared against an integer
	// whatever the rule's own value looked like.
	number string
	digits bool

	// members are the values oneof accepts, in the order they were written.
	members []string

	// pattern is what regexp was given, already compiled here so that a
	// pattern that does not compile is refused where the author can see it
	// rather than panicking in somebody else's init.
	pattern string
}

// form is what a field's type is, as far as the rules are concerned.
//
// A coarser question than the model's own classification, because what decides
// whether a rule applies is what the language will let a condition be written
// about: whether the value can be nil, whether it has a length, whether it can
// be compared, whether it can be ordered.
type form struct {
	// nilable records that the value has a nil to be distinguished from a
	// value: a pointer, an interface, a slice, a map, a channel, a function.
	nilable bool

	// sized records that len applies: a string, a slice, a map, an array.
	sized bool

	// comparable records that == may be written about it.
	comparable bool

	// numeric records that < and > may be written about the value itself
	// rather than about its length, and float that the value may have a
	// fractional part.
	numeric bool
	float   bool

	// boolean records a boolean, whose zero is written false and which nothing
	// else in this table answers for.
	boolean bool

	// text records that the value is a string, which is the only thing a
	// pattern can be matched against.
	text bool

	// array records a fixed-length array, whose length is a property of the
	// type rather than of the value.
	array bool

	// structure records a struct, which is checked by its own rules rather
	// than by any written here.
	structure bool
}

// formOf describes a type in the terms the rules are written in.
//
// Through the named types to what they are underneath, because a rule is about
// what the language will let be written: a type declared over a string takes
// every rule a string takes, and an author who wrote one expects it to.
func formOf(t types.Type) form {
	if t == nil {
		return form{}
	}

	out := form{comparable: types.Comparable(t)}

	switch under := t.Underlying().(type) {
	case *types.Basic:
		info := under.Info()
		out.numeric = info&types.IsNumeric != 0 && info&types.IsComplex == 0
		out.float = info&types.IsFloat != 0
		out.text = info&types.IsString != 0
		out.boolean = info&types.IsBoolean != 0
		out.sized = out.text

	case *types.Pointer, *types.Interface, *types.Chan, *types.Signature:
		out.nilable = true

	case *types.Slice, *types.Map:
		out.nilable, out.sized = true, true

	case *types.Array:
		out.sized, out.array = true, true

	case *types.Struct:
		out.structure = true

	default:
	}

	return out
}

// wants describes what a rule needs of a type, for the diagnostic that reports
// a rule written where it does not apply.
//
// The advice is the other half of it, and is what makes the two rules that look
// alike teachable: an author who wrote required on an int is told to write
// nonzero, and one who wrote nonzero on a slice is told to write required.
type wants struct {
	// accepts reports whether the rule may be written about this shape.
	accepts func(form) bool

	// needs says what the rule needs, and instead names the rule to write
	// where there is a better one.
	needs   string
	instead string
}

// applies describes what each rule needs of the type it is written on.
var applies = map[string]wants{
	ruleRequired: {
		accepts: func(s form) bool { return s.nilable || s.sized && !s.array },
		needs:   "a value that can be absent: a pointer, an interface, a string, a slice, a map, a channel or a function",
		instead: ruleNonzero,
	},
	ruleNonzero: {
		accepts: func(s form) bool { return s.comparable },
		needs:   "a value the language will compare",
		instead: ruleRequired,
	},
	ruleMin: {
		accepts: func(s form) bool { return s.numeric || s.sized },
		needs:   "a number, or something with a length",
	},
	ruleMax: {
		accepts: func(s form) bool { return s.numeric || s.sized },
		needs:   "a number, or something with a length",
	},
	ruleLen: {
		accepts: func(s form) bool { return s.sized },
		needs:   "a string, a slice, a map or an array",
		instead: ruleMin,
	},
	ruleOneOf: {
		accepts: func(s form) bool { return (s.text || s.numeric) && s.comparable },
		needs:   "a string or a number",
	},
	ruleRegexp: {
		accepts: func(s form) bool { return s.text },
		needs:   "a string",
	},
}

// Demands reports whether the rules on a field say a value has to carry it.
//
// Exported because it is not only this layer's question. A builder refuses to
// hand back a value whose required fields were never given, and which fields
// those are is decided by this grammar — so it is answered here, once. A second
// reader would agree until the day the two disagreed, and they would disagree
// over the thing nobody looks at: a tag is split here rather than by the shared
// parser, and a space after a comma is a rule this reader trims and a simpler
// one does not.
//
// Both rules, because between them they cover every type and neither covers all
// of them: required is what a value that can be absent takes, and nonzero is
// what the language will compare. An author marking a field mandatory writes
// whichever their field's type accepts and means the same by both.
//
// What is wrong with the tag is not reported here. Whoever asks this is asking
// which fields are mandatory, and a tag that does not parse is this layer's to
// complain about — twice over would be twice reported.
func Demands(field model.Field) bool {
	tag, tagged := field.Tag(tagKey)
	if !tagged || tag.Raw == "" {
		return false
	}

	found, _ := written(tag.Raw)
	return slices.ContainsFunc(found, func(one rule) bool {
		return one.name == ruleRequired || one.name == ruleNonzero
	})
}

// written returns the rules a tag holds, and what is wrong with it.
//
// The tag is split here rather than by the shared parser, because the last rule
// may hold commas. A pattern is written as regexp= at the end and everything
// after the sign belongs to it, which is the only way a grammar separated by
// commas can carry a repetition like {2,4}.
func written(raw string) ([]rule, []problem) {
	var (
		out      []rule
		problems []problem
	)

	for rest := strings.TrimSpace(raw); rest != ""; {
		var one string
		if at := strings.Index(rest, ","); at >= 0 {
			one, rest = rest[:at], rest[at+1:]
		} else {
			one, rest = rest, ""
		}

		one = strings.TrimSpace(one)
		if one == "" {
			problems = append(problems, problem{says: "an empty rule, written between two commas"})
			continue
		}

		// The pattern takes the rest of the tag, sign included, so a comma
		// inside it separates nothing.
		if name, value, ok := opens(one, ruleRegexp); ok {
			if rest != "" {
				value += "," + rest
			}
			held, wrong := pattern(name, value)
			if wrong != nil {
				problems = append(problems, *wrong)
			} else {
				out = append(out, held)
			}
			break
		}

		held, wrong := parse(one)
		if wrong != nil {
			problems = append(problems, *wrong)
			continue
		}
		out = append(out, held)
	}

	return out, problems
}

// opens reports whether an option is the named rule with a value, and returns
// the two halves.
func opens(one, name string) (rule, string, bool) {
	if !strings.HasPrefix(one, name+"=") {
		return rule{}, "", false
	}
	return rule{name: name}, one[len(name)+1:], true
}

// pattern finishes a regexp rule, refusing one that does not compile.
//
// Compiled here rather than left to the generated file's init, because a
// pattern that does not compile is the author's mistake and this is where they
// can be told about it. What reaches the output is a pattern already known to
// be one.
func pattern(held rule, value string) (rule, *problem) {
	if value == "" {
		return rule{}, &problem{says: "a pattern with nothing after the sign"}
	}
	if swallowed := trailing(value); swallowed != "" {
		return rule{}, &problem{says: "a pattern that has swallowed " + swallowed +
			", because " + ruleRegexp + " takes the rest of the tag; write it before the pattern"}
	}
	if _, err := regexp.Compile(value); err != nil {
		return rule{}, &problem{says: "a pattern that does not compile: " + err.Error()}
	}

	held.pattern = value
	held.written = ruleRegexp + "=" + value

	return held, nil
}

// trailing returns the rule a pattern appears to have swallowed, or nothing.
//
// The pattern takes the rest of the tag, so a rule written after it becomes
// part of it — and a pattern with `,min=2` on the end still compiles, still
// matches nothing it was meant to, and says so nowhere. Refusing it is the only
// way the ordering rule is a rule rather than a hope.
//
// A guess, and deliberately a narrow one: what is looked for is a comma
// followed by the name of a rule and the end of the tag or a sign. A pattern
// that really has to hold such a run can write the run some other way — {1}
// after a character does not change what it matches — which is a smaller price
// than a check nobody notices is not happening.
func trailing(value string) string {
	for _, name := range known {
		for _, tail := range []string{"," + name, "," + name + "="} {
			at := strings.Index(value, tail)
			if at < 0 {
				continue
			}
			if len(tail) > len(","+name) || at+len(tail) == len(value) {
				return name
			}
		}
	}
	return ""
}

// parse turns one written option into a rule.
func parse(one string) (rule, *problem) {
	name, value, valued := strings.Cut(one, "=")
	held := rule{name: name, written: one}

	switch name {
	case ruleRequired, ruleNonzero:
		if valued {
			return rule{}, &problem{says: name + " takes no value, and " + one + " gives it one"}
		}
		return held, nil

	case ruleMin, ruleMax, ruleLen:
		if !valued || value == "" {
			return rule{}, &problem{says: name + " needs a number, and " + one + " gives it none"}
		}
		if _, err := strconv.ParseInt(value, 10, 64); err == nil {
			held.number, held.digits = value, true
			return held, nil
		}
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return rule{}, &problem{says: value + " is not a number"}
		}
		held.number = value
		return held, nil

	case ruleOneOf:
		held.members = strings.Fields(value)
		if !valued || len(held.members) == 0 {
			return rule{}, &problem{says: "oneof needs the values it accepts, and " + one + " lists none"}
		}
		return held, nil

	default:
		return rule{}, &problem{says: unknown(name)}
	}
}

// unknown says what to do about a rule nobody wrote.
func unknown(name string) string {
	return name + " is not a rule this generates for; the rules are " + strings.Join(known, ", ")
}

// problem is something wrong with a tag, said in a sentence the diagnostic puts
// the field's name in front of.
type problem struct{ says string }
