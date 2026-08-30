package model

import (
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/okian/forge/internal/tags"
)

// Class is the coarse shape of a field's type: the distinction every layer
// branches on before it looks at anything finer.
//
// A named type is ClassNamed whatever its underlying type is, so a named
// struct and a named integer share a class. Layers that care about the
// difference read [Classified.Type]; layers that only need to know whether a
// value can be recursed into read the class.
type Class uint8

const (
	// ClassInvalid is the zero value: the type has not been classified.
	ClassInvalid Class = iota

	// ClassBasic is a predeclared type — a boolean, a numeric type, a string,
	// or unsafe.Pointer.
	ClassBasic

	// ClassNamed is a defined type, whatever its underlying type is.
	ClassNamed

	// ClassPointer is a pointer. Elem holds what it points to.
	ClassPointer

	// ClassSlice is a slice. Elem holds the element type.
	ClassSlice

	// ClassArray is an array. Elem holds the element type and Len its length.
	ClassArray

	// ClassMap is a map. Key and Elem hold its two halves.
	ClassMap

	// ClassStruct is an unnamed struct written in place.
	ClassStruct

	// ClassInterface is an interface, including any. A statically generated
	// codec cannot see through one, which is why it is called out rather than
	// folded into ClassNamed.
	ClassInterface

	// ClassChan is a channel. Nothing meaningful can be generated for one, so
	// layers report it rather than skipping it silently.
	ClassChan

	// ClassFunc is a function type, and carries the same caveat as ClassChan.
	ClassFunc
)

// classNames gives each class the spelling used in diagnostics.
var classNames = [...]string{
	ClassInvalid:   "invalid",
	ClassBasic:     "basic",
	ClassNamed:     "named",
	ClassPointer:   "pointer",
	ClassSlice:     "slice",
	ClassArray:     "array",
	ClassMap:       "map",
	ClassStruct:    "struct",
	ClassInterface: "interface",
	ClassChan:      "chan",
	ClassFunc:      "func",
}

// String returns the class's lower-case name.
func (c Class) String() string {
	if int(c) >= len(classNames) {
		return "class(" + strconv.Itoa(int(c)) + ")"
	}
	return classNames[c]
}

// Valid reports whether the class is one of the defined classes.
func (c Class) Valid() bool { return c != ClassInvalid && int(c) < len(classNames) }

// Classified describes the type of a field: its class, the type itself, and
// the parts a layer would otherwise have to take apart again.
//
// Unnamed composites nest, so map[string][]*Person is reachable through Key
// and Elem without unwrapping it a second time. A named type is where that
// stops: ClassNamed says nothing about the shape underneath, because the name
// is the interesting part, and a layer that needs the structure of a named
// slice or map goes through [Classified.Type] to get it.
type Classified struct {
	// Class is the type's coarse shape.
	Class Class

	// Type is the type exactly as declared, kept so that a layer needing more
	// than the class has it to hand.
	Type types.Type

	// Ref identifies a named type. It is the zero value for every other class.
	Ref TypeRef

	// Elem is what a pointer points to, or the element of an unnamed slice,
	// array, map or channel. It is nil for every other class.
	Elem *Classified

	// Key is an unnamed map's key type, nil otherwise.
	Key *Classified

	// Len is an array's length, zero otherwise.
	Len int64
}

// String renders the type as it would be written, qualified by package name
// rather than by import path, which is what a reader of a diagnostic expects
// to see.
func (c Classified) String() string {
	if c.Type == nil {
		return "<unclassified>"
	}
	return types.TypeString(c.Type, packageNameQualifier)
}

// packageNameQualifier spells a type's package by name, for output a human
// reads.
func packageNameQualifier(p *types.Package) string { return p.Name() }

// packagePathQualifier spells a type's package by import path, for identities
// that have to stay unique across packages that share a name.
func packagePathQualifier(p *types.Package) string { return p.Path() }

// noQualifier names no package at all.
func noQualifier(*types.Package) string { return "" }

// TypeString spells a type the way a rendered declaration spells it: as
// [types.TypeString] would, with every package qualifier dropped, so that a
// stack and the type at the bottom of it read as one line of source rather
// than as two vocabularies. A nil type renders as a question mark, since a
// rendering that panics is no use where renderings are reached for.
//
// The nil it accepts is the interface's own. A nil *types.Named handed over as
// a types.Type is not nil at all, and is the one value here that panics rather
// than renders — which is what [Struct.Type] exists to keep out of it.
//
// This is a spelling and not an identity. Two types of the same name in
// different packages spell alike here, which is what a reader of a declaration
// wants and what [TypeRef] exists to prevent when it is not.
func TypeString(t types.Type) string {
	if t == nil {
		return "?"
	}
	return types.TypeString(t, noQualifier)
}

// Field is one field of the subject, or of a struct reachable from it.
type Field struct {
	// Name is the field's identifier. For an embedded field it is the name Go
	// gives the field, which is the embedded type's own name.
	Name string

	// Type is the field's classified type.
	Type Classified

	// Tags holds the field's parsed struct tags in the order they were written.
	// Look one up with [Field.Tag].
	Tags []tags.Tag

	// Embedded records that the field was written without a name, so its own
	// fields are promoted into the enclosing struct.
	Embedded bool

	// Exported records that the field is visible outside its package. An
	// unexported field of a struct declared elsewhere cannot be read or written
	// by generated code at all.
	Exported bool

	// Implements lists the interfaces the field's type already satisfies,
	// sorted by [TypeRef.Less]. A layer whose interface appears here delegates
	// to what is already there rather than generating a second implementation,
	// which is how a hand-written codec stays authoritative. It is a list
	// rather than a flag because a type may carry a JSON codec and no binary
	// one, and the two layers must not have to guess.
	Implements []TypeRef

	// External records that the field's type is declared outside this module,
	// so no method may be attached to it.
	External bool

	// Pos is the position of the field's declaration, which is where a
	// diagnostic about this field points. For a field of a struct declared in
	// another package that is a position in another file, which is exactly why
	// it is recorded rather than recomputed.
	Pos token.Position
}

