// Package catalog holds a declaration in a second package, so that ordering
// across packages is exercised and not just ordering within one.
package catalog

import "declsfixture/markers"

// Item is the subject.
type Item struct {
	SKU string
}

// Items is an inline declaration in a package that sorts before model.
//
//forge:collection index=SKU
type Items markers.Collection[Item]
