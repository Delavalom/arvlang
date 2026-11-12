package evaluator

import (
	"os"
	"path/filepath"
	"testing"
)

// A regular file is not on procfs, on every platform.
func TestOnProcFSRegularFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if onProcFS(f) {
		t.Fatal("a temp file is reported on procfs")
	}
}
