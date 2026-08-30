package model_test

import (
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/okian/forge/internal/model"
)

// layer builds a written stack entry for the named marker.
func layer(name string, kind model.Kind) model.LayerRef {
	return model.LayerRef{Origin: model.TypeRef{Pkg: markerPkg, Name: name}, Kind: kind}
}

// implicitLayer builds a stack entry resolution inferred rather than the author
// writing, as happens for the default storage beneath a lone refining layer.
func implicitLayer(name string, kind model.Kind) model.LayerRef {
	ref := layer(name, kind)
	ref.Implicit = true
	return ref
}

func nestedModel(t *testing.T) *model.Model {
	t.Helper()
	return &model.Model{
		Name:    "Persons",
		Form:    model.FormSpec,
		Subject: personStruct(t),
		Stack: []model.LayerRef{
			layer("Collection", model.KindRefining),
			layer("Ring", model.KindStorage),
			layer("Json", model.KindElement),
		},
		Options: []model.Options{
			{
				Layer:   "collection",
				Entries: []model.Option{{Key: "sort", Value: "Age,LastName"}, {Key: "index", Value: "Name"}},
			},
			{
				Layer:   "ring",
				Entries: []model.Option{{Key: "cap", Value: "1024"}, {Key: "overflow", Value: "overwrite"}},
			},
		},
		Pos: token.Position{Filename: "domain/spec.go", Line: 12, Column: 6},
	}
}

func TestModelOutermost(t *testing.T) {
	ref, ok := nestedModel(t).Outermost()
	if !ok {
		t.Fatal("Outermost() reported an empty stack")
	}
	if ref.Origin.Name != "Collection" {
		t.Errorf("Outermost() = %s, want the layer written first", ref)
	}

	if _, ok := (&model.Model{}).Outermost(); ok {
		t.Error("Outermost() on an empty stack reported a layer")
	}

	var nilModel *model.Model
	if _, ok := nilModel.Outermost(); ok {
		t.Error("Outermost() on a nil model reported a layer")
	}
}

func TestModelOptionsFor(t *testing.T) {
	subject := nestedModel(t)

	opts, ok := subject.OptionsFor("ring")
	if !ok {
		t.Fatal("OptionsFor(ring) reported absent")
	}
	if got, _ := opts.Get("cap"); got != "1024" {
		t.Errorf("cap = %q, want %q", got, "1024")
	}

	if _, ok := subject.OptionsFor("json"); ok {
		t.Error("OptionsFor(json) reported present")
	}

	var nilModel *model.Model
	if _, ok := nilModel.OptionsFor("ring"); ok {
		t.Error("OptionsFor on a nil model reported present")
	}
}

func TestModelLayout(t *testing.T) {
	cases := map[string]struct {
		subject   *model.Model
		want      string
		wantSpans []model.Span
	}{
		"nested stack": {
			subject: nestedModel(t),
			want:    "Collection[Ring[Json[Person]]]",
			wantSpans: []model.Span{
				{Offset: 0, Width: 10},
				{Offset: 11, Width: 4},
				{Offset: 16, Width: 4},
			},
		},
		"inferred storage contributes no text": {
			subject: &model.Model{
				Name:    "Persons",
				Subject: personStruct(t),
				Stack: []model.LayerRef{
					layer("Collection", model.KindRefining),
					implicitLayer("Slice", model.KindStorage),
				},
			},
			want: "Collection[Person]",
			wantSpans: []model.Span{
				{Offset: 0, Width: 10},
				{Offset: 11, Width: 0},
			},
		},
		"inferred storage between written entries": {
			subject: &model.Model{
				Name:    "Persons",
				Subject: personStruct(t),
				Stack: []model.LayerRef{
					layer("Collection", model.KindRefining),
					implicitLayer("Slice", model.KindStorage),
					layer("Json", model.KindElement),
				},
			},
			want: "Collection[Json[Person]]",
			wantSpans: []model.Span{
				{Offset: 0, Width: 10},
				{Offset: 11, Width: 0},
				{Offset: 11, Width: 4},
			},
		},
		"bare subject": {
			subject:   &model.Model{Name: "Persons", Subject: personStruct(t)},
			want:      "Person",
			wantSpans: []model.Span{},
		},
		"unresolved subject": {
			subject: &model.Model{
				Name:  "Persons",
				Stack: []model.LayerRef{layer("Collection", model.KindRefining)},
			},
			want:      "Collection[?]",
			wantSpans: []model.Span{{Offset: 0, Width: 10}},
		},
		"subject carrying no type": {
			subject: &model.Model{
				Name:    "Persons",
				Subject: &model.Struct{},
				Stack:   []model.LayerRef{layer("Collection", model.KindRefining)},
			},
			want:      "Collection[?]",
			wantSpans: []model.Span{{Offset: 0, Width: 10}},
		},
		// A rendered declaration is source, not identity: the import paths that
		// keep two instantiations apart have no place in a line an author reads.
		"instantiated subject": {
			subject: &model.Model{
				Name: "Pairs",
				Subject: instantiatedStruct(t, genericPair(t),
					types.Typ[types.String], namedStruct(t, subjectPkg, "domain", "Person")),
				Stack: []model.LayerRef{layer("Collection", model.KindRefining)},
			},
			want:      "Collection[Pair[string, Person]]",
			wantSpans: []model.Span{{Offset: 0, Width: 10}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			layout := tc.subject.Layout()
			if layout.Text != tc.want {
				t.Errorf("Text = %q, want %q", layout.Text, tc.want)
			}
			if len(layout.Spans) != len(tc.wantSpans) {
				t.Fatalf("got %d spans, want %d", len(layout.Spans), len(tc.wantSpans))
			}
			for i, want := range tc.wantSpans {
				if layout.Spans[i] != want {
					t.Errorf("Spans[%d] = %+v, want %+v", i, layout.Spans[i], want)
				}
			}
		})
	}

	var nilModel *model.Model
	if layout := nilModel.Layout(); layout.Text != "" || layout.Spans != nil {
		t.Errorf("Layout() on a nil model = %+v, want the zero value", layout)
	}
}

