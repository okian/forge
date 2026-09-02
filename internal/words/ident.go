package words

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kind is what an identifier names, which is what decides its shape.
//
// The kind and the visibility are two separate questions and are asked
// separately: what a name looks like comes from the thing it names, and whether
// it is exported comes from the declaration it hangs off. A constructor is New
// plus the type in one package and new plus the type in another, and it is the
// same rule either way.
type Kind uint8

// The kinds of identifier forge writes.
const (
	// KindType names a defined type: a noun, singular, with no Type, Struct or
	// Impl suffix and no I prefix.
	KindType Kind = iota

	// KindInterface names an interface after the one method it holds, with the
	// -er ending English gives an agent noun. See [Agent].
	KindInterface

	// KindMethod names a method on a type.
	//
	// No Get prefix: the accessor is Name and the mutator SetName. A boolean
	// answer reads as a question, which is [Question]. And an iterator takes
	// one of the names Go settled on for iter.Seq — All for the whole
	// sequence, Values, Keys, Backward — because a generated iterator called
	// something else is one no reader will look for.
	KindMethod

	// KindFunc names a package-level function.
	KindFunc

	// KindField names a field of a struct.
	KindField

	// KindVar names a package-level or block-level variable.
	KindVar

	// KindConst names a constant, which for an enumeration member carries the
	// type: StatusActive rather than ACTIVE or Status_Active.
	KindConst

	// KindError names a sentinel error after what refused: ErrPersonsFull.
	KindError

	// KindReceiver names the receiver of a method: one or two letters taken
	// from the type's own name, the same for every method of that type.
	KindReceiver

	// KindLocal names a variable inside a function body. Never exported,
	// whatever it is asked for.
	KindLocal

	// KindTypeParam names a type parameter, which Go writes as a single
	// capital: T, K, V, E.
	KindTypeParam
)

// Spell writes the parts as the one Go identifier they name.
//
// The kind decides the shape and the visibility decides the case, which is the
// whole arrangement: a layer that knows it is writing a constructor for a type
// it can see the visibility of asks for Spell(KindFunc, exported, "new",
// declared) and is done, rather than keeping a copy of the rule that joins New
// to a name in the case that name has.
//
// Three kinds do not take a visibility, because the thing they name has none. A
// receiver and a local are invisible outside the function they are written in,
// and a type parameter is a single capital whatever surrounds it. The argument
// is accepted and ignored for those rather than being split into another
// function, so that every name forge writes goes through one call.
//
// A name Go could not declare comes back all the same; refusing it is [Safe]'s
// job, and it is asked separately because the caller has a position to report
// and a hint to give and this has neither.
func Spell(kind Kind, exported bool, parts ...string) string {
	switch kind {
	case KindReceiver:
		return Receiver(parts...)

	case KindTypeParam:
		return TypeParam(parts...)

	case KindLocal:
		return Camel(Join(parts...))

	case KindInterface:
		return visible(Agent(parts...), exported)

	case KindError:
		return visible(sentinel(parts...), exported)

	default:
		return visible(Join(parts...), exported)
	}
}

// visible writes a joined name in the case its declaration needs.
func visible(name string, exported bool) string {
	if exported {
		return name
	}
	return Camel(name)
}

// Export writes a name as an exported Go identifier, which is [Spell] for the
// commonest case and is spelled shorter because it is asked for so often.
func Export(name string) string { return Join(name) }

// Exported reports whether a name is one a package publishes.
//
// The question every name built around a declaration has to ask first, because
// what it decides is the visibility of the answer: a constructor for an
// unexported container has no business being reachable from outside the package
// the container is unexported in. Three layers each decoded the first rune for
// themselves before this was here.
//
// By rune rather than by byte, since a name may begin with one that is not one
// byte long. A name with nothing in it is not exported, which is the safe half
// of the answer: a name a caller cannot reach beats one they can.
func Exported(name string) bool {
	first, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(first)
}

// Around returns a name built around one that is already spelled: New around
// Persons is NewPersons, and Err around persons with Full after it is
// errPersonsFull.
//
// The difference from [Spell] is the middle. A constructor is named after the
// type it builds and a sentinel error after the type that refuses, and in both
// the type's own name has to come through exactly as its author wrote it —
// forge does not derive a declaration's name and must not respell one inside a
// name derived from it. NewMyIdThing is the constructor of a type called
// MyIdThing, however differently forge would have spelled the type given the
// chance, and NewMyIDThing is a function that reads as belonging to something
// else.
//
// Everything else is spelled: the prefix, the suffixes, and the seam. The
// visibility is the caller's because it is the declaration's — a constructor
// for an unexported container has no business being reachable from outside the
// package the container is unexported in, and a helper type a layer keeps to
// itself is unexported whatever the type it belongs to is.
func Around(exported bool, before, held string, after ...string) string {
	tail := Join(after...)

	if exported {
		return Join(before) + Upper(held) + tail
	}
	if before == "" {
		return Camel(held) + tail
	}
	return Camel(Join(before)) + Upper(held) + tail
}

