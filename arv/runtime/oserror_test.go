package runtime

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// osErrorTree builds, under both dir/g (granted) and dir/out (not granted):
// locked/f inside a mode-000 directory, and a loop1 <-> loop2 symlink loop.
// dir/g/tolocked and dir/g/toloop are symlinks from the grant into out.
func osErrorTree(t *testing.T) (dir string, g CapSet) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir = t.TempDir()
	for _, sub := range []string{"g", "out"} {
		base := filepath.Join(dir, sub)
		if err := os.MkdirAll(filepath.Join(base, "locked"), 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(base, "locked", "f"), []byte("f"), 0o644)
		os.Symlink("loop2", filepath.Join(base, "loop1"))
		os.Symlink("loop1", filepath.Join(base, "loop2"))
		locked := filepath.Join(base, "locked")
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o755) })
	}
	os.Symlink(filepath.Join(dir, "out", "locked"), filepath.Join(dir, "g", "tolocked"))
	os.Symlink(filepath.Join(dir, "out", "loop1"), filepath.Join(dir, "g", "toloop"))
	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, NewCapSet(mustCap(t, FSRead, canon+"/g"))
}

// When resolution fails at a point a grant already covers, the task is
// entitled to know why, so Admit returns the OS error rather than a denial
// that contradicts the grant it holds.
func TestAdmitShowsOSErrorInsideGrant(t *testing.T) {
	dir, g := osErrorTree(t)
	for raw, want := range map[string]error{
		dir + "/g/locked/f":     fs.ErrPermission,
		dir + "/g/loop1":        syscall.ELOOP,
		dir + "/g/loop2/x":      syscall.ELOOP,
		dir + "/g/missing/../x": fs.ErrNotExist,
	} {
		_, err := g.Admit(Capability{Kind: FSRead, Scope: raw})
		var denied *DeniedError
		if errors.As(err, &denied) || !errors.Is(err, want) {
			t.Errorf("Admit(%s) = %v, want the OS error %v", raw, err, want)
		}
	}
}

// When resolution fails outside every grant, even for a request spelled
// inside one, the failure says nothing about the filesystem: a bare denial
// naming the raw request.
func TestAdmitHidesOSErrorOutsideGrants(t *testing.T) {
	dir, g := osErrorTree(t)
	for _, raw := range []string{
		dir + "/out/locked/f",
		dir + "/out/loop1",
		dir + "/out/missing/../x",
		dir + "/g/tolocked/f",
		dir + "/g/toloop",
		dir + "/g/../out/locked/f",
	} {
		_, err := g.Admit(Capability{Kind: FSRead, Scope: raw})
		var denied *DeniedError
		if !errors.As(err, &denied) || denied.Want.Scope != raw {
			t.Errorf("Admit(%s) = %v, want a bare denial naming the raw path", raw, err)
			continue
		}
		for _, leak := range []string{"permission", "symbolic", "no such", "out/loop", "out/locked"} {
			if strings.Contains(strings.TrimPrefix(err.Error(), "capability denied: "+denied.Want.String()), leak) {
				t.Errorf("Admit(%s) = %v, leaks %q", raw, err, leak)
			}
		}
	}
}
