package diag_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// update rewrites the golden files instead of comparing against them. The
// rendered format is what users read, so a change to it should be a reviewable
// diff rather than a string edited in two places at once.
var update = flag.Bool("update", false, "rewrite golden files")

// checkGolden compares got against testdata/<name>.golden.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")

	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("create testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run go test ./internal/diag -update to create it)", path, err)
	}
	if got != string(want) {
		t.Errorf("rendered output does not match %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
