package generate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"github.com/okian/forge/internal/diag"
	"github.com/okian/forge/internal/discover"
	"github.com/okian/forge/internal/emit"
	"github.com/okian/forge/internal/merge"
	"github.com/okian/forge/internal/model"
)

// codeSkipUnclaimed reports a skip directive naming something the declaration
// was never going to claim.
//
// A 3xxx, because it is about a directive somebody wrote. It is reported rather
// than passed over for the reason every silent option is reported here: an
// author who turned something off and was not told it was already off believes
// they know what the declaration does, and they are wrong about a different
// thing than they think.
var codeSkipUnclaimed = diag.Register(3019, "skip turns nothing off")

// codeSkipRepeated reports the same interface skipped twice.
//
// The second one turns nothing off, which is the same thing wrong with the case
// above and deserves the same answer. Left silent it reads as two decisions
// where there is one, and an author who removed what they took for the only
// skip would find the claim still gone.
var codeSkipRepeated = diag.Register(3020, "skip repeats one already written")

// codeWalkElement reports a walk answering with something other than the
// declaration's elements.
//
// A 4xxx rather than a 3xxx: nobody wrote it in a directive. Either a layer
// produced a walk over the wrong thing, or an author wrote one in place of the
// generated method and changed what it yields — and the second is the one worth
// pointing at, since the stack above a walk is written against what it hands
// back.
var codeWalkElement = diag.Register(4017, "a walk answers with something other than the declaration's elements")

// The packages the interfaces below are declared in, and the ones their methods
// name.
//
// Named once because they are compared as well as written: what a claim reads
// as depends on what the file binds each path to, and a path written two ways
// in one table would be two paths as far as that comparison is concerned.
var (
	stdIO       = model.Import{Path: "io", Name: "io"}
	stdJSON     = model.Import{Path: "encoding/json/v2", Name: "json"}
	stdJSONText = model.Import{Path: "encoding/json/jsontext", Name: "jsontext"}
	stdIter     = model.Import{Path: "iter", Name: "iter"}
	stdFmt      = model.Import{Path: "fmt", Name: "fmt"}
	stdEncoding = model.Import{Path: "encoding", Name: "encoding"}
	stdSort     = model.Import{Path: "sort", Name: "sort"}
	stdSlog     = model.Import{Path: "log/slog", Name: "slog"}
	stdSync     = model.Import{Path: "sync", Name: "sync"}
)

// tabled is every package a claim in this file can name, by path.
//
// It is the whole of what synthesis knows how to write, and that is what makes
// it usable as an identity: a package outside it is written by its import path
// instead of by a name, which is not Go and is not meant to be. A package of
// somebody's own called io is then spelled myapp/io.Writer where the standard
// library's is spelled io.Writer, and no comparison can confuse them.
//
// Guessing from the shape of the path is the alternative, and it is wrong: a
// module path is only conventionally a hostname, `module myapp` is legal and
// ordinary in a private tree, and myapp/io would pass any such test.
var tabled = map[string]model.Import{
	stdIO.Path:       stdIO,
	stdJSON.Path:     stdJSON,
	stdJSONText.Path: stdJSONText,
	stdIter.Path:     stdIter,
	stdFmt.Path:      stdFmt,
	stdEncoding.Path: stdEncoding,
	stdSort.Path:     stdSort,
	stdSlog.Path:     stdSlog,
	stdSync.Path:     stdSync,
}

// spelled is one type a method's signature names, held as the package it comes
// from and its name within that package.
//
// A path and a name rather than the text, because how a package is written is
// the file's to decide. A layer that binds encoding/json/v2 under some other
// name writes its types under that name, and a table holding the fixed string
// "json.MarshalerTo" would read code that is perfectly correct as not matching.
type spelled struct {
	// from is the package the type is declared in, and is the zero value for a
	// predeclared type, which no file imports and no file qualifies.
	from model.Import

	// name is the type's own name, and ptr records that what the method names
	// is a pointer to it.
	name string
	ptr  bool
}