// Every span has to name the marker it claims to, or a diagnostic underlines
// the wrong layer of a nested stack.
func TestLayoutSpansLandOnTheirMarkerNames(t *testing.T) {
	subject := nestedModel(t)
	layout := subject.Layout()

	for i, ref := range subject.Stack {
		span := layout.Spans[i]
		got := layout.Text[span.Offset : span.Offset+span.Width]
		if got != ref.Origin.Name {
			t.Errorf("Spans[%d] covers %q, want %q", i, got, ref.Origin.Name)
		}
	}
}

func TestLayoutUnderline(t *testing.T) {
	subject := &model.Model{
		Name:    "Persons",
		Subject: personStruct(t),
		Stack: []model.LayerRef{
			layer("Collection", model.KindRefining),
			layer("Ring", model.KindStorage),
			layer("Heap", model.KindStorage),
		},
	}
	layout := subject.Layout()

	if got, want := layout.Text, "Collection[Ring[Heap[Person]]]"; got != want {
		t.Fatalf("Text = %q, want %q", got, want)
	}
	if got, want := layout.Underline(2), "                ^^^^"; got != want {
		t.Errorf("Underline(2) = %q, want %q", got, want)
	}

	// Nothing underlines an entry nobody wrote, or an entry that is not there.
	inferred := &model.Model{
		Subject: personStruct(t),
		Stack:   []model.LayerRef{implicitLayer("Slice", model.KindStorage)},
	}
	if got := inferred.Layout().Underline(0); got != "" {
		t.Errorf("Underline of an inferred entry = %q, want empty", got)
	}
	for _, i := range []int{-1, 3} {
		if got := layout.Underline(i); got != "" {
			t.Errorf("Underline(%d) = %q, want empty", i, got)
		}
	}
}

// Spans are byte offsets so that slicing the text works, while the caret line
// is drawn in characters so that it lines up on screen. A marker named with a
// non-ASCII letter is a legal Go identifier and separates the two.
func TestLayoutUnderlineCountsCharactersNotBytes(t *testing.T) {
	subject := &model.Model{
		Subject: personStruct(t),
		Stack: []model.LayerRef{
			layer("Größe", model.KindRefining),
			layer("Ring", model.KindStorage),
		},
	}
	layout := subject.Layout()

	if got, want := layout.Text, "Größe[Ring[Person]]"; got != want {
		t.Fatalf("Text = %q, want %q", got, want)
	}

	// "Größe" is six bytes and five characters, so a byte-counted caret would
	// start one column late and be one column too wide.
	if got, want := layout.Underline(1), "      ^^^^"; got != want {
		t.Errorf("Underline(1) = %q, want %q", got, want)
	}
	if got, want := layout.Underline(0), "^^^^^"; got != want {
		t.Errorf("Underline(0) = %q, want %q", got, want)
	}
}

// A stage that renders a declaration without a model of it still has to draw
// the same brackets, or one declaration reads two ways.
func TestOpenStack(t *testing.T) {
	cases := map[string]struct {
		stack     []model.LayerRef
		want      string
		wantOpen  int
		wantSpans []model.Span
	}{
		"nothing": {
			stack:     nil,
			want:      "",
			wantSpans: []model.Span{},
		},
		"written entries": {
			stack: []model.LayerRef{
				layer("Collection", model.KindRefining),
				layer("Ring", model.KindStorage),
			},
			want:      "Collection[Ring[",
			wantOpen:  2,
			wantSpans: []model.Span{{Offset: 0, Width: 10}, {Offset: 11, Width: 4}},
		},
		// The bracket count follows the text, not the entries: an inferred
		// entry writes no bracket, so counting entries would close one too many.
		"inferred entry": {
			stack: []model.LayerRef{
				layer("Collection", model.KindRefining),
				implicitLayer("Slice", model.KindStorage),
			},
			want:      "Collection[",
			wantOpen:  1,
			wantSpans: []model.Span{{Offset: 0, Width: 10}, {Offset: 11, Width: 0}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			text, spans, open := model.OpenStack(tc.stack)

			if text != tc.want {
				t.Errorf("text = %q, want %q", text, tc.want)
			}
			if open != tc.wantOpen {
				t.Errorf("open = %d, want %d", open, tc.wantOpen)
			}
			if !slices.Equal(spans, tc.wantSpans) {
				t.Errorf("spans = %+v, want %+v", spans, tc.wantSpans)
			}
		})
	}
}

