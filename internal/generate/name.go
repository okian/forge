package generate

import (
	"slices"
	"strings"
)

// The names generated files take.
//
// One file per declaration, named after it, so that a diff says which
// declaration changed before anybody opens it; and one more for whatever
// several declarations share. The zz prefix is not decoration — it sorts
// generated files below hand-written ones in every listing a person reads.
const (
	prefix = "zz_forge_"
	shared = prefix + "shared.go"
	stub   = prefix + "stubs.go"
)

// suffix is what is put after a declaration's name when the ordinary spelling
// would be a file the Go build treats specially.
//
// It has to be an element the build cannot read as anything: not an operating
// system, not an architecture, and not "test". "gen" is none of those and says
// what the file is.
const suffix = "_gen"

// Named names a generated file after the declaration it was generated for.
//
// Lower case, because Go source file names are, and because a package holding
// zz_forge_Persons.go beside zz_forge_person.go reads as two conventions. Two
// declarations whose names differ only in case therefore want one file, which
// is a collision the caller reports rather than something to resolve by
// inventing a second spelling.
//
// The awkward part is that a Go file name is not only a name. A file ending in
// _test.go is a test file and is not in the ordinary build at all; one ending
// in _linux.go or _arm64.go or _linux_amd64.go carries a build constraint
// nobody wrote. A declaration called Test or Windows is an ordinary thing to
// write — and so is one called Data_linux, since the rule is about the file's
// trailing elements and a declaration's own underscores end up among them. The
// file such a declaration would otherwise get is one the compiler quietly
// leaves out, which looks exactly like forge not having run.
func Named(declared string) string {
	name := prefix + strings.ToLower(declared)

	if constrained(name) {
		name += suffix
	}
	return name + ".go"
}

// Shared names the file holding what several declarations in a package share.
func Shared() string { return shared }

// Stubs names the file standing in for a package's output under the tag.
//
// The prefix is not only a convention here. A file forge writes under the tag
// is one every other configuration leaves out of its build, and the report for
// a file left behind by a rename recognises forge's own excluded output by this
// name — a stub called anything else makes every package holding one look half
// read, and turns that report off without saying so.
func Stubs() string { return stub }

// Reserved names the files a package writes that no single declaration owns.
//
// They are named rather than derived from a declaration, so a declaration whose
// own name lands on one of them wants a file the package has already spoken
// for. That is the same collision as two declarations wanting one file and is
// reported the same way — quietly writing both would leave whichever was
// written second, and the declaration would appear to have generated nothing.
func Reserved() []string { return []string{shared, stub} }

// Ours reports whether a file is named the way generation names one.
//
// It is only about the name, which is a convention anybody could follow — what
// says a file is forge's is the line inside it. A caller deciding whether to
// touch a file asks both, and asks this one first because it costs no read.
func Ours(name string) bool {
	return strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".go")
}

// constrained reports whether a file under this name would be left out of an
// ordinary build, or put in the test build instead.
//
// The go command's rule is about the end of the name and not about one element
// of it: everything before the last underscore is ignored, so a declaration
// that contains one puts its own text where the rule looks. A trailing _test
// makes a test file; a trailing operating system or architecture constrains it;
// and the two together constrain it twice.
func constrained(name string) bool {
	if strings.HasSuffix(name, "_test") {
		return true
	}

	parts := strings.Split(name, "_")
	last := parts[len(parts)-1]

	if slices.Contains(systems, last) {
		return true
	}
	if !slices.Contains(architectures, last) {
		return false
	}

	// An architecture on its own constrains a file, and one after an operating
	// system constrains it to the pair. Either way the file is left out of
	// nearly every build, so either way the name has to change.
	return true
}

// The operating systems and architectures the go command reads out of a file
// name.
//
// Written down because nothing exports them: go/build knows, and keeps the
// lists to itself. Being wrong about one is a generated file the compiler
// silently ignores, so this package's tests ask the toolchain for the real
// lists rather than trusting these — which is the only way to find out that a
// release added one.
var (
	systems = []string{
		"aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos",
		"ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9", "solaris",
		"wasip1", "windows", "zos",
	}

	architectures = []string{
		"386", "amd64", "amd64p32", "arm", "arm64", "arm64be", "armbe",
		"loong64", "mips", "mips64", "mips64le", "mips64p32", "mips64p32le",
		"mipsle", "ppc", "ppc64", "ppc64le", "riscv", "riscv64", "s390",
		"s390x", "sparc", "sparc64", "wasm",
	}
)
