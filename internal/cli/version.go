package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/okian/forge/internal/model"
)

// unknown is what a version reads as when the build did not record one, which
// is what `go run` and a build from a dirty tree both produce.
const unknown = "(devel)"

// build reads the version stamped into this binary, so that a test can say what
// it read.
var build = debug.ReadBuildInfo

// version reports what this binary is and what it was built from.
//
// Three lines, because three versions can disagree and each disagreement means
// something different. forge's own version says which generator wrote a file.
// The marker module's says which declarations it can resolve — markers are what
// a spec file is written against, and a spec naming one this build has never
// heard of is the failure this line explains. The Go version says which
// language the output has to compile under, which is the one of the three that
// changes what is emitted.
func version(env *environment, cmd command, args []string) error {
	flags := flagsFor(cmd)

	rest, on, err := parse(env, cmd, flags, args)
	if !on || err != nil {
		return err
	}

	if len(rest) > 0 {
		return answered(cmd, flags, "version takes no arguments, got %q", rest[0])
	}

	self, markers, toolchain := versions()
	say(env.stdout, "forge %s\nmarkers %s\ngo %s\n", self, markers, toolchain)

	return nil
}

// versions returns what this binary is, what markers it resolves, and what
// built it.
func versions() (self, markers, toolchain string) {
	self, markers, toolchain = unknown, unknown, runtime.Version()

	info, ok := build()
	if !ok {
		// A binary with no build information is one somebody assembled by hand,
		// and saying so is more use than reporting a version invented here.
		return self, markers, toolchain
	}

	if info.Main.Version != "" {
		self = info.Main.Version
	}
	if info.GoVersion != "" {
		toolchain = info.GoVersion
	}

	markers = fmt.Sprintf("%s %s", model.MarkerPkg, marked(info))

	return self, markers, toolchain
}

// marked returns the version of the module that declares the markers.
//
// Not this binary's own, and the difference is the whole reason this is a
// function. A spec file names [model.MarkerPkg] whichever binary is generating
// for it, and a binary somebody linked a layer into has a module of its own —
// so a header recording the running binary's module would say a file was
// written against markers that module does not declare. The freshness check
// compares that line, so every file such a binary wrote would look to forge's
// own command like the work of different tooling.
//
// Looked for in the main module first, since that is where it is when forge
// itself is running, and in the requirements otherwise. A module reached
// through a filesystem replace records no version, which reads as unstamped —
// correct, and the case every development build of a layer is in.
func marked(info *debug.BuildInfo) string {
	if info.Main.Path == model.MarkerPkg {
		return recorded(info.Main.Version)
	}

	for _, one := range info.Deps {
		if one != nil && one.Path == model.MarkerPkg {
			return recorded(replacing(one).Version)
		}
	}

	// A binary that does not link the markers at all, which no forge command
	// can be: the loader resolves a declaration through them. Saying so beats
	// reporting a version nothing supplied.
	return unknown
}

// replacing returns the module a requirement was resolved to, following a
// replacement.
//
// A replaced module is what a development build of a layer has: the go command
// records the requirement and the directory or version it was replaced by, and
// the version that matters is the one actually built against.
//
// Named for the replacement it follows rather than the `held` this package uses
// for a local everywhere, and rather than `resolved`, which the pipeline has: a
// package-level function of either name would be shadowed or would collide, and
// the first would keep compiling until something needed to call it.
func replacing(one *debug.Module) *debug.Module {
	if one.Replace != nil {
		return one.Replace
	}
	return one
}

// recorded returns a version, or the word for one nothing stamped.
func recorded(version string) string {
	if version == "" {
		return unknown
	}
	return version
}