// written returns the type as a file writes it, given what that file calls the
// packages it imports, and whether the file can write it at all.
func (s spelled) written(as binding) (string, bool) {
	out := s.name

	if s.from.Path != "" {
		held, can := as.name(s.from)
		if !can {
			return "", false
		}
		out = held + "." + s.name
	}

	if s.ptr {
		out = "*" + out
	}
	return out, true
}

// binding is what a file imports, by path.
type binding map[string]emit.Import

// bindings reads what a declaration's output has already asked to import, and
// what is bound beside it.
//
// Read rather than assumed, because the file is written once and everything in
// it has to agree. A claim written with forge's own idea of a package's name,
// in a file where a layer bound that path to something else, is either a name
// nothing declares or a second import of one path — and the second is refused
// outright when the file is written.
//
// Beside it, because the element is spelled before any of this runs and may
// have had to be renamed on the way: a package whose own name a layer already
// took is bound under another one by the spelling, and that binding is carried
// with the spelling rather than with the unit. A comparison that read only the
// unit would write the element one way and read a method naming it another,
// then report two spellings of one type as a disagreement about the type.
//
// What makes the second argument safe is that a spelling records an import only
// where it had to invent a binding — one the file already has is honoured and
// not recorded again. So beside is empty except in the arrangement it exists
// for, and in every other it changes nothing at all.
//
// That arrangement needs a storage layer whose backing type does not name the
// element, since one that does has already recorded the import and there is
// nothing left to diverge from. No storage in this build is like that, and none
// has to be: a store keyed by something other than the element, or one holding
// an opaque handle, would be. This does not depend on which.
//
// The import is kept whole rather than reduced to its name, because a name is
// half of what a file needs: whether it has to be written out in the import
// line is the other half, and an import that lost it binds one name and is
// referred to by another.
func bindings(imports []emit.Import, beside ...emit.Import) binding {
	out := make(binding, len(imports)+len(beside))

	for _, one := range imports {
		if one.Path != "" && one.Name != "" {
			out[one.Path] = one
		}
	}
	for _, one := range beside {
		if one.Path != "" && one.Name != "" {
			out[one.Path] = one
		}
	}

	return out
}

// binds returns the import a claim naming this package needs, and whether the
// file can name it at all.
//
// It cannot when some other path is already bound to the name: the name is
// taken, and a claim written with it would be a claim about somebody else's
// package. Two paths under one name is refused when the file is written, and
// arriving there through a claim forge invented would report it as forge's bug
// — which, at that point, it would be.
func (b binding) binds(one model.Import) (emit.Import, bool) {
	if held, is := b[one.Path]; is {
		return held, true
	}
	if b.taken(one.Name) {
		return emit.Import{}, false
	}
	return emit.Import{Path: one.Path, Name: one.Name}, true
}

// name returns what the file calls the package, and whether it can name it.
func (b binding) name(one model.Import) (string, bool) {
	held, can := b.binds(one)
	return held.Name, can
}

// taken reports whether the file already binds some path to a name.
func (b binding) taken(name string) bool {
	for _, held := range b {
		if held.Name == name {
			return true
		}
	}
	return false
}

// synthetic is one interface synthesis may assert about the declared type.
//
// A table rather than a question asked of go/types, and the difference is what
// it is asked *of*. What is being decided is whether the declarations this run
// is about to write add up to an interface — and they are not compiled yet, so
// there is no type to ask. What there is is their syntax, and a table written
// in the same terms is what lines up against it.
type synthetic struct {
	// from is the package the interface is declared in and name is its own
	// name, so that a claim can be written the way the file binds that package.
	from model.Import
	name string

	// needs are the methods the interface asks for, each by name and by the
	// types it takes and answers with.
	needs []wants
}

// ref returns the interface as a claim writes it, given the import the file
// binds its package under.
func (s synthetic) ref(bound emit.Import) string { return bound.Name + "." + s.name }

