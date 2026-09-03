package forge

// Json generates an append-based, reflection-free JSON codec for the subject,
// driven by its json tags with encoding/json/v2 semantics. The subject gains
// AppendJSON, MarshalJSON, UnmarshalJSON and UnmarshalJSONBorrowed; a container
// above it carries the whole stack in a single pass, allocating nothing beyond
// the growth of the caller's buffer.
//
// Fields whose type cannot be resolved statically — interfaces, any, and
// unresolvable cycles — are reported as errors rather than quietly costing a
// reflective fallback. Marking such a field //forge:json fallback=stdlib opts
// into that fallback explicitly.
//
// Kind: element. Stage: v1. Directive: //forge:json.
type Json[T any] struct{ _ [0]T }

// Validate generates Validate() error for the subject from its validate tags,
// checking fields in declaration order and reporting the first failure with
// its field path. The success path allocates nothing.
//
// Kind: element. Stage: v1. Directive: //forge:validate.
type Validate[T any] struct{ _ [0]T }

// Clone generates a Clone method that deep-copies the subject, following every
// type reachable from it. Pointers, slices and maps obey an explicit aliasing
// policy rather than an assumed one, so a field that is shallow by design can
// stay shallow.
//
// Kind: element. Stage: v1. Directive: //forge:clone.
type Clone[T any] struct{ _ [0]T }

// Hash generates a Hash method returning a content hash of the subject and
// every value reachable from it. The hash is stable across runs and across
// builds, which is what lets a subject with no comparable representation of its
// own serve as a set member or a map key.
//
// Kind: element. Stage: v1. Directive: //forge:hash.
type Hash[T any] struct{ _ [0]T }

// Builder generates a fluent builder type for the subject, one setter per
// field, terminating in Build. Fields tagged validate:"required" are enforced
// there rather than at the setter, so a half-populated value cannot escape the
// builder but the fields may still be set in any order.
//
// Kind: element. Stage: v1. Directive: //forge:builder.
type Builder[T any] struct{ _ [0]T }

// Patch generates a field-mask companion type for the subject together with
// Apply, which writes only the fields the mask sets. This is the shape an HTTP
// PATCH handler wants: absent and zero stay distinguishable.
//
// Kind: element. Stage: v1. Directive: //forge:patch.
type Patch[T any] struct{ _ [0]T }

// Redact generates slog.LogValuer for the subject with redact-tagged fields
// masked, so logging a value cannot leak the fields the subject declares
// sensitive.
//
// Kind: element. Stage: v1. Directive: //forge:redact.
type Redact[T any] struct{ _ [0]T }

// Enum generates the API of a closed set over a named scalar subject,
// discovering its members from the constants declared with it: String, Valid, a
// parser, Values, and a text codec that refuses a value outside the set.
//
// No JSON codec of its own, and none is wanted: encoding/json reaches for a
// text codec where a type has one, so a member goes over the wire under the
// name it is known by rather than as the number behind it. A second codec would
// be a second answer to one question.
//
// It holds inside a codec [Json] generates too, and that takes the two
// declarations together: this one gives the subject a text codec, and a codec
// generated for a struct holding one of its members writes the field through
// it. Neither mentions the other, and both being in one package is what puts
// them together — a declaration in a neighbouring package is not seen, and its
// members go over forge's own wire as the numbers behind them.
//
// So a member goes over either wire under its name, and a value the set has no
// name for is refused wherever a document holding it is written or read. Which
// includes the zero: a set counted from anything but iota's first value has no
// member for it, so the zero value of a struct holding one cannot be encoded
// until the field is set. That is what a closed set means, and it is the same
// answer encoding/json gives.
//
// Kind: element. Stage: v1. Directive: //forge:enum.
type Enum[T any] struct{ _ [0]T }

// Default generates the application of the subject's default tag values to a
// zero or partially populated value. It pairs with Validate: defaults first,
// then the rules that assume them.
//
// Kind: element. Stage: v1.x. The marker is declared so that a declaration
// naming it type-checks; generation reports it as not yet implemented.
// Directive: //forge:default.
type Default[T any] struct{ _ [0]T }

// Diff generates a Diff method that compares two subject values field by
// field, reporting what differs as a list of changes rather than as a boolean.
//
// Kind: element. Stage: v1.x. The marker is declared so that a declaration
// naming it type-checks; generation reports it as not yet implemented.
// Directive: //forge:diff.
type Diff[T any] struct{ _ [0]T }

// Fault generates the error protocol — Error, Unwrap, and the predicates
// errors.Is and errors.As match against — for a subject that models a failure.
// It is requested explicitly and never inferred from a type's name.
//
// Kind: element. Stage: v1.x. The marker is declared so that a declaration
// naming it type-checks; generation reports it as not yet implemented.
// Directive: //forge:fault.
type Fault[T any] struct{ _ [0]T }

