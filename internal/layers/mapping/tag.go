package mapping

import (
	"strings"

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
	for _, entry := range strings.Split(tag.Raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		one, forType, err := parsedEntry(entry, field)
		if err != nil {
			return pinned{}, false, err
		}

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

	for _, rung := range [][]pinned{qualified, bare} {
		switch len(rung) {
		case 0:
		case 1:
			return rung[0], true, nil
		default:
			return pinned{}, false, plugin.New(codeTagMalformed, field.Pos,
				"the from tag on %s carries %d entries that answer the mapping from %s: %s and %s",
				field.Name, len(rung), source, rung[0].entry, rung[1].entry).
				WithHint("one entry per source: at most one bare entry, and at most one naming %s", source)
		}
	}

	return pinned{}, false, nil
}

// parsedEntry reads one entry of a from tag: Member, Member(), Source.Member
// or Source.Member(), returning the pin and the source it is qualified for.
func parsedEntry(entry string, field plugin.Field) (pinned, string, error) {
	member := entry
	method := strings.HasSuffix(member, "()")
	if method {
		member = strings.TrimSuffix(member, "()")
	}

	forType := ""
	if before, after, cut := strings.Cut(member, "."); cut {
		forType, member = before, after
	}

	if member == "" || strings.ContainsAny(member, ". ()") || strings.Contains(forType, " ") {
		return pinned{}, "", plugin.New(codeTagMalformed, field.Pos,
			"%q on %s is not a from entry", entry, field.Name).
			WithHint("an entry is Member, Member(), Source.Member or Source.Member(), comma-separated")
	}

	return pinned{member: member, method: method, entry: entry}, forType, nil
}

// pin settles one member against the tag's answer: the named candidate, held
// to the same assignability every match is.
func pin(field plugin.Field, held pinned, all []candidate) (binding, error) {
	for _, one := range all {
		if one.name != held.member {
			continue
		}

		if held.method && !one.method {
			return binding{}, plugin.New(codeTagMissing, field.Pos,
				"the from tag on %s is written %s, and %s is a field",
				field.Name, held.entry, held.member).
				WithHint("drop the parens; they assert a method")
		}

		if !assignable(one, field) {
			return binding{}, unassignable(field, one, "is pinned by its from tag to")
		}

		via := settledField
		if one.method {
			via = settledMethod
		}
		return binding{field: field, via: via, from: one.name, tagged: true}, nil
	}

	return binding{}, plugin.New(codeTagMissing, field.Pos,
		"the from tag on %s names %s, which the source does not offer",
		field.Name, held.entry).
		WithHint("name an exported field, or an exported method taking nothing and returning one value; from tags read the source only")
}
