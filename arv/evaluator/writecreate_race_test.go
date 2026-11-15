package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// write_file creates or truncates. Through a file grant below an outer
// grant (openLeafIn), a missing leaf is created with O_EXCL, so the open
// never creates a file through a symlink planted after the lstat. When
// another writer created the file in between, the open failed with "file
// exists", though nothing unsafe had happened. It now looks again: the
// file exists, so it is opened without O_CREATE and checked with SameFile.
func TestWriteFileCreateRace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "new")
	beforeStep = func(path string) {
		if path == p {
			if err := os.WriteFile(p, []byte("other writer"), 0o644); err != nil {
				t.Error(err)
			}
		}
	}
	defer func() { beforeStep = nil }()
	if err := writeIn(fsTarget{outer: dir, grant: p, path: p}, "mine"); err != nil {
		t.Fatalf("writeIn: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "mine" {
		t.Fatalf("the file holds %q, want %q", b, "mine")
	}
}

// The same in Arv: tasks holding fs_write(p) for a missing p, under the
// root's fs_write(dir), write it at once.
func TestWriteFileConcurrentCreate(t *testing.T) {
	before := goruntime.NumGoroutine()
	dir := t.TempDir()
	src := fmt.Sprintf(`let p = "%s/new"; let f = fn() { sleep(100); write_file(p, "x") }; `+
		`await for i in %s { spawn f() with [fs_write(p)] }; "ok"`, dir, zeros(100))
	if got, _ := runWith(t, src, grant(t, runtime.FSWrite, dir)); got.Inspect() != `ok` {
		t.Fatalf("got %.300s, want ok", got.Inspect())
	}
	settled(t, before)
}