// spelled returns the interface as forge names it, whatever this file calls the
// package.
//
// What a skip directive is matched against, beside the file's own spelling. An
// author writing //forge:skip io.WriterTo has named the interface the way Go
// names it, and holding them to an alias a layer chose — in a file they have
// not seen yet, because they are configuring the run that writes it — would be
// asking them to know the answer before the question.
func (s synthetic) spelled() string { return s.from.Name + "." + s.name }

// wants is one method an interface asks for.
type wants struct {
	name    string
	params  []spelled
	results []spelled
}

// has is one method a declaration turns out to have, with its parameters and
// results as the file declaring them writes them.
//
// Text rather than types, because half of these methods do not exist yet: they
// are declarations this run is about to write, and the only account of them is
// the syntax the layers produced.
type has struct {
	params  []string
	results []string

	// pointer records that the method is declared on the pointer, which is what
	// decides how a claim about it has to be written: a value's method set
	// holds only its own methods, so a claim naming the value is a claim only
	// the value-receiver half can answer.
	pointer bool
}

// synthesised is every interface this build can decide about, in the order the
// assertions are written.
//
// Only the ones a layer in this build can supply. A row for an interface
// nothing produces is a row nothing exercises, and a table nothing exercises is
// a table that is wrong the day somebody adds the layer it was written for.
var synthesised = []synthetic{
	{
		from: stdIO, name: "WriterTo",
		needs: []wants{{
			name:    "WriteTo",
			params:  []spelled{{from: stdIO, name: "Writer"}},
			results: []spelled{{name: "int64"}, {name: "error"}},
		}},
	},
	{
		from: stdIO, name: "ReaderFrom",
		needs: []wants{{
			name:    "ReadFrom",
			params:  []spelled{{from: stdIO, name: "Reader"}},
			results: []spelled{{name: "int64"}, {name: "error"}},
		}},
	},
	{
		from: stdJSON, name: "MarshalerTo",
		needs: []wants{{
			name:    "MarshalJSONTo",
			params:  []spelled{{from: stdJSONText, name: "Encoder", ptr: true}},
			results: []spelled{{name: "error"}},
		}},
	},
	{
		from: stdJSON, name: "UnmarshalerFrom",
		needs: []wants{{
			name:    "UnmarshalJSONFrom",
			params:  []spelled{{from: stdJSONText, name: "Decoder", ptr: true}},
			results: []spelled{{name: "error"}},
		}},
	},
	{
		from: stdFmt, name: "Stringer",
		needs: []wants{{name: "String", results: []spelled{{name: "string"}}}},
	},
	{
		from: stdEncoding, name: "TextAppender",
		needs: []wants{{
			name:    "AppendText",
			params:  []spelled{{name: "[]byte"}},
			results: []spelled{{name: "[]byte"}, {name: "error"}},
		}},
	},
	{
		from: stdEncoding, name: "TextMarshaler",
		needs: []wants{{name: "MarshalText", results: []spelled{{name: "[]byte"}, {name: "error"}}}},
	},
	{
		from: stdEncoding, name: "TextUnmarshaler",
		needs: []wants{{
			name:   "UnmarshalText",
			params: []spelled{{name: "[]byte"}}, results: []spelled{{name: "error"}},
		}},
	},
	{
		from: stdSort, name: "Interface",
		needs: []wants{
			{name: "Len", results: []spelled{{name: "int"}}},
			{
				name:   "Less",
				params: []spelled{{name: "int"}, {name: "int"}}, results: []spelled{{name: "bool"}},
			},
			{name: "Swap", params: []spelled{{name: "int"}, {name: "int"}}},
		},
	},
	{
		from: stdSlog, name: "LogValuer",
		needs: []wants{{name: "LogValue", results: []spelled{{from: stdSlog, name: "Value"}}}},
	},
	{
		// Only ever earned by a declaration that asked for it. A concurrency
		// layer holds a lock and does not export it, because a caller holding
		// the lock directly can reach nothing it guards — so the two methods
		// exist only where the declaration said to write them, and the row is
		// here so that the one that did says so where a reader looks for it.
		from: stdSync, name: "Locker",
		needs: []wants{{name: "Lock"}, {name: "Unlock"}},
	},
}

