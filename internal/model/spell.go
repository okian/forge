package model

import (
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// Import is one import a spelling depends on: the path, and the name the
// spelling calls it by.
//
// The name is carried because it cannot be recovered from the path. An import
// binds a package to the name that package declares, which is not always the
// last element of its path — and a package whose name is already taken in the
// file being written is bound to something else again, which nothing but the
// spelling that chose it knows.
type Import struct {
	// Path is the import path.
	Path string

	// Name is what the spelling calls the package. It is the package's own name
	// unless that was taken.
	Name string

	// Aliased records that Name is not the package's own name, so a file
	// writing this import has to bind it explicitly. A file that wrote every
	// import under a name would read like nothing anybody would hand-write, and
	// this is the one bit a reader of Path and Name cannot recover: the two look
	// alike for a package whose name is the last element of its path and for one
	// that was renamed to exactly that.
	Aliased bool
}

// Spelling is a type written the way generated code in one package has to write
// it, together with what that package must import for the writing to resolve.
//
// The two are one answer and are returned as one. A caller that took the text
// and worked the imports out for itself would be re-deriving, from a string,
// something the type already knows — and would get it wrong for exactly the
// types that make it worth knowing: an instantiation names packages that its
// own name does not mention, so Pair[time.Time, uuid.UUID] needs two imports
// and reads as though it needed none.
type Spelling struct {
	// Text is the type as it must be written: a bare name for a type declared
	// in the package being generated into, and a qualified one otherwise.
	Text string

	// Imports holds what the text depends on, ordered by path and without
	// repeats. It is empty when the text names nothing outside its own package.
	Imports []Import
}

// String returns the spelling's text, so that a spelling can be printed where a
// name is wanted.
func (s Spelling) String() string { return s.Text }

// Names returns the names the spelling binds, which is what a caller spelling a
// second type has to hand back as taken.
func (s Spelling) Names() []string {
	out := make([]string, len(s.Imports))
	for i, one := range s.Imports {
		out[i] = one.Name
	}
	return out
}

// Spell writes a type as generated code in the package at local has to write
// it, and reports what that package must import.
//
// Qualified by package *name* rather than by import path, because that is what
// Go source says: an import binds a package to its declared name, which is not
// always the last element of its path, and a file importing example.com/mod/v2
// writes mod.Person. That is also why the imports come back separately — the
// text cannot be taken apart to recover them.
//
// A type from the local package is written bare. Generated code lives in the
// package it is generated for, where a self-import is not a thing that exists.
//
// The taken names are the ones the file already binds to something else — the
// packages whatever else is being written into it imports. A package whose own
// name is among them is bound to a fresh one and spelled by that, because the
// alternative is a file that imports two packages under one name: it does not
// compile, and the failure is in generated code the author cannot edit, about a
// collision they caused by naming a package slices.
func Spell(t types.Type, local string, taken []string) Spelling {
	if t == nil {
		return Spelling{Text: "?"}
	}

	var imports []Import
	qualify := func(p *types.Package) string {
		// A package with no name cannot be qualified by one, so the type is
		// spelled bare and nothing is imported for it. Recording the import
		// anyway would put a line in the file that nothing in the file names.
		if p == nil || p.Path() == local || p.Name() == "" {
			return ""
		}

		if at := slices.IndexFunc(imports, func(one Import) bool { return one.Path == p.Path() }); at >= 0 {
			return imports[at].Name
		}

		bound := free(p.Name(), taken, imports)
		imports = append(imports, Import{Path: p.Path(), Name: bound, Aliased: bound != p.Name()})

		return bound
	}

	text := types.TypeString(t, qualify)
	slices.SortFunc(imports, func(a, b Import) int { return strings.Compare(a.Path, b.Path) })

	return Spelling{Text: text, Imports: imports}
}

// free returns a name to bind a package to: its own where that is available,
// and its own with a number after it where it is not.
//
// Numbered rather than decorated, because the result is read in generated code
// and slices2.Person says what it is. Counting from two reads as the second
// package of that name, which is what it is.
func free(name string, taken []string, bound []Import) string {
	claimed := func(candidate string) bool {
		return slices.Contains(taken, candidate) ||
			slices.ContainsFunc(bound, func(one Import) bool { return one.Name == candidate })
	}

	if !claimed(name) {
		return name
	}
	for n := 2; ; n++ {
		if candidate := name + strconv.Itoa(n); !claimed(candidate) {
			return candidate
		}
	}
}

// SubjectSpelling writes the declaration's subject the way the generated file
// has to write it, avoiding the names that file already binds.
//
// It is here rather than in the layer that needs it because every layer needs
// it and they must all agree: two layers spelling one subject differently would
// each be right about their own half of a file that does not compile.
func (m *Model) SubjectSpelling(taken []string) Spelling {
	if m == nil {
		return Spelling{Text: "?"}
	}

	local := ""
	if m.Pkg != nil {
		local = m.Pkg.PkgPath
	}

	return Spell(m.Subject.Type(), local, taken)
}
