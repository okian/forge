package cli

import (
	"errors"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	generated "github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/model"
	"github.com/okian/forge/plugin"
)

// What check reports about the state of somebody's working tree.
//
// Both are 5xxx for the reason the orphan report is: they are about what is on
// disk rather than about anything anybody wrote, and what they take to fix is a
// command rather than an edit.
var (
	codeStale   = diag.Register(5004, "generated file is out of date")
	codeMissing = diag.Register(5005, "declaration has no generated file")

	// codeToolingMoved is staleness of a narrower kind: the declaration is
	// unchanged and what wrote the file has moved under it. Its own code
	// because it is its own urgency — the file still describes the source
	// beside it, so a reader deciding whether to stop what they are doing
	// wants to be able to tell this apart at a glance.
	codeToolingMoved = diag.Register(5007, "generated file was written by different tooling")
)

// check validates declarations and verifies that what was generated is fresh.
//
// The whole point of it is to be cheap enough to run on every push. A file
// records a fingerprint of everything it was made from, so asking whether it is
// current is reading a line and comparing it — not composing the stack, not
// rendering the file, and not writing anything. That is what makes this a
// different verb from --dry-run rather than a spelling of it: --dry-run answers
// "what would change" by working out the change, and this answers "is anything
// out of date" by asking each file what it was made from.
func check(env *environment, cmd command, args []string) error {
	flags := flagsFor(cmd)

	packages, on, err := parse(env, cmd, flags, args)
	if !on || err != nil {
		return err
	}

	found, err := env.pipeline.follow(env, env.loadConfig(packages...))
	if err != nil {
		return err
	}

	// Everything the pipeline found, which includes the spec files being
	// type-checked: forge loads under the tag with function bodies stripped, so
	// a spec declaration naming a subject that has been renamed or deleted is a
	// load failure here whether or not anything was regenerated.
	//
	// The other half of that declaration's output is not. A file written
	// against the tag is outside every load forge performs, so nothing here
	// compiles it — that is the ordinary build's job, and it is the build
	// everybody runs anyway.
	problems := found.All()

	// A file left behind by a rename belongs in the same report. It is not
	// staleness — nothing is out of date, there is simply a file too many — but
	// it is the same question being asked, and an author running this wants
	// both answers at once.
	loose := orphans(found)
	problems.Merge(&loose)

	fresh, checked := freshness(env.layers(), found)
	problems.Merge(&fresh)

	if env.report(problems) {
		return errReported
	}

	env.announce(true, "%s", counting(checked))
	return nil
}

// freshness reports every generated file that is out of date or missing.
//
// Per package, because that is what a package writes now and what a fingerprint
// is recorded at. The file records what every declaration in the package was
// made from, so asking whether it is current is reading one line and comparing
// it — no composing, no rendering, and nothing written.
//
// Both of a package's files are checked, which is new and is the plainest win
// of writing one. What used to keep the shared file out of this was that its
// fingerprint was a function of which helpers the declarations asked for, and
// knowing that meant generating every one of them — the work this verb exists
// to avoid. A package's fingerprint is its declarations, which are here to be
// read, so there is nothing left that this cannot cheaply answer.
func freshness(catalog *plugin.Registry, found resolved) (diag.Set, int) {
	var (
		diags   diag.Set
		checked int
	)

	cfg := against(catalog, found.Session)

	for _, pkg := range grouped(found.Requests) {
		if pkg.dir == "" {
			// A package forge cannot find the files of is one it cannot read a
			// header from. Whatever is wrong with it is already reported by
			// whatever failed to give it a directory.
			continue
		}

		// A declaration whose subject was refused is one nothing could have
		// been generated for, and the refusal is already reported. It is left
		// out of the fingerprint too, which is what generation does with it.
		held := generable(pkg.requests)
		if len(held) == 0 {
			continue
		}

		for _, name := range wanted(held) {
			stale(&diags, pkg, held, name, cfg)
		}
		checked += len(held)
	}

	return diags, checked
}

// generable returns the requests something could have been generated for.
func generable(requests []generated.Request) []generated.Request {
	out := make([]generated.Request, 0, len(requests))

	for _, one := range requests {
		if one.Model != nil {
			out = append(out, one)
		}
	}
	return out
}

// wanted returns the files a package's declarations should have between them.
//
// One, and a second only where a declaration is written in spec form: that is
// the one case the language forces two files, and a package that has stopped
// having spec declarations has stopped wanting the second. A file left behind
// by that is a leftover rather than a staleness, and the orphan report is what
// names it.
func wanted(requests []generated.Request) []string {
	out := []string{generated.Name()}

	for _, one := range requests {
		if one.Model.Form == model.FormSpec {
			return append(out, generated.Stubs())
		}
	}
	return out
}

// where points a report about a package at the first declaration in it. A
// package has no position of its own, and every declaration in it is equally
// the reason the file is there.
func where(requests []generated.Request) token.Position {
	return requests[0].Model.Pos
}