// claim is one interface a declaration is about to say it satisfies, and which
// of the type and its pointer says it.
type claim struct {
	ref     string
	through bool
}

// written returns the claim as the file writes it.
//
// Through a pointer where the methods are on one, and through the zero value
// otherwise. The zero value is spelled *new(T) rather than T{}, which reads
// better and is not valid for every type a declaration can be: a composite
// literal needs a struct, an array, a slice or a map underneath it, and a
// declaration over a named basic — which is what an enum is — has none of
// those. A claim that does not compile in somebody's package is a worse trade
// than four characters of noise in one that does.
func (c claim) written(declared string) string {
	if c.through {
		return "_ " + c.ref + " = (*" + declared + ")(nil)"
	}
	return "_ " + c.ref + " = *new(" + declared + ")"
}

// synthesis is what deciding what a declaration claims needs to know.
type synthesis struct {
	// declared is the type the claims are about, and elem how its elements are
	// written in the file being generated into.
	declared string
	elem     model.Spelling

	// pkg is the import path of the package being generated into, since a type
	// from it is written with no package name at all.
	pkg string

	// at is where the declaration was written, which is where anything wrong
	// with what it turned out to offer is reported.
	at token.Position

	// held is what the package already declares, since a method the author
	// wrote counts towards an interface exactly as a generated one does.
	held declared

	// skipped names the interfaces the declaration asked not to claim.
	skipped []discover.Directive
}

// synthesise returns the assertions a declaration's output earns, and reports a
// skip that turns nothing off.
//
// What is asserted is what the declarations add up to rather than what the
// stack was expected to produce. The difference matters because an assertion is
// a claim in a file somebody else compiles: one made from the shape would be a
// claim about what a layer said it would do, and one made from the declarations
// is a claim about what it did.
func synthesise(held merge.Unit, of synthesis, diags *diag.Set) (emit.Section, []emit.Import, claimable, bool) {
	as := bindings(held.Imports, importing(of.elem)...)
	have := methods(held.Sections, of, as)

	skipped := named(of.skipped)
	earned, unnameable, claims, imports := sifted(have, as, skipped)

	walked, walking := walk(have, of, as, diags)

	// What was claimed goes back to the caller rather than being judged here.
	// A skip is written on a declaration and turns off a claim about it or
	// about its subject, and those two are decided in different places — so
	// neither of them knows enough to say a directive turned nothing off, and
	// the run answers for all of them once both have run.
	made := claimable{earned: earned, unnameable: unnameable, walking: walking}

	// Everything from here is about what gets written, and for that the only
	// question is whether there is a signature to write. Whether there was a
	// walk to talk about is a different question, which unclaimed above has
	// already been asked: a walk that was reported is one there is nothing to
	// write about and something to say about.
	if slices.Contains(skipped, walkedRef) {
		walked = ""
	}
	if walked != "" {
		// The walk answered with a signature, so the file can name iter: walk
		// asks first and hands nothing back when it cannot.
		bound, _ := as.binds(stdIter)

		imports = append(imports, bound)

		// The element's own imports go in with it, and only with it. It is the
		// one place where what was decided from and what the file receives are
		// not the same set: a declaration that does not walk never names the
		// element, so nothing it writes needs the import — while the binding
		// still counted towards what the claims above could be written under.
		imports = append(imports, importing(of.elem)...)
	}

	if len(claims) == 0 && walked == "" {
		return emit.Section{}, nil, made, false
	}

	section, err := asserted(of, claims, walked)
	if err != nil {
		diags.AddError(err)
		return emit.Section{}, nil, made, false
	}

	return section, imports, made, true
}

