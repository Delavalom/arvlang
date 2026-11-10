//go:build unix

package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// I3 with nested grants: when a task holds a grant and a narrower one
// inside it, the narrower grant's own directory sits inside the outer
// grant, where a writer the task shares the tree with can swap it for a
// symlink. The open must stay confined to the outer grant, so the swap
// cannot lead outside every grant the task holds.
func TestNestedGrantDirSwapCannotEscape(t *testing.T) {
	for _, c := range []struct {
		name  string
		kind  runtime.Kind
		inner string // relative to the outer grant
		call  string // %q is the path
		path  string // relative to the outer grant
	}{
		{"read dir+dir", runtime.FSRead, "sub", `read_file(%q)`, "sub/f.txt"},
		{"read dir+file", runtime.FSRead, "sub/f.txt", `read_file(%q)`, "sub/f.txt"},
		{"write dir+dir", runtime.FSWrite, "sub", `write_file(%q, "pwned")`, "sub/new.txt"},
		{"write dir+file", runtime.FSWrite, "sub/f.txt", `write_file(%q, "pwned")`, "sub/f.txt"},
	} {
		t.Run(c.name, func(t *testing.T) {
			granted, outside := swapTree(t)
			swapBeforeOpen(t, filepath.Join(granted, "sub"), outside)
			got, _ := runWith(t, fmt.Sprintf(c.call, filepath.Join(granted, c.path)),
				grant(t, c.kind, granted), grant(t, c.kind, filepath.Join(granted, c.inner)))
			if got.Inspect() == "SECRET" {
				t.Fatal("read_file followed a swapped directory out of every grant")
			}
			if _, err := os.Lstat(filepath.Join(outside, "new.txt")); err == nil {
				t.Fatal("write_file created a file outside every grant")
			}
			if data, _ := os.ReadFile(filepath.Join(outside, "f.txt")); string(data) != "SECRET" {
				t.Fatalf("outside file = %q, want untouched", data)
			}
			wantErrorHas(t, got, "path escapes")
		})
	}
}

// A leaf swapped for a symlink to outside the grant must not let
// write_file truncate or overwrite the target, under a directory grant
// (os.Root) or a file grant (O_NOFOLLOW).
func TestWriteFileLeafSwapCannotEscape(t *testing.T) {
	for _, c := range []struct {
		name, grantRel, wantErr string
	}{
		{"dir grant", "", "path escapes"},
		{"file grant", "sub/f.txt", "write_file: "},
	} {
		t.Run(c.name, func(t *testing.T) {
			granted, outside := swapTree(t)
			leaf := filepath.Join(granted, "sub", "f.txt")
			swapBeforeOpen(t, leaf, filepath.Join(outside, "f.txt"))
			got, _ := runWith(t, fmt.Sprintf(`write_file(%q, "pwned")`, leaf),
				grant(t, runtime.FSWrite, filepath.Join(granted, c.grantRel)))
			if data, _ := os.ReadFile(filepath.Join(outside, "f.txt")); string(data) != "SECRET" {
				t.Fatalf("outside file = %q, want untouched", data)
			}
			wantErrorHas(t, got, c.wantErr)
		})
	}
}
