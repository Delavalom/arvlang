//go:build unix

package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// A file grant nested below a grant of the other kind is opened from the
// outer grant (confiningRoot) by openLeafIn. A leaf swapped for a symlink
// after the check must be refused at its lstat, as O_NOFOLLOW would refuse
// it, without opening the symlink's target. That target lies outside the
// file grant, so opening it is already a use of authority the task lacks,
// even if the open is refused later: write_file opening a FIFO the task
// may only read is a write-open of it, with its side effects (a reader
// sees a writer come and go; with no reader the open fails with ENXIO).
//
// The os.SameFile check after the open would refuse the result anyway, so
// what proves the target was not opened is the error: "path changed"
// (errChanged) from the lstat, not the error the open of the target gives
// (ENXIO, ENOENT, "path escapes"). Mutation check, Phase 8.3 round 4:
// openLeafIn without its symlink case passed every other test.
func TestNestedFileGrantLeafSymlinkIsNotFollowed(t *testing.T) {
	for _, c := range []struct {
		name         string
		outer, inner runtime.Kind
		link         string // symlink target, relative to the leaf's directory
		makeFIFO     bool   // create the link's target as a FIFO with no reader
	}{
		{"write to FIFO beside the leaf", runtime.FSRead, runtime.FSWrite, "fifo", true},
		{"write to missing file beside the leaf", runtime.FSRead, runtime.FSWrite, "missing", false},
		{"write to outside", runtime.FSRead, runtime.FSWrite, "../../out/f.txt", false},
		{"read missing file beside the leaf", runtime.FSWrite, runtime.FSRead, "missing", false},
		{"read outside", runtime.FSWrite, runtime.FSRead, "../../out/f.txt", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			granted, outside := mixedTree(t)
			leaf := filepath.Join(granted, "sub", "f.txt")
			if c.makeFIFO {
				if err := syscall.Mkfifo(filepath.Join(granted, "sub", "fifo"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			swapped := false
			beforeOpen = func(string) {
				beforeOpen = nil
				if err := os.Rename(leaf, leaf+".orig"); err != nil {
					t.Error(err)
				}
				if err := os.Symlink(c.link, leaf); err != nil {
					t.Error(err)
				}
				swapped = true
			}
			t.Cleanup(func() { beforeOpen = nil })

			fn, src := "read_file", fmt.Sprintf(`read_file(%q)`, leaf)
			if c.inner == runtime.FSWrite {
				fn, src = "write_file", fmt.Sprintf(`write_file(%q, "pwned")`, leaf)
			}
			got, _ := runWith(t, src, grant(t, c.outer, granted), grant(t, c.inner, leaf))
			if !swapped {
				t.Fatal("the swap never ran")
			}
			wantErrorHas(t, got, fn+": ")
			wantErrorHas(t, got, errChanged.Error())
			if _, err := os.Lstat(filepath.Join(granted, "sub", "missing")); err == nil {
				t.Errorf("%s created the symlink's missing target", fn)
			}
			wantUntouched(t, outside, "SECRET")
			wantUntouched(t, filepath.Join(granted, "other"), "other")
		})
	}
}
