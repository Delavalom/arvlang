package runtime

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// dotdotTree builds dir/g/sub, dir/g/x, dir/hidden/realdir, dir/hidden/file
// and dir/hidden/islink -> dir/elsewhere/deep/er (outside g).
func dotdotTree(t *testing.T) (dir, canon string) {
	t.Helper()
	dir = t.TempDir()
	for _, d := range []string{"g/sub", "hidden/realdir", "elsewhere/deep/er"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "g", "x"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "hidden", "file"), []byte("f"), 0o644)
	if err := os.Symlink(filepath.Join(dir, "elsewhere", "deep", "er"), filepath.Join(dir, "hidden", "islink")); err != nil {
		t.Fatal(err)
	}
	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, canon
}

// ".." after a missing component, or after a non-directory, fails as it
// does in the kernel (ENOENT, ENOTDIR) instead of climbing back lexically.
// A missing tail with no ".." after it still resolves (a file to create).
func TestCanonicalDotDotAfterMissing(t *testing.T) {
	dir, canon := dotdotTree(t)
	for _, p := range []string{dir + "/missing/../g/x", dir + "/hidden/file/../realdir", dir + "/missing/deeper/../.."} {
		if got, err := Canonical(p); err == nil {
			t.Errorf("Canonical(%s) = %s, want an error", p, got)
		}
	}
	if _, err := Canonical(dir + "/missing/../x"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing/..: err = %v, want ErrNotExist", err)
	}
	for p, want := range map[string]string{
		dir + "/hidden/realdir/../file": canon + "/hidden/file",
		dir + "/g/missing":              canon + "/g/missing",
		dir + "/g/missing/new":          canon + "/g/missing/new",
	} {
		if got, err := Canonical(p); err != nil || got != want {
			t.Errorf("Canonical(%s) = %s, %v; want %s", p, got, err, want)
		}
	}
}

// The oracle the reviewer found: with only fs_read(g), climbing out of a
// missing component was allowed while climbing out of a symlink was denied,
// so allow/deny revealed which names outside g are symlinks. Both are now
// denied alike.
func TestAdmitDotDotOracle(t *testing.T) {
	dir, canon := dotdotTree(t)
	g := NewCapSet(mustCap(t, FSRead, canon+"/g"))
	for _, raw := range []string{
		dir + "/hidden/missing/../../g/sub/../x",
		dir + "/hidden/islink/../../g/sub/../x",
		dir + "/nothere/anything/../../g/sub/../x",
	} {
		_, err := g.Admit(Capability{Kind: FSRead, Scope: raw})
		var denied *DeniedError
		if !errors.As(err, &denied) || denied.Want.Scope != raw {
			t.Errorf("Admit(%s) = %v, want a bare denial naming the raw path", raw, err)
		}
	}
	// Through a real directory the path is the kernel's, and allowed.
	if _, err := g.Admit(Capability{Kind: FSRead, Scope: dir + "/hidden/realdir/../../g/sub/../x"}); err != nil {
		t.Errorf("through a real directory: %v", err)
	}
	// Inside the grant, missing/.. is not the file next to missing.
	if c, err := g.Admit(Capability{Kind: FSRead, Scope: dir + "/g/missing/../x"}); err == nil {
		t.Errorf("g/missing/../x admitted as %s; the kernel says ENOENT", c)
	}
}
