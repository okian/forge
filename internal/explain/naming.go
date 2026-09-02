package explain

import (
	"strings"

	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/internal/words"
)

// Name is one generated name and where its spelling came from.
//
// "Why is my method called that" is the question forge gets asked about
// inflection, and it used to be answerable only by reading the generator. It is
// a question with a short answer — the name is built from a field, and either
// the dictionary knew the word or the regular rules did — so the answer belongs
// beside the rest of what explaining a declaration reports.
type Name struct {
	// Step is the step that emits the name, and Layer the marker that step is.
	Step  int
	Layer string

	// Name is the name as it will be written.
	Name string

	// From is the subject field the name was built from, or the empty string
	// where the name is the layer's own rather than the subject's.
	From string

	// By says what decided the spelling: the dictionary, the regular rules, the
	// Go initialism set, or the field's name already being plural. It is empty
	// where nothing inflected anything, which is every name a layer wrote out
	// in full.
	By string
}

// Dictionary returns the provenance of the dictionary that answered, which is
// what makes an answer about a name worth anything: two builds of forge with
// different dictionaries derive different names, and a reader comparing two
// generated files needs to know which they are looking at.
func Dictionary() string { return words.Provenance() }

// naming works out where each emitted name came from.
//
// Re-derived here rather than reported by the layer that built it. A layer
// hands over the names it will write and nothing about how it reached them, and
// asking every layer to carry a derivation would be a change to the surface
// every third-party layer is written against, for a report. What this can do
// instead is ask the same questions of the same words: a name that is the
// plural of a field is one, whichever layer built it, and a name that ends in a
// field with something spelled in front of it is that.
//
// So it answers where it can and says nothing where it cannot, which is the
// honest shape for a report that is reading the output rather than the run.
func naming(subject *model.Struct, steps []Step) []Name {
	if subject == nil {
		return nil
	}

	var out []Name
	for _, step := range steps {
		for _, method := range step.Methods {
			held := Name{Step: step.Number, Layer: step.Name, Name: method}
			held.From, held.By = derived(subject, method)
			out = append(out, held)
		}
	}
	return out
}

// derived returns the field a name was built from and what spelled it.
func derived(subject *model.Struct, method string) (string, string) {
	for _, field := range subject.Fields {
		if from, by, is := against(field.Name, method); is {
			return from, by
		}
	}
	return "", ""
}

// against asks whether one name was built from one field, and how.
//
// Three shapes, in the order that tells them apart. The plural of the field is
// a projection, and what answered is the interesting part. The field's own name
// is a projection of a field that was already plural, which is the case worth
// naming explicitly because it is the one a reader is most likely to be
// puzzled by. And the field with something in front of it is a sort or an
// index, where the inflection did nothing and the spelling still might have —
// ByID is the field ID rather than the Id somebody wrote.
func against(field, method string) (string, string, bool) {
	plural, by := words.PluralFrom(field)

	switch {
	case method == plural:
		return field, by.String(), true

	case method == field:
		return field, "the field's own name", true

	case suffix(method, field):
		return field, spelling(method[len(method)-len(field):], field), true

	default:
		return "", "", false
	}
}

// suffix reports whether a name ends in a field, a whole word at a time.
//
// The words rather than the letters, or a field called ID would be found at the
// end of every name ending in a d — which is a derivation nobody made and an
// answer worse than none.
func suffix(method, field string) bool {
	if len(method) <= len(field) {
		return false
	}

	held, of := words.Words(method), words.Words(field)
	if len(held) <= len(of) {
		return false
	}

	for at := range of {
		if !strings.EqualFold(held[len(held)-len(of)+at], of[at]) {
			return false
		}
	}
	return true
}

// spelling says whether a name respelled the field it ends in.
func spelling(written, field string) string {
	if written == field {
		return ""
	}
	return words.FromInitialism.String()
}
