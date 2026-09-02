package words

import (
	"slices"
	"strconv"
)

// Scope is every name one generated package has taken.
//
// Collisions are not a projection problem, they are a package problem.
// Generated code shares one package block with the author's own declarations
// and with everything the other layers wrote, so a type, a constructor, a
// sentinel error and a pattern variable written by four layers can all land on
// one name — and a field and a method landing on one name does not compile at
// all. No layer concatenating its own identifiers can see any of that.
//
// So a layer asks for the name it wants and is told the name it gets. What
// comes back is the same on every run given the same input, because nothing
// here depends on the order the layers ran in beyond the order they asked in,
// which is the stack's own order.
//
// A collision is a refusal rather than a suffixed guess. There is no directive
// an author can write to break a tie, which makes the report the whole remedy:
// it names what wanted the name, what already holds it, and what to do. A
// generated NewPersons2 would be a name nobody asked for, in a file nobody can
// edit, and the author would find out from a call site rather than from forge.
//
// The zero value is a package that has taken nothing, which is what a run
// starts with before the author's own declarations are reserved into it.
type Scope struct {
	// pkg is the package block: types, functions, variables, constants.
	pkg []claim

	// members are the names each type has taken. One set per type, because a
	// field and a method of one name is a compile failure and a field of one
	// type and a method of another is nothing at all.
	members []memberSet
}

// claim is one name and what holds it, in the words a report prints.
type claim struct{ name, by string }

// memberSet is one type's fields and methods together, which is the namespace
// the compiler keeps them in.
type memberSet struct {
	typ  string
	held []claim
}

// Reserve records a name the package already declares.
//
// The author's own first, before any layer writes: generated code lands beside
// what they wrote, and a layer that did not know about their NewPersons will
// write a second one. Then whatever a layer means to write verbatim — the one
// route to a name [Declare] refuses, and the way a layer says that String is
// the method it meant rather than a name it derived by accident.
//
// Reserving the same name twice is a clash, because it is two declarations of
// it. Reserving is how forge finds that out before writing the file rather than
// after.
func (s *Scope) Reserve(name string) error { return s.hold(&s.pkg, name, "already declared") }

// ReserveMember records a name one type already carries, whether as a field or
// as a method.
func (s *Scope) ReserveMember(typ, name string) error {
	return s.hold(s.set(typ), name, "already on "+typ)
}

// Declare returns the name a package-level declaration gets, and refuses where
// it cannot have one.
//
// The kind decides the shape and the visibility decides the case, which is
// [Spell]'s arrangement; what this adds is that the answer is not one anything
// else has taken.
func (s *Scope) Declare(kind Kind, exported bool, parts ...string) (string, error) {
	name := Spell(kind, exported, parts...)
	if err := s.hold(&s.pkg, name, "generated"); err != nil {
		return "", err
	}
	return name, nil
}

// Member returns the name a field or a method of one type gets.
//
// Fields and methods together, because that is the namespace the compiler keeps
// them in: a struct with a field Age cannot also have a method Age, and the
// failure is a redeclaration in a file the author did not write. Any layer that
// adds a method to a struct it also gives fields to has to ask one place.
//
// A name the standard library has already given a meaning to is refused here
// and nowhere else, because it is a method name that only matters on a method.
// A layer that means to write String or Error says so with [ReserveMember]
// first; what this catches is the layer that derived one from a field and did
// not notice.
func (s *Scope) Member(typ string, kind Kind, exported bool, parts ...string) (string, error) {
	name := Spell(kind, exported, parts...)

	if meaning, is := Standard(name); is {
		return "", &ClashError{Name: name, Why: "is " + meaning, Hint: standardHint}
	}
	if err := s.hold(s.set(typ), name, "generated for "+typ); err != nil {
		return "", err
	}
	return name, nil
}

// Taken reports whether the package block already holds a name, which is what a
// layer asks when it wants to know rather than to claim.
func (s *Scope) Taken(name string) bool {
	return slices.ContainsFunc(s.pkg, func(one claim) bool { return one.name == name })
}

