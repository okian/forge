package diag

import (
	"fmt"
	"slices"
	"strconv"
	"sync"
)

// Code is a stable diagnostic identifier.
//
// A code is permanent. Messages get reworded, hints get better, and the rule a
// code reports may be reimplemented entirely, but the number keeps its meaning
// so that anything referring to it — a suppression, a runbook, a search — does
// not rot.
type Code int

// Category groups codes by the stage that reports them.
type Category uint8

const (
	// CategoryInvalid is the zero value: the code falls in no reserved range.
	CategoryInvalid Category = iota

	// CategoryComposition covers stacks that cannot be built: FRG1xxx.
	CategoryComposition

	// CategorySubject covers the subject and its type model: FRG2xxx.
	CategorySubject

	// CategoryOptions covers directive options: FRG3xxx.
	CategoryOptions

	// CategoryEmission covers generation and name collisions: FRG4xxx.
	CategoryEmission

	// CategoryToolchain covers input, output and the toolchain: FRG5xxx.
	CategoryToolchain

	// CategoryLayer covers everything a layer forge does not ship reports:
	// FRG6xxx and above.
	//
	// One category for all of them rather than a range each, because forge
	// cannot hand out ranges to code it has never seen and a reader placing a
	// failure by its number wants to know only that it came from a layer
	// somebody added. Which layer is in the message.
	CategoryLayer
)

// categoryNames gives each category the noun used when describing a range.
var categoryNames = [...]string{
	CategoryInvalid:     "invalid",
	CategoryComposition: "composition",
	CategorySubject:     "subject",
	CategoryOptions:     "options",
	CategoryEmission:    "emission",
	CategoryToolchain:   "toolchain",
	CategoryLayer:       "layer",
}

// String returns the category's lower-case name.
func (c Category) String() string {
	if int(c) >= len(categoryNames) {
		return "category(" + strconv.Itoa(int(c)) + ")"
	}
	return categoryNames[c]
}

// Code range boundaries. Every code lives in exactly one reserved range, which
// is what lets a reader place a failure from its number alone.
//
// Forge's own end at 5999. Everything above belongs to layers forge does not
// ship, which is why there is a ceiling at all rather than none: a code has to
// have four digits to be printed as one, and a code of no category could not be
// placed by a reader or listed beside the others.
const (
	minCode Code = 1000
	forges  Code = 5999
	maxCode Code = 9999
)

// Category returns the reserved range the code falls in, or CategoryInvalid
// for a code outside every range.
func (c Code) Category() Category {
	switch {
	case c < minCode || c > maxCode:
		return CategoryInvalid
	case c > forges:
		return CategoryLayer
	default:
		return Category(c / 1000)
	}
}

// Ours reports whether a code is one forge itself reports.
//
// What it is for is telling a reader where to look. A failure forge raised is
// forge's to explain and is documented with the rest; one a layer raised is
// that layer's, and pointing at forge's index for it would send somebody to the
// wrong place.
func (c Code) Ours() bool { return c >= minCode && c <= forges }

// String returns the code as it is printed and referred to, "FRG1003".
func (c Code) String() string { return fmt.Sprintf("FRG%04d", int(c)) }

// Entry is a registered code together with the canonical one-line summary of
// what it reports.
type Entry struct {
	// Code is the identifier.
	Code Code

	// Summary describes the failure in a few words, in lower case and without
	// terminating punctuation, so that it composes into a longer message.
	Summary string
}

// registry holds every code the linked binary knows about.
//
// Registration belongs in a package-level variable, and the language runs
// package initialisation in a single goroutine, so the ordinary path could not
// race even without a lock. The lock is here for the paths that are not
// initialisation — a test registering a code deliberately, say — so that
// reading the registry is safe whatever else a program is doing.
var registry struct {
	sync.RWMutex
	entries map[Code]string
}

// Register records a code and returns it, so that a package declares its codes
// where it uses them:
//
//	var codeTwoStorageLayers = diag.Register(1003, "two storage layers in stack")
//
// It panics if the code falls outside every reserved range, if the summary is
// empty, or if the code is already registered. All three are programming
// errors that surface the first time the package is linked into a test, and
// none of them is something a user could provoke.
func Register(code Code, summary string) Code {
	if code.Category() == CategoryInvalid {
		panic(fmt.Sprintf("diag: code %d is outside the reserved ranges %d-%d", int(code), int(minCode), int(maxCode)))
	}
	if summary == "" {
		panic(fmt.Sprintf("diag: %s registered without a summary", code))
	}

	registry.Lock()
	defer registry.Unlock()

	if registry.entries == nil {
		registry.entries = make(map[Code]string)
	}
	if existing, ok := registry.entries[code]; ok {
		panic(fmt.Sprintf("diag: %s already registered as %q", code, existing))
	}
	registry.entries[code] = summary

	return code
}

// Summary returns the summary registered for the code, and whether the code is
// registered at all.
func Summary(code Code) (string, bool) {
	registry.RLock()
	defer registry.RUnlock()

	summary, ok := registry.entries[code]
	return summary, ok
}

// Registered returns every registered code in ascending order, which is the
// order a diagnostics index lists them in.
func Registered() []Entry {
	registry.RLock()
	defer registry.RUnlock()

	entries := make([]Entry, 0, len(registry.entries))
	for code, summary := range registry.entries {
		entries = append(entries, Entry{Code: code, Summary: summary})
	}
	slices.SortFunc(entries, func(a, b Entry) int { return int(a.Code - b.Code) })

	return entries
}
