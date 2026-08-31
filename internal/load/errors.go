package load

import (
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/okian/forge/internal/diag"
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

// unusedImport matches the type-checker's two phrasings for an import nothing
// uses, and nothing else.
//
// Anchoring matters. Three of the checker's messages end in "and not used",
// and the other two — an unused label, and an unused type-switch guard — are
// real errors that stripping cannot cause but a function literal can still
// produce, because literals keep their bodies. Matching on the suffix alone
// would swallow both.
var unusedImport = regexp.MustCompile(`^"[^"]*" imported( as \S+)? and not used$`)

// collect turns a package's load errors into diagnostics, dropping the ones
// forge's own way of loading creates and the ones it has already reported.
func (s *Session) collect(pkg *packages.Package) {
	// A pattern that resolves to nothing comes back as a synthetic package
	// carrying the go command's complaint. So does a directory whose Go files
	// are all excluded by a build tag, or one that holds no Go files at all —
	// which for forge's purposes is the same answer: there is nothing here to
	// generate for.
	empty := len(pkg.GoFiles) == 0 && pkg.Name == ""

	for _, err := range pkg.Errors {
		if caused(err) {
			continue
		}

		code, fallback := codeBuildError, buildHint
		if empty {
			code, fallback = codeNoPackages, noPackagesHint
		}

		message, hint := splitMessage(err.Msg)
		if hint == "" {
			hint = fallback
		}

		s.add(diag.New(code, s.position(err.Pos), "%s", message).WithHint("%s", hint))
	}
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
