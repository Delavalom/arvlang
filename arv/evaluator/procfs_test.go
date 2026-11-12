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

// FreeBSD's linprocfs and procfs (and DragonFly's procfs) expose
// /proc/<pid>/environ too, so they are refused by their f_fstypename
// (review round 2). The name is decoded from statfs's NUL-padded array.
func TestProcFSNames(t *testing.T) {
	for name, want := range map[string]bool{"procfs": true, "linprocfs": true, "apfs": false, "ufs": false, "zfs": false, "devfs": false, "": false} {
		if got := isProcFSName(name); got != want {
			t.Errorf("isProcFSName(%q) = %v, want %v", name, got, want)
		}
	}
	var buf [16]int8
	for i, c := range "linprocfs" {
		buf[i] = int8(c)
	}
	if got := fsTypeString(buf[:]); got != "linprocfs" {
		t.Fatalf("fsTypeString = %q", got)
	}
	full := [4]int8{'a', 'b', 'c', 'd'} // no NUL: the whole array
	if got := fsTypeString(full[:]); got != "abcd" {
		t.Fatalf("fsTypeString(no NUL) = %q", got)
	}
}