// Tag returns the field's parsed tag written under key, and whether it carries
// one.
func (f Field) Tag(key string) (tags.Tag, bool) {
	for _, tag := range f.Tags {
		if tag.Key == key {
			return tag, true
		}
	}
	return tags.Tag{}, false
}

// TagKeys returns the struct tag keys the field carries, in written order.
func (f Field) TagKeys() []string {
	keys := make([]string, len(f.Tags))
	for i, tag := range f.Tags {
		keys[i] = tag.Key
	}
	return keys
}

// Satisfies reports whether the field's type already implements iface.
func (f Field) Satisfies(iface TypeRef) bool { return slices.Contains(f.Implements, iface) }

// String returns the field as it reads in a struct definition, "Name Type".
func (f Field) String() string {
	return f.Name + " " + f.Type.String()
}

// Struct is the model of one struct type: the subject of a declaration, or a
// struct reachable from it that needs generated code of its own.
type Struct struct {
	// Named is the struct's type as the loader saw it.
	Named *types.Named

	// Fields holds the struct's own fields in declaration order, embedded ones
	// included but their promoted fields not. Generated output follows this
	// order, so a field added in the middle of a struct produces a diff in the
	// middle of the generated file.
	Fields []Field

	// Closure lists every struct transitively reachable from this one that
	// needs generated code, in a deterministic order and without repeats. A
	// struct never appears in its own closure.
	Closure []*Struct

	// Implements lists the interfaces the struct already satisfies, sorted by
	// [TypeRef.Less], with the same meaning it has on a field: a layer whose
	// interface is already implemented emits nothing rather than a duplicate
	// method, which would not compile.
	Implements []TypeRef

	// Cyclic records that the struct reaches itself. Layers that walk the
	// closure must terminate on this rather than recursing.
	Cyclic bool

	// External records that the struct is declared outside this module. No
	// method can be attached to it, so element layers emit standalone functions
	// in the local package instead.
	External bool

	// Instantiated records that the struct is an instantiation of a generic
	// type. Go cannot attach a method to an instantiation, so it takes the same
	// standalone path as an external struct.
	Instantiated bool

	// Pos is the position of the struct's declaration, which is where a
	// diagnostic about the struct itself points.
	Pos token.Position
}

// Ref returns the struct's identity, or the zero value if it has none yet.
//
// An instantiation carries its type arguments, so that Pair[string, int] and
// Pair[string, bool] are two identities and not one. Arguments are spelled by
// import path, since two packages may share a name.
func (s *Struct) Ref() TypeRef {
	if s == nil || s.Named == nil {
		return TypeRef{}
	}

	obj := s.Named.Obj()
	ref := TypeRef{Name: obj.Name()}
	if pkg := obj.Pkg(); pkg != nil {
		ref.Pkg = pkg.Path()
	}

	if args := s.Named.TypeArgs(); args != nil && args.Len() > 0 {
		var b strings.Builder
		b.WriteByte('[')
		for i := range args.Len() {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(types.TypeString(args.At(i), packagePathQualifier))
		}
		b.WriteByte(']')
		ref.Args = b.String()
	}

	return ref
}

// Type returns the struct's type, or nil when it has none yet.
//
// It exists so that a caller cannot turn a struct with no type into a non-nil
// types.Type holding a nil pointer. That value renders as a panic rather than
// as a name, and the compiler has no warning for it, so the conversion is done
// once here rather than at every call site that has to remember.
func (s *Struct) Type() types.Type {
	if s == nil || s.Named == nil {
		return nil
	}
	return s.Named
}

// Local reports whether a method may be attached to the struct. When it cannot,
// element layers emit package-level functions instead, and callers above the
// subject never see the difference.
func (s *Struct) Local() bool {
	return s != nil && s.Named != nil && !s.External && !s.Instantiated
}

// Satisfies reports whether the struct already implements iface.
func (s *Struct) Satisfies(iface TypeRef) bool {
	return s != nil && slices.Contains(s.Implements, iface)
}

// Field returns the field written under name, and whether the struct has one.
// Only the struct's own fields are searched; a field promoted from an embedded
// struct is found through that struct.
func (s *Struct) Field(name string) (Field, bool) {
	if s == nil {
		return Field{}, false
	}
	for _, field := range s.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return Field{}, false
}

// FieldNames returns the names of the struct's own fields, in declaration
// order. An embedded field contributes its type's name, not the names it
// promotes.
func (s *Struct) FieldNames() []string {
	if s == nil {
		return nil
	}
	names := make([]string, len(s.Fields))
	for i, field := range s.Fields {
		names[i] = field.Name
	}
	return names
}

// String returns the struct's qualified name, or a placeholder when it has
// none yet.
func (s *Struct) String() string {
	ref := s.Ref()
	if ref.IsZero() {
		return "<unresolved struct>"
	}
	return ref.String()
}
