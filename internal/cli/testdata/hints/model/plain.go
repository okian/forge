package model

// A hint in an ordinary file is refused: the spec file is where a function
// that is compiled and never linked belongs.
//
//forge:map hint
func personFromUserInline(src *User, dst *Person) {
	dst.Name = src.Name
}