// Camel writes a Go name with its first word in lower case.
//
// The first word rather than the first letter, because an exported Go name
// often opens with an initialism and lowering one letter of it produces
// jSONValue — a name nobody would write and no reader would recognise. What
// counts as a word is [Words]'s answer, so that a codec naming a wire member
// and an enumeration naming a set member cannot disagree about where one ends.
//
// Only the case of the first word changes, and every other byte of the name
// survives — the letters of the words after it, initialism or not, and whatever
// separated them. This is asked of names that are already spelled, including
// names that go out on a wire, where respelling somebody's UserId as userID
// would rename a document member on their behalf and dropping the underscore
// from http_server would rename it twice. Building a name out of words is
// [Join]'s job and it is the one that spells them.
func Camel(name string) string {
	at, end := word(name, 0)
	if at < 0 {
		return name
	}
	return name[:at] + lowered(name[at:end]) + name[end:]
}

// lowered writes the word that opens an unexported name.
//
// A word at a time, so a fixed spelling goes all the way down — ID is id and
// not iD — and so does a run of capitals that is not one, since ABC lowered a
// letter at a time is aBC and nothing is served by that either.
func lowered(w string) string {
	if _, fixed := canonical(w); fixed {
		return strings.ToLower(w)
	}
	if upperRun(w) {
		return strings.ToLower(w)
	}
	return Lower(w)
}

// upperRun reports whether a word is more than one letter and all of them are
// capitals.
func upperRun(w string) bool {
	count := 0
	for _, r := range w {
		if !unicode.IsUpper(r) {
			return false
		}
		count++
	}
	return count > 1
}

// Upper returns a name with its first letter in upper case, and Lower with it
// in lower case.
//
// Between them they are the seam every name forge builds out of another one is
// joined at: a constructor named after the type it builds, a helper prefixed
// with the declaration it belongs to. A letter and not a word, which is the
// difference from [Camel] — what these are for is joining fragments, and what
// [Camel] is for is naming something a reader sees.
//
// By rune rather than by byte, since a name may begin with one that is not one
// byte long and cutting it in half produces something that is not a name at
// all.
func Upper(name string) string { return recase(name, unicode.ToUpper) }

// Lower returns a name with its first letter in lower case. See [Upper] for why
// the pair is here.
func Lower(name string) string { return recase(name, unicode.ToLower) }

// recase applies a case change to a name's first rune.
func recase(name string, to func(rune) rune) string {
	if name == "" {
		return name
	}

	first, width := utf8.DecodeRuneInString(name)
	return string(to(first)) + name[width:]
}

// Receiver names the receiver every method of a type is written with.
//
// The initials of the type's own words, at most two of them, in lower case:
// Persons is p and HomeAddress is ha. Short because a receiver is read on every
// line of a method and says nothing the signature above it has not already
// said, and derived from the type rather than chosen so that every method of
// one type is written the same way — which is the part a reader notices when it
// is not true, and the part no layer generating one method at a time can hold
// on its own.
//
// Never self, this or me, which name the receiver after a language forge is not
// generating, and never the word value spelled out.
func Receiver(parts ...string) string {
	var out strings.Builder

	for _, part := range parts {
		for _, w := range Words(part) {
			if out.Len() == 2 {
				return out.String()
			}

			first, _ := utf8.DecodeRuneInString(w)
			out.WriteRune(unicode.ToLower(first))
		}
	}
	return out.String()
}

// TypeParam names a type parameter, which Go writes as a single capital.
//
// A word would read as a type rather than as a stand-in for one, and the
// convention is old enough that E in a container and T in a wrapper are read
// without being explained. The letter is the first of whatever the caller
// called it, so that a layer naming its parameter Element gets E and one naming
// it T gets T, and neither has to know the rule.
func TypeParam(parts ...string) string {
	for _, part := range parts {
		for _, r := range part {
			if unicode.IsLetter(r) {
				return string(unicode.ToUpper(r))
			}
		}
	}
	return "T"
}

// sentinel names an error value after what refused it: ErrPersonsFull.
//
// The prefix is added only where it is not already there, so that a caller
// passing a whole name and one passing the parts of it reach the same answer.
func sentinel(parts ...string) string {
	held := Join(parts...)
	if w := Words(held); len(w) > 0 && w[0] == "Err" {
		return held
	}
	return "Err" + held
}

// Question returns the method name a boolean answer is read under.
//
// A method returning a bool reads as a question — IsEmpty, HasKey, CanRetry —
// and the question is chosen by what the method returns rather than by how the
// field it reads is spelled, so a bool field named Active yields IsActive. That
// also keeps it out of the way of a projection, which would already have taken
// Active.
//
// A name that already asks something is left alone, so that a field named
// IsActive does not become IsIsActive and one named HasKey keeps the verb its
// author chose.
func Question(parts ...string) string {
	held := Join(parts...)

	if w := Words(held); len(w) > 0 && asking(w[0]) {
		return held
	}
	return "Is" + held
}

// asking reports whether a word already opens a question.
func asking(w string) bool {
	switch strings.ToLower(w) {
	case "is", "has", "can", "should", "was", "were", "does", "did", "must", "are":
		return true
	default:
		return false
	}
}
