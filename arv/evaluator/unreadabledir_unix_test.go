//go:build unix

package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// These tests pin a known limit of os.Root (16-implementation-notes.md,
// Phase 8.3, Known limits): os.Root opens every directory it walks, the
// root included, read-only (openat O_RDONLY|O_DIRECTORY), so a directory
// the process may search (x) but not read (r) fails the open with EACCES.
// Before Phase 8.3 a plain open with O_NOFOLLOW only needed search
// permission. The failure is closed: it denies, it never widens. If a later
// walker opens directories for search only (O_SEARCH, O_PATH), these tests
// should flip to expect success.

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permission bits")
	}
}

// chmodTemp sets dir's mode and restores 0755 at cleanup, so TempDir's
// RemoveAll can delete the tree.
func chmodTemp(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
}

func wantPermissionDenied(t *testing.T, what string, got value.Object) {
	t.Helper()
	e, ok := got.(*value.Error)
	if !ok || !strings.Contains(e.Message, "permission denied") {
		t.Fatalf("%s: got %s, want a permission-denied error (os.Root limit)", what, got.Inspect())
	}
}

// TestSearchOnlyDirInsideGrantIsRefused: fs_read=/g and fs_write=/g, with
// /g/priv mode 0100 (search only, like a 0711 home directory seen by
// another user). A file below it was readable before os.Root; now the
// walk through priv fails. A grant on the readable directory below priv
// still works.
func TestSearchOnlyDirInsideGrantIsRefused(t *testing.T) {
	skipIfRoot(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g := filepath.Join(root, "g")
	priv := filepath.Join(g, "priv")
	pub := filepath.Join(priv, "pub")
	mustMkdir(t, pub)
	f := filepath.Join(pub, "f.txt")
	if err := os.WriteFile(f, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	chmodTemp(t, priv, 0o100)

	got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, f), grant(t, runtime.FSRead, g))
	wantPermissionDenied(t, "read below a search-only dir", got)

	n := filepath.Join(pub, "n.txt")
	got, _ = runWith(t, fmt.Sprintf(`write_file(%q, "w")`, n), grant(t, runtime.FSWrite, g))
	wantPermissionDenied(t, "write below a search-only dir", got)
	if _, err := os.Lstat(n); !os.IsNotExist(err) {
		t.Fatalf("refused write created %s (err %v)", n, err)
	}

	got, _ = runWith(t, fmt.Sprintf(`read_file(%q)`, f), grant(t, runtime.FSRead, pub))
	if s, ok := got.(*value.String); !ok || s.Value != "ok" {
		t.Fatalf("grant on the readable dir below: got %s, want \"ok\"", got.Inspect())
	}
}

// TestWriteOnlyGrantDirIsRefused: a drop-box grant directory, mode 0300
// (write and search, no read), can no longer be written into, because
// os.OpenRoot opens the grant directory itself read-only.
func TestWriteOnlyGrantDirIsRefused(t *testing.T) {
	skipIfRoot(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g := filepath.Join(root, "drop")
	mustMkdir(t, g)
	chmodTemp(t, g, 0o300)

	n := filepath.Join(g, "n.txt")
	got, _ := runWith(t, fmt.Sprintf(`write_file(%q, "w")`, n), grant(t, runtime.FSWrite, g))
	wantPermissionDenied(t, "write into a write-only grant dir", got)
}
