package runtime

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// FuzzCanonical: Canonical never panics, and on success its result (when
// it stays inside the fuzz tree, so the host's own files are not part of
// the property) is
//
//   - absolute and clean, with no symlink in any existing component;
//   - a fixpoint: Canonical(result) == result;
//   - the file the kernel reaches: if os.Stat(path) succeeds, the result
//     exists and is the same file.
//
// Every failure is a *CanonicalError naming the path as given.
func FuzzCanonical(f *testing.F) {
	root := fuzzTree(f, f.TempDir())
	for _, s := range pathSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		path := fuzzPath(root, s)
		got, err := Canonical(path)
		if err != nil {
			var ce *CanonicalError
			if !errors.As(err, &ce) || ce.Path != path {
				t.Fatalf("Canonical(%q) failed with %T %v, want a *CanonicalError for the path", path, err, err)
			}
			return
		}
		if !under(got, root) {
			return
		}
		if !filepath.IsAbs(got) || got != filepath.Clean(got) {
			t.Fatalf("Canonical(%q) = %q: not absolute and clean", path, got)
		}
		for p := got; under(p, root) && p != root; p = filepath.Dir(p) {
			if fi, err := os.Lstat(p); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
				t.Fatalf("Canonical(%q) = %q: component %q is a symlink", path, got, p)
			}
		}
		again, err := Canonical(got)
		if err != nil || again != got {
			t.Fatalf("Canonical(%q) = %q, but Canonical of that = %q, %v", path, got, again, err)
		}
		want, err := os.Stat(path)
		if err != nil {
			return // the kernel may refuse what Canonical accepts ("file/.", more hops)
		}
		have, err := os.Stat(got)
		if err != nil || !os.SameFile(want, have) {
			t.Fatalf("Canonical(%q) = %q, which is not the file the kernel reaches (%v)", path, got, err)
		}
	})
}
