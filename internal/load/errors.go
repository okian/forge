package load

import (
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/model"
)

// Diagnostics this package reports.
var (
	codeBuildError = diag.Register(5001, "package does not build")
	codeNoPackages = diag.Register(5002, "pattern matched no Go files")
)

// Hints for the two ways a load can fail. Each says the thing the message
// itself does not, and each gives way to a better suggestion when the go
// command supplies one of its own.
const (
	buildHint      = "forge reads the package's type information, so it has to type-check before generation can run"
	noPackagesHint = "no Go files were in scope for this pattern, so forge has nothing to generate for"
)

// ungeneratedHint is what forge says about a missing name in a package nothing
// has generated for yet.
//
// It is the one build failure forge itself causes. A reference to a method or a
// type that only the generated file declares cannot type-check before that file
// exists, and forge will not generate for a package that does not type-check —
// so the first run over a package whose author has already written the call
// sites refuses, with a message about a name they can see is missing and no
// word about why.
//
// Written as a possibility rather than a finding, because it is one. Nothing
// here can tell a name forge would have written from a bare name somebody
// misspelt: the two arrive as one error, and what separates them is whether the
// file that would have declared it exists — which is the thing that has not
// happened yet. So the reader is told what to check rather than what is wrong,
// and the shapes that could only be a misspelling are kept out: a qualified
// name is asked of the package it names, and one the checker has already found
// a near-match for is left alone.
const ungeneratedHint = "if this is a name forge writes for a declaration, " +
	"nothing has written it yet — comment the reference out, generate, and put it back"

// staleHint is what forge says about a build error inside its own output.
//
// The mirror of [ungeneratedHint]: that one is about a name forge has not
// written yet, and this one is about a file it wrote for declarations that
// have since moved on — a declaration deleted, a subject renamed, a type that
// left the package. The file cannot be edited into agreement and does not need
// to be: it is output, and removing it clears the way for the next run to
// write what the declarations now say. Without this, the recovery is a
// chicken-and-egg an author has to see through on their own — the stale file
// stops the type-check, the type-check stops generation, and generation is the
// only thing that would have replaced the stale file.
const staleHint = "this file is forge's own output and no longer matches the declarations it was " +
	"written from — delete the package's forge.gen.go and forge_stubs.gen.go and generate again"

// unusedImport matches the type-checker's two phrasings for an import nothing
// uses, and nothing else.
//
// Anchoring matters. Three of the checker's messages end in "and not used",
// and the other two — an unused label, and an unused type-switch guard — are
// real errors that stripping cannot cause but a function literal can still
// produce, because literals keep their bodies. Matching on the suffix alone
// would swallow both.
var unusedImport = regexp.MustCompile(`^"[^"]*" imported( as \S+)? and not used$`)

// The two shapes a reference to something forge has not written yet arrives in.
//
// A type in a signature is the first — `func f(Persons) PersonsSeq` — and a
// call of a generated method the second: `var _ = Persons(nil).Len()`. They are
// not the checker's only ways of saying a name is missing, which is why each
// is written out in full rather than matched by a suffix. A field nobody
// declared in a struct literal is a third, and an interface a type fails to
// satisfy a fourth, and neither is a name generating supplies — the literal
// names a field forge does not add, and the assertion forge writes for is
// dropped on the way in, so what is left of it is somebody else's.
//
// Each captures the package a name was looked for in, where it has one, so that
// the suggestion is made about the package that would have supplied the name
// rather than about the one that wanted it. Written to the end on both sides:
// the checker's helpful variants — "but does have Sqrt", "but does have FoO" —
// are by construction a misspelling of something that exists, never a name
// nothing has written yet, and the comma before them is what leaves them out.
var (
	undefinedName = regexp.MustCompile(`^undefined: (?:(\w+)\.)?\w+$`)
	missingMember = regexp.MustCompile(
		`^.+ undefined \(type \*?(?:(\w+)\.)?.+ has no field or method [^,]+\)$`)
)

// supplied reports whether a load error is a name generating would have
// written, asking the package that would have written it.
func (s *Session) supplied(pkg *packages.Package, err packages.Error, here bool) bool {
	for _, one := range []*regexp.Regexp{undefinedName, missingMember} {
		held := one.FindStringSubmatch(err.Msg)
		if held == nil {
			continue
		}
		if held[1] == "" {
			return here
		}
		return asksForCode(s.imported(pkg, err, held[1]))
	}
	return false
}