// Layout and Span are exported, so a caller can hand Underline a span that came
// from somewhere other than Layout — and can draw one against text that never
// belonged to a Layout at all.
func TestLayoutUnderlineRejectsSpansOutsideTheText(t *testing.T) {
	layout := model.Layout{
		Text: "Collection[Person]",
		Spans: []model.Span{
			{Offset: -1, Width: 4},
			{Offset: 0, Width: 100},
		},
	}

	if got, want := (model.Span{Offset: 11, Width: 6}).Underline(layout.Text), "           ^^^^^^"; got != want {
		t.Errorf("Span.Underline() = %q, want %q", got, want)
	}

	for i := range layout.Spans {
		if got := layout.Underline(i); got != "" {
			t.Errorf("Underline(%d) = %q, want empty for a span outside the text", i, got)
		}
	}
}

func TestModelString(t *testing.T) {
	if got, want := nestedModel(t).String(), "Persons Collection[Ring[Json[Person]]]"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	// An unnamed model still renders its stack rather than a leading space.
	unnamed := &model.Model{Subject: personStruct(t)}
	if got, want := unnamed.String(), "Person"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	var nilModel *model.Model
	if got, want := nilModel.String(), "<nil model>"; got != want {
		t.Errorf("String() on a nil model = %q, want %q", got, want)
	}
}

func TestOptionsLookup(t *testing.T) {
	opts := model.Options{
		Layer: "collection",
		Entries: []model.Option{
			{Key: "sort", Value: "Age,LastName"},
			{Key: "primary"},
			{Key: "sort", Value: "Name"},
		},
	}

	entry, ok := opts.Lookup("sort")
	if !ok {
		t.Fatal("Lookup(sort) reported absent")
	}
	if entry.Value != "Age,LastName" {
		t.Errorf("Lookup(sort) = %q, want the first occurrence %q", entry.Value, "Age,LastName")
	}

	if _, ok := opts.Lookup("index"); ok {
		t.Error("Lookup(index) reported present")
	}

	// A key written on its own is present with an empty value, which is not the
	// same as being absent.
	value, ok := opts.Get("primary")
	if !ok || value != "" {
		t.Errorf("Get(primary) = %q, %v; want an empty value that is present", value, ok)
	}
}

func TestOptionsList(t *testing.T) {
	cases := map[string]struct {
		entries []model.Option
		key     string
		want    []string
	}{
		"multiple values":  {[]model.Option{{Key: "sort", Value: "Age,LastName"}}, "sort", []string{"Age", "LastName"}},
		"single value":     {[]model.Option{{Key: "index", Value: "Name"}}, "index", []string{"Name"}},
		"spaces trimmed":   {[]model.Option{{Key: "sort", Value: "Age, LastName "}}, "sort", []string{"Age", "LastName"}},
		"empty parts drop": {[]model.Option{{Key: "sort", Value: "Age,,"}}, "sort", []string{"Age"}},
		"absent key":       {nil, "sort", nil},
		"empty value":      {[]model.Option{{Key: "sort"}}, "sort", nil},
		"only separators":  {[]model.Option{{Key: "sort", Value: ",,"}}, "sort", nil},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := model.Options{Entries: tc.entries}.List(tc.key)
			if len(got) != len(tc.want) {
				t.Fatalf("List(%q) = %v, want %v", tc.key, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("List(%q)[%d] = %q, want %q", tc.key, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestOptionString(t *testing.T) {
	cases := map[string]struct {
		option model.Option
		want   string
	}{
		"key and value": {model.Option{Key: "cap", Value: "1024"}, "cap=1024"},
		"key alone":     {model.Option{Key: "primary"}, "primary"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.option.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// An option set renders as the directive that produced it, so an explain or a
// diagnostic can quote the author's own line back at them.
func TestOptionsString(t *testing.T) {
	opts, ok := nestedModel(t).OptionsFor("collection")
	if !ok {
		t.Fatal("OptionsFor(collection) reported absent")
	}
	if got, want := opts.String(), "//forge:collection sort=Age,LastName index=Name"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	if got, want := (model.Options{Layer: "clone"}).String(), "//forge:clone"; got != want {
		t.Errorf("String() with no entries = %q, want %q", got, want)
	}
}
