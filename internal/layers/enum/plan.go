package enum

import (
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/okian/forge/plugin"
)

// What can be wrong with a subject asked for the API of a closed set.
//
// Refusals rather than empty sets, for the reason the package doc gives: what
// an empty one would be written is a Parse that accepts nothing and a String
// that always fails, which is a type made harder to use in exchange for nothing.
var (
	codeNotScalar = plugin.Register(2027, "a closed set was asked for over something that is not a named scalar")
	codeNoMembers = plugin.Register(2028, "a closed set was asked for and no constants are declared of the type")
	codeNotLocal  = plugin.Register(2029, "a closed set was asked for over a type another package declares")
	codeOneName   = plugin.Register(2030, "two members of a closed set are called the same thing")
)

// The methods a set carries, and the prefix its parser is named under.
const (
	displayMethod   = "String"
	validMethod     = "Valid"
	valuesFunc      = "Values"
	parseFunc       = "Parse"
	marshalMethod   = "MarshalText"
	unmarshalMethod = "UnmarshalText"
	appendMethod    = "AppendText"
)

// member is one constant of the set.
type member struct {
	// name is the constant's own name, and text what the member is called on
	// the wire and in a rendering.
	name string
	text string

	// literal is the value written out as Go source, which is what tells two
	// names for one member apart from two members.
	literal string

	// only records that no earlier constant is called this.
	//
	// A named string's members are their own text, so two constants written
	// with one value are two members of one name — which is a switch with the
	// same case twice, and does not compile. The first is the one kept, for the
	// reason the first of two names for one value is.
	only bool

	// first records that no earlier constant holds this value.
	//
	// Aliasing is ordinary while a name is being changed, and the two halves of
	// the API want different things from it. Parsing takes every name, because
	// a reader that took only the new one would break every caller the moment
	// it was added. Rendering and listing take the first only: a switch with
	// two cases of one value does not compile, and a list holding a value twice
	// is not a set.
	first bool
}

// plan is one closed set.
type plan struct {
	// of is the subject, and spelled how it is written in the file being
	// generated into.
	of      *plugin.Struct
	spelled plugin.Spelling

	// members are the constants of it, in declaration order.
	members []member

	// text records that the underlying type is a string, and unsigned that it is
	// a number with no negatives. Between them they decide what a member is
	// called and how a value that is not one is rendered.
	text     bool
	unsigned bool

	diags plugin.Diagnostics
}

// planned works out what a subject's closed set is made of.
func planned(held *plugin.Struct, fset *token.FileSet, into string, bound []plugin.Import, at token.Position) *plan {
	out := &plan{of: held, spelled: plugin.Spell(held.Type(), into, bound)}

	basic, scalar := underlying(held)
	if !scalar {
		out.diags.Add(plugin.New(codeNotScalar, at,
			"%s is not a named scalar, so there are no constants of it to find",
			plugin.TypeString(held.Type())).
			WithHint("%s", "a closed set is a named number or a named string with the constants "+
				"of it declared alongside, as in `type Status int` and a const block counting from iota"))
		return out
	}

	out.text = basic.Info()&types.IsString != 0
	out.unsigned = basic.Info()&types.IsUnsigned != 0
	out.members = members(held, fset, out.text)
	out.collisions(held, at)

	if len(out.members) == 0 {
		out.diags.Add(plugin.New(codeNoMembers, at,
			"nothing declares a constant of %s, so there is no set to write the API of",
			plugin.TypeString(held.Type())).
			WithHint("%s", "declare the members as constants of the type, in a const block beside "+
				"it; what is written here is read from those and from nothing else"))
	}

	return out
}

