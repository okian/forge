package people

// Status is what a credential is, out of a closed set of things it may be.
//
// A named integer with constants declared against it is an enumeration in
// everything but what it can do, and what it cannot do is say its own name,
// take one back, or refuse a value nobody declared. The declaration in spec.go
// asks for those.
//
// The zero is a member on purpose. A credential nobody has said anything about
// is pending, not invalid, and a set whose zero meant "wrong" would make every
// unset field a fault.
type Status int

// The statuses a credential can be in, in the order they read.
//
// Counted from iota, which is why declaration order matters. The names are what
// a reader of Go sees and the numbers are nobody's to write down — but they are
// what [Statuses] warns goes over forge's own wire, so a member inserted here
// renames every stored credential holding one below it. Adding at the end costs
// nothing and moves nothing.
const (
	StatusPending Status = iota
	StatusActive
	StatusRevoked
)

// Credential is what somebody signs in with.
//
// It is the third subject of this package, after [Person] and [Status], and it
// is here because two layers have nothing to demonstrate over a Person: a
// closed set needs a named scalar, and the redaction layer only earns its keep
// when the secret is a struct down. A redact tag on a field of the subject
// buys a masking log value with no layer at all, which is what [Person.Email]
// shows; reaching past the subject into what it holds is what the layer adds.
type Credential struct {
	// Owner is who the credential belongs to, by the ID a Person is indexed
	// under.
	Owner int

	// State is the closed set, which is the field the enumeration is for.
	//
	// Read [Statuses] before believing this field encodes as its name. It does
	// through the standard library, which reaches for the text codec the
	// enumeration writes; it does not through the codec generated here, which
	// sees a named integer and writes the integer.
	State Status

	// Secret is held behind a pointer, and is nil for a credential that has
	// none.
	//
	// A pointer because a revoked credential keeps its owner and its state and
	// gives up its token, so absent is a state the field has to be able to be
	// in. It is also the shape a generated log value has to guard: slog
	// recovers a panic in a LogValue and logs the stack trace where the field
	// should have been, so a method that dereferenced this without asking would
	// turn a nil secret into a log line nobody can read.
	//
	// Held rather than inlined, so that what must be kept out of a log is one
	// struct down from the value being logged. That is the case a method on the
	// outer type alone does not cover, and the reason the layer walks what a
	// subject reaches: slog resolves one value at a time, so a Credential that
	// handed its Secret over unchanged would have the handler reach in and
	// print the token.
	Secret *Secret
}

// Secret is the half of a credential that must not be logged.
//
// A struct of its own rather than two fields on [Credential], because that is
// how a real one is usually held — the thing that proves who you are travels
// together with the thing that says when it stops proving it.
type Secret struct {
	// Issued is when the token was minted, which is not a secret and is worth
	// having in a log.
	Issued string

	// Token is the secret. The tag is what keeps it out of a log, and the
	// generated log value is what makes the tag mean anything: without one,
	// slog reaches for a Secret's fields and prints this.
	Token string `redact:""`
}