// sifted sorts every row into what the declarations earned, what they earned
// and the file cannot name, and what survives the skips.
//
// Earned and claimed are kept apart, and the difference is a skip. What was
// earned is what the declarations add up to; what is claimed is what is left
// after the author turned some of it off. A single list would have nothing left
// to compare a skip against, and every skip would look like one that named
// something the declaration never claimed.
func sifted(
	have map[string]has, as binding, skipped []string,
) (earned, unnameable []string, claims []claim, imports []emit.Import) {
	for _, row := range synthesised {
		through, does := satisfies(have, row, as)
		if !does {
			continue
		}

		bound, can := as.binds(row.from)
		if !can {
			// The name a claim would need belongs to something else in this
			// file, so nothing that could be written here would be about this
			// interface. Kept apart from what was never earned, because an
			// author who skips it is not making the mistake that answer
			// describes: the methods did add up, and the file could not say so.
			unnameable = append(unnameable, row.spelled())
			continue
		}

		ref := row.ref(bound)
		earned = append(earned, ref, row.spelled())

		if slices.Contains(skipped, ref) || slices.Contains(skipped, row.spelled()) {
			continue
		}

		claims = append(claims, claim{ref: ref, through: through})
		imports = append(imports, bound)
	}

	return earned, unnameable, claims, imports
}

// walkedRef is what a skip directive names to turn the walk's own assertion
// off. It is not an interface, and is written as the thing it is about.
const walkedRef = "All"

// satisfies reports whether the methods a declaration ends up with add up to an
// interface, as the file being written spells both, and whether the claim has
// to be written about the pointer.
//
// It has to whenever any of the methods is declared on one. A value's method
// set holds only the methods declared on the value, so a claim naming the value
// would not compile — and where every method is on the value, naming the
// pointer would compile and would understate what the type does.
func satisfies(have map[string]has, one synthetic, as binding) (through, does bool) {
	for _, need := range one.needs {
		got, declares := have[need.name]
		if !declares {
			return false, false
		}
		if !agrees(got.params, need.params, as) || !agrees(got.results, need.results, as) {
			return false, false
		}
		through = through || got.pointer
	}
	return through, true
}

// agrees reports whether a method's parameters or results are the ones an
// interface asks for.
func agrees(got []string, want []spelled, as binding) bool {
	if len(got) != len(want) {
		return false
	}
	for i, one := range want {
		held, can := one.written(as)
		if !can || got[i] != held {
			return false
		}
	}
	return true
}

// walk returns the walk's own signature as a method expression is checked
// against, and whether there is one.
//
// A method expression rather than an interface assertion, because it is checked
// at compile time and calls nothing: an interface assertion has to name a
// value, and this costs neither an allocation nor a line of initialisation.
// Through the pointer whichever receiver the walk takes, since the pointer's
// method set holds both and the value's holds only half.
//
// The element in the signature is the declaration's, not the one the method was
// found to yield — which is the only arrangement under which the claim checks
// anything. Built from the method's own result it would hold however wrong that
// result was, and the single regression it could still catch is All going
// missing altogether. Built from the element, a container that walks the wrong
// thing fails where the claim is written rather than wherever somebody ranges
// over it.
func walk(have map[string]has, of synthesis, as binding, diags *diag.Set) (walked string, present bool) {
	held, declares := have[walkedRef]
	if !declares || len(held.params) != 0 || len(held.results) != 1 {
		return "", false
	}

	name, can := as.name(stdIter)
	if !can {
		return "", false
	}

	sequence := name + ".Seq["
	if !strings.HasPrefix(held.results[0], sequence) {
		return "", false
	}

	// Both spellings are in hand, so a disagreement between them is answered
	// here rather than written out. Writing it would be correct in one sense —
	// the claim is what the declaration is supposed to be — and useless in
	// every other: what reaches the author is a package that does not build,
	// pointing at a file they may not edit, about a line they did not write.
	want := sequence + of.elem.Text + "]"
	if held.results[0] != want {
		diags.Add(diag.New(codeWalkElement, of.at,
			"%s walks %s where its elements are %s",
			of.declared, held.results[0], of.elem.Text).
			WithHint("%s", "a walk answers with the declaration's own element type; "+
				"a method written in place of the generated one has to answer with the same"))

		// Present all the same, so that a skip naming it is not also told the
		// declaration has no walk — which would be the opposite of what just
		// happened.
		return "", true
	}

	return "func(*" + of.declared + ") " + want, true
}