// TakenMember reports whether a type already carries a member under this name.
func (s *Scope) TakenMember(typ, name string) bool {
	return slices.ContainsFunc(*s.set(typ), func(one claim) bool { return one.name == name })
}

// hold puts a name into a set, or says why it cannot go there.
func (s *Scope) hold(into *[]claim, name, by string) error {
	if reason, ok := Safe(name); !ok {
		return &ClashError{Name: name, Why: reason, Hint: unsafeHint}
	}

	if at := slices.IndexFunc(*into, func(one claim) bool { return one.name == name }); at >= 0 {
		return &ClashError{Name: name, Why: "is taken", Held: (*into)[at].by, Hint: takenHint}
	}

	*into = append(*into, claim{name: name, by: by})
	return nil
}

// set returns the member set of a type, making one where the type has none yet.
func (s *Scope) set(typ string) *[]claim {
	at := slices.IndexFunc(s.members, func(one memberSet) bool { return one.typ == typ })
	if at < 0 {
		s.members = append(s.members, memberSet{typ: typ})
		at = len(s.members) - 1
	}
	return &s.members[at].held
}

// The three sentences a refusal ends with, kept together so that two of them
// cannot drift into saying the same thing differently.
const (
	unsafeHint   = "name the declaration something else"
	takenHint    = "rename one of the two, or drop the layer that wrote the second"
	standardHint = "reserve the name before asking for it where that is the method you meant"
)

// ClashError is a name a generated package cannot take, and why.
//
// It carries what a diagnostic has to print rather than a sentence, so that the
// caller can put its own position and its own code around it: the layer knows
// where the declaration was written and this knows nothing about that.
type ClashError struct {
	// Name is the name that was asked for.
	Name string

	// Why says what stops it, in lower case and without terminating
	// punctuation, so that it composes into a longer sentence.
	Why string

	// Held says what already holds the name, and is empty where nothing does —
	// a keyword is refused without anything having claimed it.
	Held string

	// Hint says what to do about it.
	Hint string
}

// Error returns the whole refusal as one sentence.
func (c *ClashError) Error() string {
	out := c.Name + " " + c.Why
	if c.Held != "" {
		out += " (" + c.Held + ")"
	}
	if c.Hint != "" {
		out += ": " + c.Hint
	}
	return out
}

// Block is the names visible inside one function body: what the file imports,
// the receiver, the parameters, and whatever the body has bound already.
//
// Separate from the package block because the answer is different. A local that
// collides can be renamed — nothing outside the function can see it, so the
// rename costs a reader nothing — where a package-level name that collides is
// part of somebody's API and renaming it silently is how a generated package
// stops compiling somewhere else.
//
// What it is for beyond uniqueness is shadowing. A local named slices in a file
// that imports slices does not fail to compile; it fails on the next line that
// meant the package, in generated code the author cannot edit.
type Block struct {
	taken []string
}

// Block returns a scope for one function body, given the names already visible
// in it.
func (s *Scope) Block(taken ...string) *Block {
	return &Block{taken: slices.Clone(taken)}
}

// Declare returns the name a local gets, renaming it where the name it would
// have had is already visible.
//
// Numbered rather than decorated, and counting from two, because the result is
// read in generated code: held2 says it is the second held and nothing else.
// Never fails, which is the difference from [Scope.Declare] — a local always
// has another name available and taking one costs nobody anything.
func (b *Block) Declare(parts ...string) string {
	name := Spell(KindLocal, false, parts...)
	if name == "" {
		name = "v"
	}

	if b.free(name) {
		b.taken = append(b.taken, name)
		return name
	}

	for n := 2; ; n++ {
		if candidate := name + strconv.Itoa(n); b.free(candidate) {
			b.taken = append(b.taken, candidate)
			return candidate
		}
	}
}

// Shadows reports whether a name is one the block already binds or the file
// already imports, which is the question a layer asks about a name it did not
// get from [Block.Declare].
func (b *Block) Shadows(name string) bool { return !b.free(name) }

// free reports whether a name is available in the block.
func (b *Block) free(name string) bool {
	if _, ok := Safe(name); !ok {
		return false
	}
	return !slices.Contains(b.taken, name)
}
