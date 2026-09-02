package validate

import (
	"github.com/okian/forge/internal/layers/failures"
	"github.com/okian/forge/plugin"
)

// reporting returns the types a check reports through, and the folding it needs
// to put a nested failure under the path that reaches it.
//
// Two contributions rather than one, under the keys the package that owns them
// gives: the types are shared with whatever else reports a failure, and the
// folding is this layer's alone. A package that has a builder and no check
// holds the first and not the second.
func reporting() (map[string]plugin.Unit, error) {
	out := make(map[string]plugin.Unit, 2)

	held, err := failures.Unit()
	if err != nil {
		return nil, err
	}
	out[failures.Key] = held

	folding, err := failures.Nested()
	if err != nil {
		return nil, err
	}
	out[failures.NestedKey] = folding

	return out, nil
}
