package templates_test

import (
	"strings"
	"testing"

	"github.com/okian/forge/internal/templates"
)

// ordinary is the rewrite these tests vary one thing at a time from.
var ordinary = templates.Rewrite{
	Param: "T", Subject: "Person",
	Container: "Collection", Declared: "Persons",
	Prefix: "persons",
}

// refused specialises a template and returns what was said about it.
func refused(t *testing.T, source string, r templates.Rewrite) string {
	t.Helper()

	_, diags := templates.Apply(templates.Template{Name: "toy", Source: []byte(source)}, r, where)
	return diags.Render()
}

// A rewrite that does not say what it is rewriting into cannot be carried out,
// and carrying out half of one would produce a file naming a type that is
// partly the template's.
func TestARewriteThatSaysTooLittle(t *testing.T) {
	const source = "package tmpl\n\ntype Collection[T any] []T\n"

	cases := map[string]templates.Rewrite{
		"no type parameter":                    {Subject: "Person", Container: "Collection", Declared: "Persons"},
		"no subject":                           {Param: "T", Container: "Collection", Declared: "Persons"},
		"no container":                         {Param: "T", Subject: "Person", Declared: "Persons"},
		"no declared name":                     {Param: "T", Subject: "Person", Container: "Collection"},
		"a parameter named like the container": {Param: "T", Subject: "Person", Container: "T", Declared: "Persons"},
	}

	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			reported := refused(t, source, r)

			if !strings.Contains(reported, "FRG4911") {
				t.Errorf("an incomplete rewrite was carried out:\n%s", reported)
			}
			// A fault in forge is not one an author can fix, and saying so is
			// the difference between a report and a wild goose chase.
			if !strings.Contains(reported, "fault in forge") {
				t.Errorf("the failure does not say whose fault it is:\n%s", reported)
			}
		})
	}
}

// A template that does not parse is a fault in forge's own source, reported
// against the declaration that asked because that is what its author was doing.
func TestATemplateThatDoesNotParse(t *testing.T) {
	reported := refused(t, "package tmpl\n\ntype Collection[T any] struct {\n", ordinary)

	if !strings.Contains(reported, "FRG4910") {
		t.Errorf("a template that does not parse was rewritten:\n%s", reported)
	}
	if !strings.Contains(reported, "model/spec.go:8:6") {
		t.Errorf("the failure does not point at the declaration that asked:\n%s", reported)
	}
}

// A template with no container is not a template for this declaration, and
// rewriting it would emit methods on a type nothing declares.
func TestATemplateWithNoContainer(t *testing.T) {
	reported := refused(t, "package tmpl\n\ntype Other[T any] []T\n", ordinary)

	if !strings.Contains(reported, "no type called Collection") {
		t.Errorf("the failure does not say what was missing:\n%s", reported)
	}
}

// A helper needs a prefix, because generated code lands in a package that has
// its own names and a collision is a build failure in a file its author did not
// write.
func TestAHelperWithNoPrefixToTakeIt(t *testing.T) {
	const source = "package tmpl\n\ntype Collection[T any] []T\n\nfunc each[T any](c Collection[T]) {}\n"

	r := ordinary
	r.Prefix = ""

	reported := refused(t, source, r)
	if !strings.Contains(reported, "no prefix was given") {
		t.Errorf("a helper was emitted with the name the template gave it:\n%s", reported)
	}
}

// A template declaring nothing but its container needs no prefix, since there
// is nothing left to collide.
func TestATemplateWithNothingToPrefix(t *testing.T) {
	const source = "package tmpl\n\ntype Collection[T any] []T\n\nfunc (c Collection[T]) Len() int { return len(c) }\n"

	r := ordinary
	r.Prefix = ""

	if reported := refused(t, source, r); reported != "" {
		t.Errorf("a template with nothing to prefix was refused:\n%s", reported)
	}
}

