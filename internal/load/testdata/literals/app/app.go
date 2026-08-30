package app

// Switcher keeps its body, because a function literal is part of an expression
// the type-checker still has to evaluate. The guard it declares is never used,
// which is a real error that stripping did not cause.
var Switcher = func(x any) {
	switch v := x.(type) {
	case int:
	case string:
	}
}
