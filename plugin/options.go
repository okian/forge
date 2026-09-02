package plugin

import (
	"github.com/okian/forge/internal/layer"
	"github.com/okian/forge/internal/model"
)

// OptionDef declares one option a layer accepts: its key, what shape of value
// it takes, where it may be written, and what it is for.
//
// Declared rather than parsed by the layer, so that one validator checks every
// layer's options rather than each layer checking its own — and so that an
// option nobody declared is reported as a misspelling with the nearest name
// beside it rather than silently ignored.
type OptionDef = layer.OptionDef

// Options are the options written for one layer, already checked against its
// schema.
//
// Already checked, so a layer reads them without checking again: a value that
// names a field has been resolved against the subject, an enumerated value is
// one of the ones declared, and anything wrong was reported before the layer
// was asked to generate.
type Options = model.Options

// ValueKind says what shape an option's value takes.
type ValueKind = layer.ValueKind

// The shapes an option's value may take.
//
// ValueNone is a bare key, written without an equals sign. ValueField and
// ValueFields name one and several of the subject's fields, and are resolved
// against it before a layer sees them. ValueEnum is one of a list the option
// declares. The rest are what they say.
const (
	ValueNone   = layer.ValueNone
	ValueBool   = layer.ValueBool
	ValueInt    = layer.ValueInt
	ValueString = layer.ValueString
	ValueEnum   = layer.ValueEnum
	ValueField  = layer.ValueField
	ValueFields = layer.ValueFields
)

// Scope says where an option may be written.
type Scope = layer.Scope

// The places an option may be written.
//
// ScopeDeclaration is the directive above the declaration, which is where an
// option about the whole thing goes. ScopeField is a tag on one of the
// subject's fields, which is where an option about that field goes. An option
// declared for one and written in the other is reported rather than read.
const (
	ScopeDeclaration = layer.ScopeDeclaration
	ScopeField       = layer.ScopeField
)