// Rewriting is done by name, so a template that uses one of its own
// package-level names for something else would have that something else
// renamed too — silently, into code that means something different.
func TestATemplateThatReusesItsOwnName(t *testing.T) {
	cases := map[string]string{
		"as a parameter": "package tmpl\n\ntype Collection[T any] []T\n\n" +
			"type counted struct{ n int }\n\n" +
			"func (c Collection[T]) Do(counted int) int { return counted }\n",

		"as a local": "package tmpl\n\ntype Collection[T any] []T\n\n" +
			"type counted struct{ n int }\n\n" +
			"func (c Collection[T]) Do() int { counted := 1; return counted }\n",

		"as a field": "package tmpl\n\ntype Collection[T any] []T\n\n" +
			"type counted struct{ n int }\n\n" +
			"type holder struct{ counted int }\n",

		"as a label": "package tmpl\n\ntype Collection[T any] []T\n\n" +
			"type counted struct{ n int }\n\n" +
			"func (c Collection[T]) Do() {\ncounted:\n\tfor range c {\n\t\tbreak counted\n\t}\n}\n",
	}

	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			reported := refused(t, source, ordinary)

			if !strings.Contains(reported, "FRG4912") {
				t.Errorf("a name used two ways was rewritten anyway:\n%s", reported)
			}
			if !strings.Contains(reported, "counted") {
				t.Errorf("the failure does not name the collision:\n%s", reported)
			}
		})
	}
}

// A package-level variable is a name like any other, and its own declaration is
// not a second use of it.
func TestATemplateWithAPackageLevelVariable(t *testing.T) {
	const source = "package tmpl\n\ntype Collection[T any] []T\n\n" +
		"var empty = 0\n\n" +
		"func (c Collection[T]) Len() int { return len(c) + empty }\n"

	out, diags := templates.Apply(templates.Template{Name: "toy", Source: []byte(source)}, ordinary, where)
	if !diags.Empty() {
		t.Fatalf("a template with a variable was refused:\n%s", diags.Render())
	}

	text := rendered(t, out)
	if !strings.Contains(text, "var personsEmpty = 0") {
		t.Errorf("the variable did not take the prefix:\n%s", text)
	}
	if !strings.Contains(text, "+ personsEmpty") {
		t.Errorf("the use did not follow the declaration:\n%s", text)
	}
}

// A blank name and an init function are names nothing can refer to, so neither
// can collide with anything in the package the output lands in.
func TestNamesNothingCanReferTo(t *testing.T) {
	const source = "package tmpl\n\ntype Collection[T any] []T\n\n" +
		"var _ = 0\n\nfunc init() {}\n"

	r := ordinary
	r.Prefix = ""

	out, diags := templates.Apply(templates.Template{Name: "toy", Source: []byte(source)}, r, where)
	if !diags.Empty() {
		t.Fatalf("a template with a blank and an init was refused:\n%s", diags.Render())
	}

	text := rendered(t, out)
	for _, want := range []string{"var _ = 0", "func init()"} {
		if !strings.Contains(text, want) {
			t.Errorf("the output does not hold %q:\n%s", want, text)
		}
	}
}

// A function generic over something the subject does not answer for stays
// generic over it: a view over U is a view over U whatever the element is.
func TestAParameterThatIsNotTheElement(t *testing.T) {
	const source = "package tmpl\n\ntype Collection[T any] []T\n\n" +
		"func mapped[T, U any](c Collection[T], f func(T) U) []U {\n" +
		"\tout := make([]U, 0, len(c))\n\tfor _, v := range c {\n\t\tout = append(out, f(v))\n\t}\n\treturn out\n}\n"

	out, diags := templates.Apply(templates.Template{Name: "toy", Source: []byte(source)}, ordinary, where)
	if !diags.Empty() {
		t.Fatalf("a template with a second parameter was refused:\n%s", diags.Render())
	}

	text := rendered(t, out)
	if !strings.Contains(text, "func personsMapped[U any](c Persons, f func(Person) U) []U") {
		t.Errorf("the second parameter did not survive:\n%s", text)
	}
}

