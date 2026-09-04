//go:build forgespec

package model

import "strings"

//forge:map hint
func renamedFromUser(src *User, dst *Renamed) {
	dst.Moniker = src.Email
}

//forge:map hint
func convertedFromUser(src *User, dst *Converted) {
	dst.Age = float64(src.Age)
}

// personFromUser overrides a member the ladder would settle on its own.
//
//forge:map hint
func personFromUser(src *User, dst *Person) {
	dst.Email = src.Email + "!"
}

// declares says more than a hint may: a local is not an assignment.
//
//forge:map hint
func declares(src *User, dst *Renamed) {
	x := src.Email
	dst.Moniker = x
}

// branches says more than a hint may: a hint has no conditions.
//
//forge:map hint
func branches(src *User, dst *Renamed) {
	if src.Age > 0 {
		dst.Moniker = src.Email
	}
}

// writesBack assigns into the source, which no mapping does.
//
//forge:map hint
func writesBack(src *User, dst *Renamed) {
	src.Email = "x"
	dst.Moniker = "y"
}

// promotes assigns a member the target does not declare: Core is Base's.
//
//forge:map hint
func promotes(src *User, dst *Based) {
	dst.Core = src.ID
	dst.Own = src.Age
}

// twice assigns one member two ways, and a mapping has no last word.
//
//forge:map hint
func twice(src *User, dst *Renamed) {
	dst.Moniker = src.Email
	dst.Moniker = "again"
}

// imported reaches through an import, which the constructor cannot carry: the
// hint's file is compiled and never linked, and its imports stay with it.
//
//forge:map hint
func imported(src *User, dst *Renamed) {
	dst.Moniker = strings.ToUpper(src.Email)
}

// stickerFromUser assigns the member Sticker's tag already pins, which is two
// explicit answers for one member.
//
//forge:map hint
func stickerFromUser(src *User, dst *Sticker) {
	dst.Email = src.Email
}

// echoes reads the value under construction, which the constructor allows —
// the literal is already built when the hint runs — and a fused mapping must
// refuse: there is no target value while the document is written.
//
//forge:map hint
func echoes(src *User, dst *Renamed) {
	dst.ID = src.ID
	dst.Moniker = dst.Moniker + src.Email
}
