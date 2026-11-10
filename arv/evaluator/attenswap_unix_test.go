//go:build unix

package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// I2 and I3 across spawn: a child attenuated to a grant nested inside its
// parent's grant must be confined as the parent is. A writer inside the
// parent's grant swaps the child's grant directory for a symlink to
// outside every grant between the child's check and its open; the child
// may not reach the target, just as the parent may not (review round 3).
func TestAttenuatedChildSwapCannotEscape(t *testing.T) {
	for _, c := range []struct {
		name     string
		kind     runtime.Kind
		call     string // %q is the path, then the child's grant
		path     string // relative to sub
		childRel string // the child's grant, relative to sub ("" is sub)
	}{
		{"read, dir grant", runtime.FSRead, `await spawn read_file(%q) with [fs_read(%q)]`, "f.txt", ""},
		{"read, file grant", runtime.FSRead, `await spawn read_file(%q) with [fs_read(%q)]`, "f.txt", "f.txt"},
		{"read, deep grant", runtime.FSRead, `await spawn read_file(%q) with [fs_read(%q)]`, "deep/f.txt", "deep"},
		{"write, dir grant", runtime.FSWrite, `await spawn write_file(%q, "pwned") with [fs_write(%q)]`, "new.txt", ""},
		{"write, file grant", runtime.FSWrite, `await spawn write_file(%q, "pwned") with [fs_write(%q)]`, "f.txt", "f.txt"},
		{"write, deep grant", runtime.FSWrite, `await spawn write_file(%q, "pwned") with [fs_write(%q)]`, "deep/new.txt", "deep"},
	} {
		t.Run(c.name, func(t *testing.T) {
			granted, outside := mixedTree(t)
			sub := filepath.Join(granted, "sub")
			swapBeforeOpen(t, sub, outside)
			got, _ := runWith(t, fmt.Sprintf(c.call, filepath.Join(sub, c.path), filepath.Join(sub, c.childRel)),
				grant(t, c.kind, granted))
			fn := "read_file"
			if c.kind == runtime.FSWrite {
				fn = "write_file"
			}
			wantNoLeak(t, got, fn)
			wantUntouched(t, outside, "SECRET")
		})
	}
}

// The same holds two spawns down, with the middle task holding the same
// grant as the grandchild: the open is confined by the outermost grant of
// the whole lineage, here the root's, not only of the task or its parent.
func TestGrandchildSwapCannotEscape(t *testing.T) {
	granted, outside := mixedTree(t)
	sub := filepath.Join(granted, "sub")
	swapBeforeOpen(t, sub, outside)
	prog := fmt.Sprintf(`let inner = fn() { await spawn read_file(%q) with [fs_read(%q)] }; await spawn inner() with [fs_read(%q)]`,
		filepath.Join(sub, "f.txt"), sub, sub)
	got, _ := runWith(t, prog, grant(t, runtime.FSRead, filepath.Dir(granted)))
	wantNoLeak(t, got, "read_file")
}

// Without a swap, an attenuated child reads and writes inside its grant,
// following a relative symlink there as usual.
func TestAttenuatedChildNestedGrantWorks(t *testing.T) {
	granted, _ := mixedTree(t)
	sub := filepath.Join(granted, "sub")
	if err := os.Symlink("deep", filepath.Join(sub, "link")); err != nil {
		t.Fatal(err)
	}
	got, _ := runWith(t, fmt.Sprintf(`await spawn read_file(%q) with [fs_read(%q)]`, filepath.Join(sub, "link", "f.txt"), sub),
		grant(t, runtime.FSRead, granted))
	if got.Inspect() != "inside" {
		t.Fatalf("child read = %s, want inside", got.Inspect())
	}
	for _, g := range []string{sub, filepath.Join(sub, "f.txt")} {
		got, _ = runWith(t, fmt.Sprintf(`await spawn write_file(%q, "new") with [fs_write(%q)]`, filepath.Join(sub, "f.txt"), g),
			grant(t, runtime.FSWrite, granted))
		if data, _ := os.ReadFile(filepath.Join(sub, "f.txt")); string(data) != "new" {
			t.Fatalf("child write (grant %s) = %s, file %q", g, got.Inspect(), data)
		}
	}
}