// A template with no declarations at all has nothing to say, and saying so is
// better than emitting an empty file.
func TestATemplateWithNothingInIt(t *testing.T) {
	reported := refused(t, "package tmpl\n", ordinary)

	if !strings.Contains(reported, "no type called Collection") {
		t.Errorf("an empty template was rewritten:\n%s", reported)
	}
}

// An import written under another name keeps it, since the bodies refer to the
// package by the name the template gave it.
func TestAnImportWrittenUnderAnotherName(t *testing.T) {
	const source = "package tmpl\n\nimport seq \"iter\"\n\n" +
		"type Collection[T any] []T\n\n" +
		"func (c Collection[T]) All() seq.Seq[T] { return nil }\n"

	out, diags := templates.Apply(templates.Template{Name: "toy", Source: []byte(source)}, ordinary, where)
	if !diags.Empty() {
		t.Fatalf("a renamed import was refused:\n%s", diags.Render())
	}

	// The name as well as the path: the bodies call the package by the name the
	// template gave it, and an import block that wrote only the path would
	// leave them naming a package that is not there.
	if want := `seq "iter"`; written(out.Imports) != want {
		t.Errorf("the imports are %s, want %s", written(out.Imports), want)
	}
	if text := rendered(t, out); !strings.Contains(text, "seq.Seq[Person]") {
		t.Errorf("the name the template used was rewritten:\n%s", text)
	}
}

// A dot import and a blank one carry no name for the bodies to use, and a
// template that wrote one meant the package to be there.
func TestImportsWithNoNameToUse(t *testing.T) {
	const source = "package tmpl\n\nimport (\n\t_ \"embed\"\n\t\"iter\"\n)\n\n" +
		"type Collection[T any] []T\n\n" +
		"func (c Collection[T]) All() iter.Seq[T] { return nil }\n"

	out, diags := templates.Apply(templates.Template{Name: "toy", Source: []byte(source)}, ordinary, where)
	if !diags.Empty() {
		t.Fatalf("a blank import was refused:\n%s", diags.Render())
	}

	if want := `_ "embed","iter"`; written(out.Imports) != want {
		t.Errorf("the imports are %s, want %s", written(out.Imports), want)
	}
}

// A template with no doc comment on its first declaration keeps the comments
// that come after it, since what is left behind is the one about the template
// rather than everything before the first thing it declares.
func TestATemplateWhoseFirstDeclarationIsUndocumented(t *testing.T) {
	const source = "// Package tmpl holds bodies.\npackage tmpl\n\n" +
		"type Collection[T any] []T\n\n" +
		"// Len reports how many.\nfunc (c Collection[T]) Len() int { return len(c) }\n"

	out, diags := templates.Apply(templates.Template{Name: "toy", Source: []byte(source)}, ordinary, where)
	if !diags.Empty() {
		t.Fatalf("the template was refused:\n%s", diags.Render())
	}

	text := rendered(t, out)
	if !strings.Contains(text, "// Len reports how many.") {
		t.Errorf("a comment after the first declaration was lost:\n%s", text)
	}
	if strings.Contains(text, "Package tmpl") {
		t.Errorf("the template's own package comment was carried through:\n%s", text)
	}
}

// A name already opening in upper case is prefixed without being changed
// further, since a prefix is joined to what the template wrote.
func TestAPrefixedNameThatAlreadyOpensInUpperCase(t *testing.T) {
	const source = "package tmpl\n\ntype Collection[T any] []T\n\ntype Holder struct{ n int }\n"

	out, diags := templates.Apply(templates.Template{Name: "toy", Source: []byte(source)}, ordinary, where)
	if !diags.Empty() {
		t.Fatalf("the template was refused:\n%s", diags.Render())
	}

	if text := rendered(t, out); !strings.Contains(text, "type personsHolder struct") {
		t.Errorf("the name was not prefixed as written:\n%s", text)
	}
}