// claimable is what a skip is answered against: what the declaration turned out
// to claim, and what it turned out it could not.
type claimable struct {
	// earned holds every claim the declarations added up to, in both the
	// spelling this file uses and the one Go uses.
	//
	// What was earned rather than what survives the skips, since by then every
	// skipped name has been removed and each one would look like a name that
	// matched nothing.
	earned []string

	// unnameable holds the ones the file cannot write, which is a different
	// answer to a different question and gets a different sentence.
	unnameable []string

	// walking records that there is a walk to talk about, whether or not one
	// ended up claimed.
	walking bool
}

// with returns what two stages claimed between them, keeping this one's walk.
//
// The walk stays this declaration's because it is the only part of a claim that
// is about the declaration alone: a subject has no walk, and a skip naming one
// on a declaration that does not walk is a mistake however many other
// declarations in the package do.
func (c claimable) with(other claimable) claimable {
	return claimable{
		earned:     append(append([]string(nil), c.earned...), other.earned...),
		unnameable: append(append([]string(nil), c.unnameable...), other.unnameable...),
		walking:    c.walking,
	}
}

// unclaimed reports a skip that turns nothing off.
//
// Held against what the run claimed rather than against what one stage of it
// did. A skip turns off a claim about a declaration or about its subject, and
// those are decided in two places — so neither of them is in a position to say
// a directive turned nothing off, and this is asked once both have.
func unclaimed(held claimable, of judgement, diags *diag.Set) {
	seen := make(map[string]token.Position, len(of.skipped))

	for _, one := range of.skipped {
		want := strings.TrimSpace(one.Args)

		switch first, twice := seen[want]; {
		case want == "":
			diags.Add(diag.New(codeSkipUnclaimed, one.Pos,
				"%s names nothing to skip", one.Text).
				WithHint("%s", "write the interface after it, as in //forge:skip io.WriterTo"))

		case twice:
			diags.Add(diag.New(codeSkipRepeated, one.ArgsPos(),
				"%s is already skipped", want).
				WithHint("the first is at %s; one skip turns one claim off", first))

		case slices.Contains(held.earned, want), want == walkedRef && held.walking:
			// Turned something off, which is what it is for.

		case slices.Contains(held.unnameable, want):
			diags.Add(diag.New(codeSkipUnclaimed, one.ArgsPos(),
				"%s satisfies %s, but this file cannot name it, so nothing claims it",
				of.declared, want).
				WithHint("%s", "a layer has bound the package name this claim would need to "+
					"something else; the skip is not needed and the claim is already absent"))

		default:
			diags.Add(diag.New(codeSkipUnclaimed, one.ArgsPos(),
				"%s does not claim %s, so skipping it turns nothing off", of.declared, want).
				WithHint("%s", "the interfaces a declaration claims are the ones its methods add up to, "+
					"and the generated file lists them in one var block near its end"))
		}

		// Recorded whether or not it turned anything off, so that a name
		// written twice is reported as a repeat the second time rather than as
		// the same mistake over again.
		if _, already := seen[want]; want != "" && !already {
			seen[want] = one.ArgsPos()
		}
	}
}

// named returns what the skip directives asked to turn off.
func named(skipped []discover.Directive) []string {
	out := make([]string, 0, len(skipped))
	for _, one := range skipped {
		out = append(out, strings.TrimSpace(one.Args))
	}
	return out
}

