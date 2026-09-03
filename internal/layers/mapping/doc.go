// Package mapping generates a constructor that builds one type from another:
// target members matched to source members by name where that is unambiguous
// and assignable, settled by a //forge:map hint or a from tag where it is not,
// and refused where they are neither.
//
// The from tag sits on a target field and pins it to the source member it
// names: `from:"Contact"` reads whichever source maps in, and
// `from:"Account.Contact, Company.EmailAddress"` carries one entry per source
// where several map into one target. Parens — `from:"User.NickName()"` —
// assert the member is a method; a bare name resolves field first, method
// second, exactly as the ladder does. A tag and a hint settling one member is
// refused: two explicit answers do not agree by accident.
//
// The marker is [github.com/okian/forge.Map], the one two-parameter marker in
// the catalog: a bridge over a source and a target rather than a layer over a
// stream. What it generates is a package function — PersonFromUser for
// Map[User, Person] — so the target's method set is untouched and the source
// may be a struct or an interface.
package mapping
