//go:build freebsd || dragonfly || darwin

package evaluator

import (
	"os"
	"testing"
)

// The fstatfs path runs here: a temp file's file system has a name that
// is not procfs (apfs on macOS, ufs or zfs on FreeBSD).
func TestFSTypeNameOfATempFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	name, err := fsTypeName(f)
	if err != nil || name == "" || isProcFSName(name) {
		t.Fatalf("fsTypeName = %q, %v", name, err)
	}
	t.Logf("temp file system: %s", name)
}
