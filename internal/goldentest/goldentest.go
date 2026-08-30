package goldentest

// Check holds a package's generated files to the copies recorded for this test,
// and then compiles the package they belong to.
//
// Both halves are needed and neither is sufficient. A recorded copy says the
// output has not changed, which is what turns a layer quietly emitting
// something different into a diff somebody reads; compiling says the output is
// Go that works, which a recorded copy of something broken never would. A suite
// with only the first happily records nonsense; one with only the second lets
// every regression through that still compiles, which is most of them.
func Check(t T, pkg Package) {
	t.Helper()

	// Named before anything is written. Two files under one name would each be
	// recorded over the other's golden, and the run that noticed would be the
	// next one, blaming the output rather than the name.
	if err := distinct(pkg.Files); err != nil {
		t.Errorf("package %s: %v", pkg.Path, err)
		return
	}

	recorded := 0
	for _, source := range pkg.Files {
		if !source.Generated {
			continue
		}
		recorded++
		Compare(t, source.Name, source.Content)
	}

	// A package with no generated file in it compiles, records nothing, and
	// passes — so one forgotten field turns the golden half off for a layer
	// permanently, with nothing to notice. It is always a mistake in the test:
	// a package with no output in it is not a thing this harness is for.
	if recorded == 0 {
		t.Errorf("package %s has no generated file in it, so nothing was held to a recorded copy", pkg.Path)
		return
	}

	// Compiled even when the comparison failed. The two answer different
	// questions, and being told only that output changed — when what it changed
	// into does not build — is being told the less useful half.
	if err := Compiles(pkg); err != nil {
		t.Errorf("%v", err)
	}
}