// imported returns the package a qualifier names where the error was written,
// or nothing where it names none.
//
// A repository over a model is the ordinary arrangement, and a method handing
// back a name forge writes is the ordinary thing to put on one — so the name
// that cannot be found is very often in the neighbouring package rather than in
// the one that reached for it. Asking only about the package holding the error
// would leave the case this exists for unexplained in the layout most likely to
// produce it.
//
// Resolved through the file the error is in rather than by matching the
// qualifier against what the imported packages call themselves. A qualifier is
// what the author wrote, and an aliased import means that is not the package's
// own name — so matching on the name would miss every aliased import, and would
// have to answer for both of them where two packages of one name are imported,
// which is precisely the arrangement an alias is written for.
func (s *Session) imported(pkg *packages.Package, err packages.Error, qualifier string) *packages.Package {
	at := s.position(err.Pos).Filename

	for _, file := range pkg.Syntax {
		if s.FileName(file) != at {
			continue
		}

		for _, spec := range file.Imports {
			path, malformed := strconv.Unquote(spec.Path.Value)
			if malformed != nil {
				continue
			}

			held, known := pkg.Imports[path]
			if !known {
				continue
			}

			// The alias where there is one, and the package's own name where
			// there is not, which is what the author had to write to reach it.
			name := held.Name
			if spec.Name != nil {
				name = spec.Name.Name
			}
			if name == qualifier {
				return held
			}
		}
	}

	return nil
}

// ownOutput reports whether an error points into a file forge itself wrote.
//
// By the header rather than by the file's name, so a driver carrying forge's
// layers under another binary is recognised by what it writes rather than by
// what it is called. Forge's header and not any generator's, unlike
// [Session.Generated]: the hint this answer selects names forge's files and
// forge's verb, and advice to delete somebody else's output would be advice to
// break their build.
func (s *Session) ownOutput(pkg *packages.Package, err packages.Error) bool {
	at := s.position(err.Pos).Filename

	for _, file := range pkg.Syntax {
		if s.FileName(file) != at {
			continue
		}
		return len(file.Comments) > 0 && len(file.Comments[0].List) > 0 &&
			strings.HasPrefix(file.Comments[0].List[0].Text, "// Code generated by forge")
	}

	return false
}

// collect turns a package's load errors into diagnostics, dropping the ones
// forge's own way of loading creates and the ones it has already reported.
func (s *Session) collect(pkg *packages.Package) {
	// Nothing wrong is the overwhelming case, and everything below is about
	// something being wrong. Answered here rather than by falling through an
	// empty loop, so that the work of deciding what to suggest is done only for
	// the packages that need a suggestion.
	if len(pkg.Errors) == 0 {
		return
	}

	// A pattern that resolves to nothing comes back as a synthetic package
	// carrying the go command's complaint. So does a directory whose Go files
	// are all excluded by a build tag, or one that holds no Go files at all —
	// which for forge's purposes is the same answer: there is nothing here to
	// generate for.
	empty := len(pkg.GoFiles) == 0 && pkg.Name == ""

	// Asked once for the package rather than once per error, since it is a
	// question about the package and a broken one reports the same missing name
	// several times over.
	here := asksForCode(pkg)

	for _, err := range pkg.Errors {
		if caused(err) {
			continue
		}

		code, fallback := codeBuildError, buildHint
		switch {
		case empty:
			code, fallback = codeNoPackages, noPackagesHint
		case s.ownOutput(pkg, err):
			// Asked before the message's shape is, because where the error
			// sits is the stronger fact: an undefined name inside forge's own
			// file is not one commenting out will cure, whatever it is called.
			fallback = staleHint
		case s.supplied(pkg, err, here):
			fallback = ungeneratedHint
		}

		message, hint := splitMessage(err.Msg)
		if hint == "" {
			hint = fallback
		}

		s.add(diag.New(code, s.position(err.Pos), "%s", message).WithHint("%s", hint))
	}
}