// collisions reports two members that are called the same thing and are not the
// same member.
//
// Two constants of one value written under two names is an alias, and is
// answered rather than refused: both parse and the first renders. Two constants
// of *different* values whose names come to one word is not that. Both would
// render alike, both would say they are members, and parsing the name they
// share would give whichever was declared first — so one of them could go out
// and never come back, and nothing would say so.
//
// It is the shape a rule invites: taking the type's name off the front and
// lower-casing what is left maps CodedOK and CodedOk onto one word. Almost
// nobody means that, and a set where it happens is a set with a typo in it.
func (p *plan) collisions(held *plugin.Struct, at token.Position) {
	seen := make(map[string]member, len(p.members))

	for _, one := range p.members {
		first, taken := seen[one.text]
		if !taken {
			seen[one.text] = one
			continue
		}
		if first.literal == one.literal {
			continue
		}

		p.diags.Add(plugin.New(codeOneName, at,
			"%s and %s are both called %q, and they are not the same member of %s",
			first.name, one.name, one.text, plugin.TypeString(held.Type())).
			WithHint("%s", "a member is named after its constant with the type's own name taken "+
				"off the front, so two constants differing only in how they are capitalised "+
				"come to one name — rename one of them, or give them the same value if one "+
				"was meant as the other's older spelling"))
	}
}

// elsewhere reports a subject this package cannot declare on.
//
// Every part of a closed set's API is the subject's — the methods are on it and
// the two functions are named after it — so a set over a type declared
// somewhere else would have to be written in a package that may not declare any
// of it. Reported against the declaration, which is the thing the author wrote
// and the thing they can move.
func elsewhere(held *plugin.Struct, at token.Position) error {
	return plugin.New(codeNotLocal, at,
		"%s is declared in another package, and every part of a closed set's API belongs to it",
		plugin.TypeString(held.Type())).
		WithHint("%s", "Go lets a method be declared only in the package that declares its type, "+
			"and the parser and the lister are named after it — so write the declaration in "+
			"the package that declares "+held.Ref().Name+", or over a type of your own")
}

// underlying returns the predeclared type a subject is a name for, and whether
// it is one.
//
// A named number or a named string, and nothing else. A closed set is a value
// compared against a fixed list, so the underlying type has to be one a
// constant can be written of — which rules out a slice with methods and a
// struct alike, for the same reason and by the same test.
func underlying(held *plugin.Struct) (*types.Basic, bool) {
	if held == nil || held.Named == nil {
		return nil, false
	}

	basic, is := held.Named.Underlying().(*types.Basic)
	if !is {
		return nil, false
	}

	// A boolean has two values and both are spelled already, so a set over one
	// is a rename of true and false. Untyped kinds cannot be a subject at all.
	const usable = types.IsInteger | types.IsString
	return basic, basic.Info()&usable != 0
}

// earlier orders two positions by the file they are in and then by where in it.
//
// Not by [token.Pos], which is only ordered within one file. A package's files
// are parsed in parallel into one file set, so which of them gets the lower
// base is decided by which goroutine finished first — and a set whose members
// are split across two files would be ordered differently from one run to the
// next, rewriting the file every time and flapping the check that asks whether
// it is current.
//
// By file name rather than by the order the files were read, because that is
// the order the go command lists them in and the order somebody reading a
// package sees.
func earlier(a, b token.Position) int {
	if a.Filename != b.Filename {
		return strings.Compare(a.Filename, b.Filename)
	}
	return a.Offset - b.Offset
}

