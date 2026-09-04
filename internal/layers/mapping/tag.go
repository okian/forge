package mapping

import (
	"go/token"
	"go/types"
	"strings"
	"unicode"

	"github.com/okian/forge/plugin"
)

// Codes the from tag refuses with. The tag names a source member from the
// target's own field, so what can be wrong is the spelling of the entry and
// what it points at.
var (
	codeTagMalformed = plugin.Register(3034, "a from tag is not shaped like one")
	codeTagMissing   = plugin.Register(3035, "a from tag names a member the source does not offer")
	codeTagAndHint   = plugin.Register(3036, "a from tag and a hint both settle one member")
)

// pinned is what a from tag says about one member for one mapping: the source
// member it must read, whether the author asserted a method, and the entry as
// written for the diagnostics that quote it back.
type pinned struct {
	member string
	method bool
	entry  string
}

// fromTag reads the field's from tag as it applies to the mapping whose source
// is named, and reports whether any entry does.
//
// One tag serves every mapping into the target, so it is a comma-separated
// list: a qualified entry — Account.Contact — applies only to the mapping from
// that source, and a bare entry — Email — to whichever source maps in. The
// qualified entry wins where both are written, because it is the author being
// specific; two entries of one specificity are refused, because they are the
// author being contradictory.
func fromTag(field plugin.Field, source string) (pinned, bool, error) {
	tag, ok := field.Tag("from")
	if !ok {
		return pinned{}, false, nil
	}

	var qualified, bare []pinned
	total := 0
	for entry := range strings.SplitSeq(tag.Raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		one, forType, err := parsedEntry(entry, field)
		if err != nil {
			return pinned{}, false, err
		}
		total++

		switch forType {
		case source:
			qualified = append(qualified, one)
		case "":
			bare = append(bare, one)
		default:
			// Another mapping's entry: this target is bridged from more than
			// one source, and the entry belongs to a sibling declaration.
		}
	}

	if total == 0 {
		return pinned{}, false, plugin.New(codeTagMalformed, field.Pos,
			"the from tag on %s says nothing", field.Name).
			WithHint("write an entry — Member, Member(), Source.Member or Source.Member() — or drop the tag")
	}

	// Both rungs are checked before either is chosen: a contradiction is a
	// contradiction whichever entry would have answered this mapping, and a
	// tag must not be accepted by one sibling and refused by another.
	for _, rung := range [][]pinned{qualified, bare} {
		if len(rung) > 1 {
			return pinned{}, false, plugin.New(codeTagMalformed, field.Pos,
				"the from tag on %s carries %d entries that answer one mapping: %s and %s",
				field.Name, len(rung), rung[0].entry, rung[1].entry).
				WithHint("one entry per source: at most one bare entry, and at most one naming each source")
		}
	}

	if len(qualified) == 1 {
		return qualified[0], true, nil
	}
	if len(bare) == 1 {
		return bare[0], true, nil
	}
	return pinned{}, false, nil
}

// parsedEntry reads one entry of a from tag: Member, Member(), Source.Member
// or Source.Member(), returning the pin and the source it is qualified for.
//
// Both halves are held to being identifiers. The qualifier especially: an
// entry whose qualifier is not a name a source could have is a mistake to
// refuse, not a sibling mapping's entry to skip — skipped, it would fall back
// to the ladder and read a member the author pinned away from.
func parsedEntry(entry string, field plugin.Field) (pinned, string, error) {
	member := entry
	method := strings.HasSuffix(member, "()")
	if method {
		member = strings.TrimSuffix(member, "()")
	}

	forType := ""
	before, after, cut := strings.Cut(member, ".")
	if cut {
		forType, member = before, after
	}

	if !identifier(member) || (cut && !identifier(forType)) {
		return pinned{}, "", plugin.New(codeTagMalformed, field.Pos,
			"%q on %s is not a from entry", entry, field.Name).
			WithHint("an entry is Member, Member(), Source.Member or Source.Member(), comma-separated")
	}

	return pinned{member: member, method: method, entry: entry}, forType, nil
}

// identifier reports whether a name is one Go identifier, which is what both
// halves of an entry have to be.
func identifier(name string) bool {
	for i, r := range name {
		switch {
		case unicode.IsLetter(r) || r == '_':
		case i > 0 && unicode.IsDigit(r):
		default:
			return false
		}
	}
	return name != ""
}

// pin settles one member against the tag's answer: the named member, held to
// the same assignability every match is.
func pin(field plugin.Field, held pinned, all []candidate, source types.Type) (binding, error) {
	one, ok := offered(held.member, all, source)
	if !ok {
		return binding{}, plugin.New(codeTagMissing, field.Pos,
			"the from tag on %s names %s, which the source does not offer",
			field.Name, held.entry).
			WithHint("name an exported field, or an exported method taking nothing and returning one value; from tags read the source only")
	}

	if held.method && !one.method {
		return binding{}, plugin.New(codeTagMissing, field.Pos,
			"the from tag on %s is written %s, and %s is a field",
			field.Name, held.entry, held.member).
			WithHint("drop the parens; they assert a method")
	}

	if !assignable(one, field) {
		return binding{}, plugin.New(codeUnassignable, field.Pos,
			"%s is pinned by its from tag to %s, and %s does not assign to %s",
			field.Name, one.name,
			plugin.TypeString(one.typ), plugin.TypeString(field.Type.Type)).
			WithHint("pin a member whose type assigns, or drop the tag and write a //forge:map hint that converts")
	}

	via := settledField
	if one.method {
		via = settledMethod
	}
	return binding{field: field, via: via, from: one.name, tagged: true}, nil
}

// offered finds the member a pin names: among what the source declares first,
// and behind an embedded field second. A tag may name a promoted member —
// the author wrote the name and Go resolves the selector — where the ladder
// stays with what the source declares.
func offered(member string, all []candidate, source types.Type) (candidate, bool) {
	for _, one := range all {
		if one.name == member {
			return one, true
		}
	}

	if !token.IsExported(member) {
		return candidate{}, false
	}

	obj, _, _ := types.LookupFieldOrMethod(source, true, nil, member)
	switch held := obj.(type) {
	case *types.Var:
		if held.IsField() {
			return candidate{name: member, typ: held.Type()}, true
		}
	case *types.Func:
		sig, ok := held.Type().(*types.Signature)
		if ok && sig.Params().Len() == 0 && sig.Results().Len() == 1 {
			return candidate{name: member, typ: sig.Results().At(0).Type(), method: true}, true
		}
	}

	return candidate{}, false
}