// asksForCode reports whether anything in the package asks forge to write for
// it.
//
// The condition on the hint about a name that has not been generated. A package
// with no directive in it gets nothing written for it, so a name missing from
// one is missing for an ordinary reason and a suggestion about generation would
// send its author looking in the wrong place entirely.
//
// Every comment rather than the ones above a declaration, which is what
// discovery reads. This is deciding what to suggest rather than what to
// generate, and the looser question is the simpler one and the safer one: a
// directive written somewhere it will not be read still says an author expects
// forge to write for this package, and is if anything a reason they are
// confused about what has been written.
// Nothing is a fair thing to be asked about, and answers no. A qualifier that
// names no import forge can resolve is one this run knows nothing about, and a
// package it knows nothing about is not one it writes for — where refusing to
// answer would make every caller check first for a case none of them causes.
func asksForCode(pkg *packages.Package) bool {
	if pkg == nil {
		return false
	}

	for _, file := range pkg.Syntax {
		for _, group := range file.Comments {
			for _, line := range group.List {
				if strings.HasPrefix(line.Text, model.DirectivePrefix) {
					return true
				}
			}
		}
	}
	return false
}

// Broken reports whether a package failed to build.
//
// Not the same question as whether it carries errors, and the difference is
// forge's own doing: a package is loaded with its function bodies stripped, so
// every import used only inside one reports as unused. A caller reading the
// errors go/packages carries would find every package holding generated code
// broken, which is the opposite of the answer.
//
// Exported because it is asked outside this package — a report about a
// generated file left behind is only worth making about a package that will not
// compile — and because answering it anywhere else would mean a second copy of
// the rule that decides which errors are forge's own.
func Broken(pkg *packages.Package) bool {
	if pkg == nil {
		return false
	}

	for _, err := range pkg.Errors {
		if !caused(err) {
			return true
		}
	}
	return false
}

// caused reports whether a load error is one forge's own way of loading
// produces rather than one a build would have raised.
func caused(err packages.Error) bool {
	return unusedImport.MatchString(err.Msg) && err.Kind == packages.TypeError
}

// add records a diagnostic unless an identical one has already been recorded.
//
// The go command reports a single bad file more than once — as a list error,
// as a parse error, and again as whatever type error follows from it — and an
// author who made one mistake should be told about one mistake.
func (s *Session) add(d diag.Diagnostic) {
	key := d.Error()
	if s.seen[key] {
		return
	}
	if s.seen == nil {
		s.seen = make(map[string]bool)
	}
	s.seen[key] = true
	s.Diagnostics.Add(d)
}

// reportNoPackages records that the patterns matched nothing at all.
//
// The go command normally explains an unmatched pattern itself, and that
// explanation is the better one. This covers the case where nothing comes back
// to explain anything, where the worst possible answer would be silence: a run
// that generates nothing and says nothing looks exactly like a run with no work
// to do.
func (s *Session) reportNoPackages(patterns []string) {
	s.add(
		diag.New(codeNoPackages, token.Position{}, "no packages match %s", strings.Join(patterns, " ")).
			WithHint("%s", noPackagesHint),
	)
}

// splitMessage separates the go command's complaint from the suggestion it
// sometimes appends beneath it.
//
// The go tool writes those suggestions as indented continuation lines — "to add
// it:" followed by the go get command to run — which is precisely what a
// diagnostic's hint is for, and better advice than anything forge could invent.
func splitMessage(msg string) (message, hint string) {
	message, rest, found := strings.Cut(msg, "\n")
	if !found {
		return message, ""
	}

	var parts []string
	for line := range strings.SplitSeq(rest, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, line)
		}
	}

	return message, strings.Join(parts, "; ")
}

// position parses the "file:line:col" string a packages.Error carries and
// resolves it against the directory the session loaded from.
//
// The go command reports the same file both ways — relative to the load
// directory in one error and absolute in the next — and diagnostics are sorted
// by file name, so leaving both forms in would scatter one file's problems
// across the report.
func (s *Session) position(pos string) token.Position {
	out := parsePosition(pos)
	if out.Filename == "" || filepath.IsAbs(out.Filename) {
		return out
	}
	out.Filename = filepath.Join(s.dir, out.Filename)
	return out
}

// parsePosition splits a "file:line:col" string.
//
// The line and column are peeled off the end rather than the file name split
// off the front, so that a path which itself contains a colon cannot be
// mistaken for a position.
func parsePosition(pos string) token.Position {
	if pos == "" {
		return token.Position{}
	}

	out := token.Position{Filename: pos}
	for range 2 {
		i := strings.LastIndexByte(out.Filename, ':')
		if i < 0 {
			break
		}
		n, err := strconv.Atoi(out.Filename[i+1:])
		if err != nil {
			break
		}
		out.Column = out.Line
		out.Line = n
		out.Filename = out.Filename[:i]
	}

	return out
}