// members returns the constants declared of a type, in declaration order.
//
// The package's scope rather than the file the type is in, because a large set
// is usually written away from the type it belongs to — and a walk over one
// file would find half of it and say nothing about the rest.
//
// Nothing, where there is no file set to resolve a position against. One comes
// from the loader and a model built by hand has none, so a caller assembling
// one gets a subject with no members and the refusal that goes with it, rather
// than a panic in the middle of working out where a constant was written.
//
// The type's own package, which for a set forge writes is the package being
// generated into — [elsewhere] refuses any other, since every part of the API
// is the type's own and Go lets only its package declare that. It is asked of
// the type rather than of the run all the same, because the type is what the
// constants belong to and reading them from anywhere else would be reading them
// from a coincidence.
//
// Sorted by position rather than by name. Declaration order is what a reader of
// the constant block expects and what a run counted by iota means; the order
// the scope reports is alphabetical, which would put StatusActive before
// StatusUnknown and make Values read as somebody's mistake.
func members(held *plugin.Struct, fset *token.FileSet, text bool) []member {
	pkg := held.Named.Obj().Pkg()
	if pkg == nil || pkg.Scope() == nil || fset == nil {
		return nil
	}

	var (
		found []member
		where = make(map[string]token.Position)
	)

	for _, name := range pkg.Scope().Names() {
		one, is := pkg.Scope().Lookup(name).(*types.Const)
		if !is || !types.Identical(one.Type(), held.Named) {
			continue
		}

		// The exported ones. A run counted by iota usually ends in a sentinel
		// nobody outside the package is meant to hold — a count, an end marker
		// — and a set that offered one would be offering a member whose whole
		// purpose is not being one.
		if !one.Exported() {
			continue
		}

		found = append(found, member{
			name:    name,
			text:    called(name, held.Ref().Name, one, text),
			literal: one.Val().ExactString(),
		})
		where[name] = fset.Position(one.Pos())
	}

	slices.SortFunc(found, func(a, b member) int {
		return earlier(where[a.name], where[b.name])
	})

	// Marked after sorting, because which of two is first is a fact about
	// declaration order and the scope reported them alphabetically.
	//
	// Twice over, because a set can hold two of one thing in two ways and the
	// two halves of the API want different answers. Two names for one value is
	// an alias, and parsing takes both. Two members of one name is not an
	// alias — for a named string it is two constants written with one text —
	// and nothing can take both, since a switch with one case twice does not
	// compile.
	values := make(map[string]bool, len(found))
	names := make(map[string]bool, len(found))

	for i, one := range found {
		found[i].first = !values[one.literal]
		found[i].only = !names[one.text]

		values[one.literal] = true
		names[one.text] = true
	}

	return found
}

// distinct returns the members that are the first name for their value, which
// is what a switch over values may hold.
func (p *plan) distinct() []member {
	return only(p.members, func(one member) bool { return one.first })
}

// named returns the members that are the first of their name, which is what a
// switch over names may hold.
func (p *plan) named() []member {
	return only(p.members, func(one member) bool { return one.only })
}

// only returns the members a test keeps.
func only(held []member, keep func(member) bool) []member {
	out := make([]member, 0, len(held))
	for _, one := range held {
		if keep(one) {
			out = append(out, one)
		}
	}
	return out
}

// called returns what a member is known as outside the package.
//
// A named string carries its text already, and that text is what the author
// chose to put on the wire — so it is read off the value rather than derived
// from the name. Deriving one would give two answers about a member and send
// the wrong one out.
//
// A named number has no text, so the name is it, with the type's own name taken
// off the front where it is there: StatusActive is "active" for a Status. What
// is left is lower-cased a word at a time rather than a letter at a time, by
// the rule a codec already names a field with — StatusOK is "ok" and not "oK",
// which is a name nobody would write and no reader would recognise.
//
// A constant whose name does not begin with the type's keeps all of it, because
// there is nothing to take off and cutting somewhere else would name a member
// after a rule rather than after what its author wrote.
func called(name, of string, held *types.Const, text bool) string {
	if text {
		return constant.StringVal(held.Val())
	}

	shortened, cut := shortened(name, of)
	if !cut || shortened == "" {
		return name
	}
	return plugin.Camel(shortened)
}

// shortened takes the type's name off the front of a member's, and reports
// whether it was there to take.
//
// A word at a time through [plugin.Words] rather than a byte at a time, which
// is the same splitting a codec names a wire member with — the two have to
// agree, because a member's name is written by whichever of them the value
// reaches first. It is also what keeps a type called Status from cutting
// Statuses down to esActive: es is a prefix of the letters and not of the
// words, and what is left of a name cut there is not a name.
func shortened(name, of string) (string, bool) {
	member, typ := plugin.Words(name), plugin.Words(of)
	if len(typ) == 0 || len(member) < len(typ) {
		return name, false
	}

	for at, one := range typ {
		if !strings.EqualFold(member[at], one) {
			return name, false
		}
	}
	return strings.Join(member[len(typ):], ""), true
}
