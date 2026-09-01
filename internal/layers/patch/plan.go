package patch

import (
	"strconv"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/model"
)

// codeTakenName reports a field whose place in the patch would take a name the
// patch already uses.
//
// A 4xxx rather than a 2xxx: nothing is wrong with the field, and nothing is
// wrong with the patch. What is wrong is that the two would be one name, which
// is a decision about emission rather than about either.
var codeTakenName = diag.Register(4020, "a patch's field wants a name the patch already has")

// codeNothingToChange reports a subject a patch would carry nothing of.
//
// A 2xxx, because it is about the subject: a patch with no fields is a request
// that cannot say anything, and only the author can change that.
var codeNothingToChange = diag.Register(2023, "a patch over a subject with nothing a caller can change")

// codeUnwritable reports a field whose type a patch cannot name, or must not
// copy.
//
// A 2xxx as well. Both are facts about the subject's fields that the patch's
// own declaration or Apply would run into, and both are the author's to change.
var codeUnwritable = diag.Register(2024, "a patch cannot carry a field of this type")

// takenHint says what to do about a field named after one of the patch's own
// methods.
const takenHint = "rename the field: a patch needs two names of its own, and they are the one " +
	"that writes it over a value and the one that says it asks for nothing"

// emptyHint says what to do about a subject a patch would carry nothing of.
const emptyHint = "export a field, or drop the layer: a patch is a request to change something, " +
	"and one that can name nothing is a request nobody can make"

// unnameableHint says what to do about a field whose type a patch cannot write
// down.
const unnameableHint = "a patch declares a field of the same type, and an unexported name " +
	"belongs to the package that declared it — export the type, or move the declaration " +
	"into that package"

// uncopyableHint says what to do about a field a patch would have to copy and
// must not.
const uncopyableHint = "hold it behind a pointer, which is the usual advice for a lock and is " +
	"what makes the value copyable — a patch writes a field over another by assigning it"

// The methods a patch carries and what its type is called after the subject.
const (
	applyMethod = "Apply"
	zeroMethod  = "IsZero"
	suffix      = "Patch"
	verb        = "patch"
)

// patched is one field a patch can carry.
type patched struct {
	// field is the field itself, which is what a diagnostic points at.
	field model.Field

	// name is the patch's own field, which is the subject's.
	name string

	// spelled is how the field's type must be written in the file being
	// generated into. The patch holds a pointer to it.
	spelled model.Spelling

	// tag is the field's struct tag, exactly as the subject carries it.
	//
	// Carried across rather than dropped, because a patch and the subject go
	// over the same wire. A codec reads a json tag for a member's name, so a
	// patch whose fields carried none would be written and read under the
	// field's own name while the subject used the tag's — and a request sent
	// with the names a reply came back under would name nothing the patch
	// recognised, decode into a patch that sets nothing, and change nothing at
	// all without reporting anything.
	tag string
}

// plan is the whole of one patch.
type plan struct {
	// into is the package being generated into, which decides what the patch's
	// own fields may name.
	into string

	// of is the subject, and spelled how it is written in the file being
	// generated into.
	of      *model.Struct
	spelled model.Spelling

	// declared is the patch type's own name.
	declared string

	// fields are the ones a patch can carry, in declaration order.
	fields []patched

	// kept records that a field was left out because a patch cannot reach it,
	// so that the type can say so rather than leave a reader to notice.
	kept bool

	diags diag.Set
}

// planned works out what a subject's patch is made of.
func planned(held *model.Struct, into string) *plan {
	out := &plan{
		into:     into,
		of:       held,
		spelled:  model.Spell(held.Type(), into, nil),
		declared: model.Through(held, "", suffix, into),
	}

	for _, field := range held.Fields {
		out.consider(field)
	}

	// A patch with no fields is a request that cannot say anything. Reported
	// rather than written, because what would be written is a type with two
	// methods, one of which always answers true and the other of which always
	// does nothing.
	if len(out.fields) == 0 && out.diags.Empty() {
		out.diags.Add(diag.New(codeNothingToChange, held.Pos,
			"%s has no field a caller could change", held.Ref().Name).
			WithHint("%s", emptyHint))
	}

	return out
}

// consider decides what one field of the subject means to the patch.
func (p *plan) consider(field model.Field) {
	// A patch is filled in from outside the package that declares the subject,
	// which is what makes it a patch rather than an assignment, and an
	// unexported field is not reachable from there.
	if !field.Exported {
		p.kept = true
		return
	}

	if field.Name == applyMethod || field.Name == zeroMethod {
		p.diags.Add(diag.New(codeTakenName, field.Pos,
			"a patch carrying %s would take the name one of its own methods has", field.Name).
			WithHint("%s", takenHint))
		return
	}
	if !p.writable(field) {
		return
	}

	p.fields = append(p.fields, patched{
		field:   field,
		name:    field.Name,
		spelled: model.Spell(field.Type.Type, p.into, nil),
		tag:     tagged(field),
	})
}

// tagged returns the field's struct tag as source writes it, or nothing where
// the field carries none.
//
// Rebuilt rather than carried across, because the model holds a tag as the keys
// it was read as rather than as the text it was written in. What comes out is
// the same tag: every key in the order it was written, each with its own value
// quoted again.
//
// Written with backquotes where it can be, which is how a tag is written, and
// as an ordinary string literal where a value holds one — a backquote inside a
// raw string ends it, and a tag that ended early would be a tag that meant
// something else.
func tagged(field model.Field) string {
	if len(field.Tags) == 0 {
		return ""
	}

	var out strings.Builder
	for i, one := range field.Tags {
		if i > 0 {
			out.WriteByte(' ')
		}
		out.WriteString(one.Key)
		out.WriteByte(':')
		out.WriteString(strconv.Quote(one.Raw))
	}

	held := out.String()
	if strings.Contains(held, "`") {
		return strconv.Quote(held)
	}
	return "`" + held + "`"
}

// writable reports whether the patch can carry this field at all, and says why
// where it cannot.
//
// Two ways it cannot. The patch declares a field of the same type, and a name
// that is unexported belongs to the package that declared it; and Apply assigns
// the value, which is a copy — so a field holding a lock would produce an
// assignment the vet everybody runs reports, in a file the caller cannot fix.
func (p *plan) writable(field model.Field) bool {
	if what, found := model.Unnameable(field.Type.Type, p.into); found {
		p.diags.Add(diag.New(codeUnwritable, field.Pos,
			"%s is a %s, and %s cannot be named from the package being generated into",
			field.Name, field.Type, what).
			WithHint("%s", unnameableHint))
		return false
	}

	if what, found := model.Uncopyable(field.Type.Type); found {
		p.diags.Add(diag.New(codeUnwritable, field.Pos,
			"%s holds a %s, which is a value that must not be copied", field.Name, what).
			WithHint("%s", uncopyableHint))
		return false
	}

	return true
}
