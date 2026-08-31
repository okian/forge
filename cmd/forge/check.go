package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/emit"
	generated "github.com/okian/forge/internal/generate"
	"github.com/okian/forge/internal/layers"
)

// What check reports about the state of somebody's working tree.
//
// Both are 5xxx for the reason the orphan report is: they are about what is on
// disk rather than about anything anybody wrote, and what they take to fix is a
// command rather than an edit.
var (
	codeStale   = diag.Register(5004, "generated file is out of date")
	codeMissing = diag.Register(5005, "declaration has no generated file")
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

	fresh, checked := freshness(found)
	problems.Merge(&fresh)

	if env.report(problems) {
		return errReported
	}

	env.announce(true, "%s", counting(checked))
	return nil
}

// freshness reports every generated file that is out of date or missing.
//
// Per declaration, because that is the level a fingerprint is recorded at and
// the level an author acts at.
//
// The two files a package writes for itself are not checked. Both record a
// fingerprint like any other file, and the value to compare it against is what
// this cannot cheaply have: the shared file is a function of which helpers the
// declarations asked for, which is not known until all of them have composed,
// and composing every declaration is the work this verb exists to avoid.
//
// Most of what would make them stale reaches a declaration first — an option
// changed, a field added, a subject renamed — and that declaration's own file
// is reported here, which is the line the author acts on. What is genuinely
// missed is a change to no declaration at all: somebody editing the shared file
// by hand, or a change to forge's own generator that this build's version does
// not distinguish. Regenerating is what settles those, and the run that does it
// says what it rewrote.
func freshness(found resolved) (diag.Set, int) {
	var (
		diags   diag.Set
		checked int
	)

	cfg := configured(layers.Builtins())

	for _, pkg := range grouped(found.Requests) {
		if pkg.dir == "" {
			// A package forge cannot find the files of is one it cannot read a
			// header from. Whatever is wrong with it is already reported by
			// whatever failed to give it a directory.
			continue
		}

		for _, req := range pkg.requests {
			if req.Model == nil {
				// A declaration whose subject was refused is one nothing could
				// have been generated for, and the refusal is already reported.
				continue
			}
			stale(&diags, pkg, req, cfg)
			checked++
		}
	}

	return diags, checked
}

// stale reports what is wrong with one declaration's file, if anything.
func stale(diags *diag.Set, pkg packaged, req generated.Request, cfg generated.Config) {
	name := generated.Named(req.Model.Name)

	held, err := os.ReadFile(filepath.Join(pkg.dir, name)) //nolint:gosec // a package directory and a name forge chose.
	switch {
	case errors.Is(err, os.ErrNotExist):
		diags.Add(diag.New(codeMissing, req.Model.Pos,
			"%s has no %s", req.Model.Name, name).
			WithHint("%s", "run forge generate"))
		return

	case err != nil:
		// There and unreadable, which is neither missing nor stale: forge has
		// nothing to compare and cannot say which. A directory under the name,
		// or a mode nothing can open, both land here.
		diags.Add(diag.New(codeForeign, req.Model.Pos,
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
		diags.Add(diag.New(codeForeign, req.Model.Pos,
			"%s is already there and does not say forge wrote it", name).
			WithHint("%s", "delete it and run forge generate if it is forge's and lost its header, "+
				"or rename the declaration if the file is yours"))
		return
	}

	var sum emit.Digest
	generated.Fingerprint(&sum, req, pkg.name, cfg)

	if said := staleness(recorded, sum.String(), cfg); said != "" {
		diags.Add(diag.New(codeStale, req.Model.Pos, "%s is %s", name, said).
			WithHint("%s", "run forge generate"))
	}
}

// staleness says how a file is out of date, or nothing.
//
// The fingerprint answers it: it covers the declaration, the subject, the
// options and all three versions, so a file whose fingerprint matches was made
// from these inputs by this build and a file whose fingerprint differs was not.
//
// A file with no fingerprint at all is reported rather than passed over, and
// the reason is that forge writes one into every file it produces. So a file
// that says forge wrote it and records none is a file that differs from what
// forge writes — by that line at least, and by however much else was lost with
// it. Saying "this cannot tell" and moving on would turn the check off for that
// file permanently, since nothing else would ever ask about it again; saying so
// and asking for a regenerate costs one command and settles it.
//
// The versions are what is left to say something *specific* with, and are worth
// saying where they differ: a file from an older forge or written against older
// markers is a different problem from one that was merely edited, and a reader
// deciding whether to worry wants to know which.
func staleness(recorded emit.Header, inputs string, cfg generated.Config) string {
	if recorded.Inputs != "" {
		if recorded.Inputs == inputs {
			return ""
		}
		return "not what these inputs produce"
	}

	switch {
	case recorded.Forge != "" && recorded.Forge != cfg.Forge:
		return "from forge " + recorded.Forge + " and records nothing to compare, and this is " + cfg.Forge
	case recorded.Markers != "" && recorded.Markers != cfg.Markers:
		return "written against markers " + recorded.Markers + " and records nothing to compare"
	}
	return "missing the fingerprint forge writes into every file, so what it holds cannot be compared"
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