// stale reports what is wrong with one of a package's files, if anything.
func stale(diags *diag.Set, pkg packaged, requests []generated.Request, name string, cfg generated.Config) {
	at := where(requests)

	held, err := os.ReadFile(filepath.Join(pkg.dir, name)) //nolint:gosec // a package directory and a name forge chose.
	switch {
	case errors.Is(err, os.ErrNotExist):
		diags.Add(diag.New(codeMissing, at,
			"package %s has no %s", pkg.name, name).
			WithHint("%s", "run forge generate"))
		return

	case err != nil:
		// There and unreadable, which is neither missing nor stale: forge has
		// nothing to compare and cannot say which. A directory under the name,
		// or a mode nothing can open, both land here.
		diags.Add(diag.New(codeForeign, at,
			"%s cannot be read: %v", name, err).
			WithHint("%s", "make it readable, or move it out of the way and run forge generate"))
		return
	}

	recorded, wrote := emit.ReadHeader(held)
	if !wrote {
		// A file forge will not write over, which is not staleness: nothing is
		// out of date, there is a file in the way. Reported in the same words
		// the run that met it would use, and offering the same two ways out,
		// because forge cannot tell somebody's own file from a generated one
		// whose header was lost.
		diags.Add(diag.New(codeForeign, at,
			"%s is already there and does not say forge wrote it", name).
			WithHint("%s", "delete it and run forge generate if it is forge's and lost its header, "+
				"or move it out of the way"))
		return
	}

	said, moved := staleness(recorded, requests, pkg.name, cfg)
	if said == "" {
		return
	}

	code, hint := codeStale, "run forge generate"
	if moved {
		code = codeToolingMoved
		hint = "run forge generate when convenient; nothing about the declarations has changed"
	}

	diags.Add(diag.New(code, at, "%s is %s", name, said).WithHint("%s", hint))
}

// staleness says how a file is out of date, and whether the only thing that
// changed is which tooling made it.
//
// The fingerprint answers the first part: it covers every declaration in the
// package, each one's subject and options, and all three versions — so a file
// whose fingerprint matches was made from these inputs by this build and a file
// whose fingerprint differs was not.
//
// That leaves "differs how", which matters because two very different things
// land in one answer. A declaration somebody edited and a forge somebody
// upgraded both move the fingerprint, and only one of them is a file that no
// longer describes the source beside it. Telling them apart is why the header
// records all three versions: the fingerprint is recomputed a second time
// against the ones the file recorded, and a file that matches *then* is one
// whose declaration has not changed at all.
//
// A file with no fingerprint is reported rather than passed over, because forge
// writes one into every file it produces. So a file that says forge wrote it
// and records none already differs from what forge writes — by that line at
// least, and by however much else went with it. Saying "this cannot tell" and
// moving on would turn the check off for that file for good, since nothing else
// would ever ask about it again.
func staleness(recorded emit.Header, requests []generated.Request, pkg string, cfg generated.Config) (said string, tooling bool) {
	if recorded.Inputs == "" {
		return "missing the fingerprint forge writes into every file, so what it holds cannot be compared", false
	}

	var sum emit.Digest
	generated.FingerprintPackage(&sum, requests, pkg, cfg)

	if recorded.Inputs == sum.String() {
		return "", false
	}

	// Again, against what the file says made it. Anything the header does not
	// record is left as it is, which is the safe direction: a version this
	// cannot substitute is one the answer stays "something changed" for.
	was := cfg
	for _, held := range []struct {
		recorded string
		into     *string
	}{
		{recorded.Forge, &was.Forge},
		{recorded.Markers, &was.Markers},
		{recorded.Toolchain, &was.Toolchain},
	} {
		if held.recorded != "" {
			*held.into = held.recorded
		}
	}

	var then emit.Digest
	generated.FingerprintPackage(&then, requests, pkg, was)

	if recorded.Inputs == then.String() {
		return "unchanged, and was written by " + moved(recorded, cfg), true
	}
	return "not what these inputs produce", false
}

// moved names the tooling that differs, and only that.
//
// Only that, because the point of the sentence is which thing changed. Printing
// all three versions twice would bury the one that moved in five that did not,
// and the one that moved is the whole of what a reader is being told.
func moved(recorded emit.Header, cfg generated.Config) string {
	var said []string
	for _, one := range []struct{ what, was, now string }{
		{"forge", recorded.Forge, cfg.Forge},
		{"markers", recorded.Markers, cfg.Markers},
		{"go", recorded.Toolchain, cfg.Toolchain},
	} {
		if one.was != "" && one.was != one.now {
			said = append(said, one.what+" "+one.was+" rather than "+one.now)
		}
	}

	if len(said) == 0 {
		// The fingerprint matched under what the file recorded and the versions
		// are the same, which means something the header does not record moved.
		return "tooling this cannot name"
	}
	return strings.Join(said, ", ")
}

// counting says how many declarations were compared against their files, so
// that a run which found nothing says what it looked at rather than only that
// it is happy.
//
// What freshness looked at rather than what resolution found, which are not the
// same number: a declaration in a package forge cannot locate the files of is
// one nothing was compared for, and counting it would report a file as current
// that was never opened.
func counting(checked int) string {
	if checked == 1 {
		return "1 declaration is up to date"
	}
	return fmt.Sprintf("%d declarations are up to date", checked)
}
