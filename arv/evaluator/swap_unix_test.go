//go:build unix

package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// swapTree builds root/g/sub/f.txt ("inside"), root/g/other/f.txt ("other")
// and root/out/f.txt ("SECRET"). The grant is root/g.
func swapTree(t *testing.T) (granted, outside string) {
	t.Helper()
	root := t.TempDir()
	granted, outside = filepath.Join(root, "g"), filepath.Join(root, "out")
	for path, text := range map[string]string{
		filepath.Join(granted, "sub", "f.txt"):   "inside",
		filepath.Join(granted, "other", "f.txt"): "other",
		filepath.Join(outside, "f.txt"):          "SECRET",
	} {
		mustMkdir(t, filepath.Dir(path))
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return granted, outside
}

// swapBeforeOpen replaces dir with a symlink to target in the window
// between the capability check and the open, as a racing process could.
func swapBeforeOpen(t *testing.T, dir, target string) {
	t.Helper()
	beforeOpen = func(string) {
		if err := os.Rename(dir, dir+".orig"); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(target, dir); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { beforeOpen = nil })
}

// I3: a directory inside the grant swapped for a symlink to outside it
// after the check must not redirect read_file there.
func TestReadFileDirSwapCannotEscape(t *testing.T) {
	granted, outside := swapTree(t)
	swapBeforeOpen(t, filepath.Join(granted, "sub"), outside)
	got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, filepath.Join(granted, "sub", "f.txt")),
		grant(t, runtime.FSRead, granted))
	if got.Inspect() == "SECRET" {
		t.Fatal("read_file followed a swapped directory out of its grant")
	}
	wantErrorHas(t, got, "path escapes")
}

// I3: the same race must not let write_file create a file outside the grant.
func TestWriteFileDirSwapCannotEscape(t *testing.T) {
	granted, outside := swapTree(t)
	swapBeforeOpen(t, filepath.Join(granted, "sub"), outside)
	got, _ := runWith(t, fmt.Sprintf(`write_file(%q, "pwned")`, filepath.Join(granted, "sub", "new.txt")),
		grant(t, runtime.FSWrite, granted))
	if _, err := os.Lstat(filepath.Join(outside, "new.txt")); err == nil {
		t.Fatal("write_file followed a swapped directory out of its grant")
	}
	wantErrorHas(t, got, "path escapes")
}

// A swap to a relative symlink that stays inside the grant reaches a file
// the task may read anyway, so os.Root follows it. (os.Root treats every
// absolute symlink as escaping, even one that points inside the root.)
func TestReadFileDirSwapInsideGrantIsFollowed(t *testing.T) {
	granted, _ := swapTree(t)
	swapBeforeOpen(t, filepath.Join(granted, "sub"), "other")
	got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, filepath.Join(granted, "sub", "f.txt")),
		grant(t, runtime.FSRead, granted))
	if got.Inspect() != "other" {
		t.Fatalf("got %s, want the swapped-in file inside the grant", got.Inspect())
	}
}

// A leaf swapped for a symlink to outside the grant is refused too.
func TestReadFileLeafSwapCannotEscape(t *testing.T) {
	granted, outside := swapTree(t)
	leaf := filepath.Join(granted, "sub", "f.txt")
	swapBeforeOpen(t, leaf, filepath.Join(outside, "f.txt"))
	got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, leaf), grant(t, runtime.FSRead, granted))
	if got.Inspect() == "SECRET" {
		t.Fatal("read_file followed a swapped leaf out of its grant")
	}
	wantErrorHas(t, got, "path escapes")
}

// A file grant has no directory to confine; the leaf keeps O_NOFOLLOW.
func TestReadFileFileGrantLeafSwapIsRefused(t *testing.T) {
	granted, outside := swapTree(t)
	leaf := filepath.Join(granted, "sub", "f.txt")
	swapBeforeOpen(t, leaf, filepath.Join(outside, "f.txt"))
	got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, leaf), grant(t, runtime.FSRead, leaf))
	if got.Inspect() == "SECRET" {
		t.Fatal("read_file followed a swapped leaf out of its file grant")
	}
	wantErrorHas(t, got, "read_file: ")
}