// Binary generates a compact binary codec for the subject: the
// encoding.BinaryMarshaler and encoding.BinaryUnmarshaler pair, plus
// encoding.BinaryAppender for callers who want to encode into a buffer they
// already own.
//
// Kind: element. Stage: v1.x. The marker is declared so that a declaration
// naming it type-checks; generation reports it as not yet implemented.
// Directive: //forge:binary.
type Binary[T any] struct{ _ [0]T }

// Slice stores elements in an append-ordered backing array. It is the default
// storage: a refining layer written with no storage beneath it resolves as
// though Slice were written there. Because a refining marker is itself declared
// as a slice, that default is also what an inline Collection[Person]
// declaration's []Person underlying type honestly represents.
//
// Kind: storage. Stage: v1. Directive: //forge:slice.
type Slice[T any] []T

// Ring stores elements in a fixed-capacity circular buffer, so a long-running
// producer cannot grow memory without bound. Capacity is declared with cap,
// and overflow chooses between overwriting the oldest element and reporting an
// error.
//
// Because a ring's head index and length are invariants that raw slice access
// would corrupt, a declaration using Ring belongs in a spec file.
//
// Kind: storage. Stage: v1. Directive: //forge:ring.
type Ring[T any] []T

// Set stores at most one element per distinct key, deduplicating on insert by
// a declared key field or by the subject's content hash.
//
// Kind: storage. Stage: v1.x. The marker is declared so that a declaration
// naming it type-checks; generation reports it as not yet implemented.
// Directive: //forge:set.
type Set[T any] []T

// LRU stores a bounded number of elements keyed by a declared field, evicting
// the least recently used one when it is full.
//
// Kind: storage. Stage: v1.x. The marker is declared so that a declaration
// naming it type-checks; generation reports it as not yet implemented.
// Directive: //forge:lru.
type LRU[T any] []T

// Index stores elements alongside a lookup structure over a declared field,
// unique or multi-valued, turning a scan into a map access.
//
// Kind: storage. Stage: v1.x. The marker is declared so that a declaration
// naming it type-checks; generation reports it as not yet implemented.
// Directive: //forge:index.
type Index[T any] []T

// Heap stores elements in priority order by a declared key, so the extreme
// element is always the cheap one to reach.
//
// Kind: storage. Stage: v1.x. The marker is declared so that a declaration
// naming it type-checks; generation reports it as not yet implemented.
// Directive: //forge:heap.
type Heap[T any] []T

// Collection adds the query surface a storage layer cannot express on its own,
// specialised to the subject's fields: a lazy sequence view, projections of
// individual fields, sorted views for each declared sort key, and lookup maps
// for each declared index.
//
// Kind: refining. Stage: v1. Directive: //forge:collection.
type Collection[T any] []T

// Sorted maintains the order of the storage beneath it on insert, by a
// declared key, rather than sorting on demand.
//
// Kind: refining. Stage: v1.x. The marker is declared so that a declaration
// naming it type-checks; generation reports it as not yet implemented.
// Directive: //forge:sorted.
type Sorted[T any] []T

// Page adds offset and cursor windowing over an ordered, sized stack, so a
// caller can walk a large collection without materialising it.
//
// Kind: refining. Stage: v1.x. The marker is declared so that a declaration
// naming it type-checks; generation reports it as not yet implemented.
// Directive: //forge:page.
type Page[T any] []T

// Guarded protects the stack beneath it with a sync.RWMutex and replaces its
// iteration surface with scoped access: Do and RDo run a function against a
// view of the inner stack while the corresponding lock is held, and Snapshot
// copies out so the caller can work lock-free.
//
// The view type deliberately omits Do and RDo, so a re-entrant call — the way
// a generated lock would otherwise deadlock — cannot be written. Encoding takes
// a snapshot first under encode=snapshot, the default, so that a slow
// io.Writer cannot hold the lock for the length of a network round trip;
// encode=locked encodes in place, without the copy, for callers who know their
// sink is fast.
//
// Kind: decorator. Stage: v1. Directive: //forge:guarded.
type Guarded[T any] []T

// Atomic publishes the stack beneath it through an atomic pointer, so a read
// is one atomic load with no lock and no allocation. Writers clone, modify and
// publish, which costs a copy of the whole stack per write and suits
// read-mostly data.
//
// Kind: decorator. Stage: v1.x. The marker is declared so that a declaration
// naming it type-checks; generation reports it as not yet implemented.
// Directive: //forge:atomic.
type Atomic[T any] []T

// Csv encodes and decodes the whole stack as CSV, mapping the subject's fields
// to a header row.
//
// Kind: transport. Stage: v1.x. The marker is declared so that a declaration
// naming it type-checks; generation reports it as not yet implemented.
// Directive: //forge:csv.
type Csv[T any] []T

// Map generates a constructor that builds the second type from the first:
// members matched by name where that is unambiguous and assignable, settled by
// a //forge:map hint where it is not, and refused where they are neither. The
// source may be a struct or an interface; the target gains nothing — the
// constructor is a package function named from both, PersonFromUser for
// Map[User, Person].
//
// Kind: bridge. Stage: v1. Directive: //forge:map.
type Map[S, T any] struct {
	_ [0]S
	_ [0]T
}