// methods returns the methods the declared type ends up with, by name, as the
// file being written spells them.
//
// Generated and authored together, because an interface does not care which. A
// method the author wrote in place of a generated one is the method the type
// has, and a claim that ignored it would be a claim about a file rather than
// about a type.
//
// What the author wrote means what they declared. A method promoted from an
// embedded type counts towards an interface as far as the compiler is
// concerned and is not counted here, so a declaration that embeds its way to
// io.WriterTo goes unclaimed. The method set that would find it would also find
// what forge itself wrote last time, and counting that makes every run disagree
// with the one before it — a claim that is merely missing costs a reader a line
// they could have had, where generation that is not idempotent costs everybody
// their diffs.
func methods(sections []emit.Section, of synthesis, as binding) map[string]has {
	out := make(map[string]has)

	for _, section := range sections {
		for _, decl := range section.Decls {
			fn, on, is := methodOn(decl)
			if !is || on != of.declared {
				continue
			}

			out[fn.Name.Name] = has{
				params:  rendered(fn.Type.Params),
				results: rendered(fn.Type.Results),
				pointer: indirect(fn),
			}
		}
	}

	for name, one := range of.held.methods[of.declared] {
		signature, is := one.Type().(*types.Signature)
		if !is {
			continue
		}
		_, through := signature.Recv().Type().(*types.Pointer)

		out[name] = has{
			params:  tupled(signature.Params(), of.pkg, as),
			results: tupled(signature.Results(), of.pkg, as),
			pointer: through,
		}
	}

	return out
}

// indirect reports whether a method is declared on the pointer.
func indirect(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	_, through := fn.Recv.List[0].Type.(*ast.StarExpr)
	return through
}

// rendered returns the types a parameter or result list holds, as they are
// written.
func rendered(list *ast.FieldList) []string {
	if list == nil {
		return nil
	}

	var out []string
	for _, field := range list.List {
		written := types.ExprString(field.Type)
		for range max(len(field.Names), 1) {
			out = append(out, written)
		}
	}
	return out
}

// tupled returns the types of a signature's parameters or results, spelled as a
// file in the package at in writes them.
//
// The types come from the type checker, which knows them by the package they
// were declared in. What is wanted is how they are written where the claim
// goes, and [spelling] is the difference.
func tupled(held *types.Tuple, in string, as binding) []string {
	if held == nil {
		return nil
	}

	written := spelling(in, as)

	out := make([]string, 0, held.Len())
	for i := range held.Len() {
		out = append(out, types.TypeString(held.At(i).Type(), written))
	}
	return out
}

// spelling names a package the way a claim in the package at in compares it.
//
// Mostly the way the file writes it: the package being generated into is named
// with nothing, since a file does not qualify its own declarations; one the
// table knows, or one the file imports, is named the way the file names it; and
// anything else is named by its own name, which is what a file that came to
// import it would call it.
//
// The exception is a package whose name is one a claim already writes something
// else under. That one is named by its import path — which is not Go and is not
// meant to be. It is the only case where two packages would be written the same
// way, so it is the only case where a comparison could confuse them, and a path
// is the one spelling nothing else can produce. Doing it to every package
// outside the table would be simpler and wrong in the other direction: it would
// put a path where the walk's own element is written, and the walk is compared
// against Go.
//
// A predeclared type belongs to no package and arrives as a nil one. Go's own
// printer does not ask about those today, and a generator that panicked the day
// it started to would be a poor trade for the line it saves.
func spelling(in string, as binding) types.Qualifier {
	return func(p *types.Package) string {
		if p == nil || p.Path() == in {
			return ""
		}

		if one, known := tabled[p.Path()]; known {
			held, can := as.name(one)
			if !can {
				// The file cannot write this package at all, so no claim about
				// it is written either, and nothing here has to stay distinct
				// from anything: what keeps a row from being earned wrongly is
				// that written answers the same "cannot" and agrees with
				// nothing.
				return p.Path()
			}
			return held
		}

		if held, imported := as[p.Path()]; imported {
			return held.Name
		}
		if shadowing(p.Name(), as) {
			return p.Path()
		}
		return p.Name()
	}
}

// shadowing reports whether a name already means something else here.
//
// Two ways it can. The file may import a package under it, which is the case
// that matters for a subject and its neighbours: two packages whose last
// element is the same word are ordinary, and only one of them can be domain.
// Or a claim may be written under it, which is the case that matters for the
// table: an io nobody imports is still what io.WriterTo is written with.
//
// Asked of what the file calls the table's packages rather than of their own
// names, because that is what a claim is written with: an io bound as stdio
// leaves the name io free, and a package of somebody's own may have it.
func shadowing(name string, as binding) bool {
	if as.taken(name) {
		return true
	}

	for _, one := range tabled {
		if held, can := as.name(one); can && held == name {
			return true
		}
	}
	return false
}

// importing returns what the element's own spelling binds, which the walk's
// signature names.
func importing(held model.Spelling) []emit.Import {
	out := make([]emit.Import, 0, len(held.Imports))
	for _, one := range held.Imports {
		out = append(out, emit.Import{Path: one.Path, Name: one.Name, Aliased: one.Aliased})
	}
	return out
}

// asserted builds the declarations that make the claims.
func asserted(of synthesis, claims []claim, walked string) (emit.Section, error) {
	w := &strings.Builder{}

	if len(claims) > 0 {
		w.WriteString("// " + of.declared + " satisfies these.\n")
		w.WriteString("//\n")
		w.WriteString("// The claim is checked when the package is built rather than when a caller\n")
		w.WriteString("// first tries, so a stack that stops satisfying one of these fails here\n")
		w.WriteString("// rather than at somebody's call site. And a reader who is not going to read\n")
		w.WriteString("// forty methods can see what they add up to.\n")
		w.WriteString("var (\n")
		for _, one := range claims {
			w.WriteString("\t" + one.written(of.declared) + "\n")
		}
		w.WriteString(")\n\n")
	}

	if walked != "" {
		w.WriteString(walkingSays(len(claims) > 0))
		w.WriteString("var _ " + walked + " = (*" + of.declared + ")." + walkedRef + "\n\n")
	}

	decls, comments, fset, err := parsedGo(w.String(), of.declared)
	if err != nil {
		return emit.Section{}, err
	}

	return emit.Section{Decls: decls, Comments: comments, Fset: fset}, nil
}

// walkingSays returns the comment above the walk's claim, which depends on
// whether anything was claimed above it.
//
// Two forms rather than one, because the shorter reads as a continuation and
// there is nothing for it to continue in a file that claims no interfaces. A
// comment opening mid-thought is a small thing to fix and an odd thing to leave
// in a file the reader did not write and cannot edit.
func walkingSays(after bool) string {
	if after {
		return "// And the walk's own signature, checked without calling it.\n" +
			"//\n" +
			"// A method expression is resolved when the package is built and costs nothing\n" +
			"// at run time, where an interface assertion would have to name a value and\n" +
			"// initialise it.\n"
	}

	return "// The walk's own signature, checked when the package is built.\n" +
		"//\n" +
		"// A method expression is resolved by the compiler and costs nothing at run\n" +
		"// time, where an interface assertion would have to name a value and initialise\n" +
		"// it. What it holds the walk to is the element type of the declaration, so a\n" +
		"// container that walks something else fails here rather than wherever somebody\n" +
		"// ranges over it.\n"
}

// parsedGo reads assembled source back as declarations.
//
// The assertions are assembled as text for the reason every layer's output is:
// what is written is a handful of lines a person will read, and a tree for them
// is many times their size. The cost is the possibility of writing something
// that is not Go, and it is paid here — an error about the declaration rather
// than a file on disk that does not build.
func parsedGo(source, about string) ([]ast.Decl, []*ast.CommentGroup, *token.FileSet, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "synth.go", "package forge\n\n"+source, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("what %s claims is not valid Go: %w", about, err)
	}

	return file.Decls, file.Comments, fset, nil
}
