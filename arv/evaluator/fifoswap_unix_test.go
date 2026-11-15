//go:build unix

package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// A directory on the way from an outer grant to a nested grant of the
// other kind, swapped for a FIFO after the check, must be refused at once
// by step's lstat ("path changed during the open"). Opening it as a root
// instead would block in open(2) until a writer appears (on darwin), and
// no context can interrupt that syscall, so the task would hang past
// cancellation (mutation check, Phase 8.3 round 3).
//
// If the open blocks, the test opens the FIFO itself after a grace period
// so the run returns; the assertion is on the error, not on timing, so a
// slow machine cannot make the test flaky.
func TestNestedGrantStepRefusesSwappedFIFO(t *testing.T) {
	for _, c := range []struct {
		name         string
		outer, inner runtime.Kind
		call         string
	}{
		{"read under write", runtime.FSWrite, runtime.FSRead, `read_file(%q)`},
		{"write under read", runtime.FSRead, runtime.FSWrite, `write_file(%q, "pwned")`},
	} {
		t.Run(c.name, func(t *testing.T) {
			granted, _ := mixedTree(t)
			sub := filepath.Join(granted, "sub")
			swapped := false
			beforeOpen = func(string) {
				beforeOpen = nil
				if err := os.Rename(sub, sub+".orig"); err != nil {
					t.Error(err)
				}
				if err := syscall.Mkfifo(sub, 0o644); err != nil {
					t.Error(err)
				}
				swapped = true
			}
			t.Cleanup(func() { beforeOpen = nil })

			fn := "read_file"
			if c.inner == runtime.FSWrite {
				fn = "write_file"
			}
			src := fmt.Sprintf(c.call, filepath.Join(sub, "f.txt"))
			if c.inner == runtime.FSWrite {
				src = fmt.Sprintf(c.call, filepath.Join(sub, "new.txt"))
			}
			caps := []runtime.Capability{grant(t, c.outer, granted), grant(t, c.inner, sub)}
			done := make(chan value.Object, 1)
			go func() {
				got, _ := runWith(t, src, caps...)
				done <- got
			}()

			var got value.Object
			select {
			case got = <-done:
			case <-time.After(5 * time.Second):
				// Unblock a hung open: O_RDWR on a FIFO never blocks and
				// counts as both a reader and a writer.
				t.Logf("%s still running after 5s; opening the FIFO to unblock it", fn)
				f, err := os.OpenFile(sub, os.O_RDWR|syscall.O_NONBLOCK, 0)
				if err == nil {
					defer f.Close()
				}
				select {
				case got = <-done:
				case <-time.After(2 * time.Minute):
					t.Fatalf("%s did not return even after the FIFO was opened", fn)
				}
			}
			if !swapped {
				t.Fatal("the swap never ran")
			}
			wantErrorHas(t, got, fn+": ")
			wantErrorHas(t, got, errChanged.Error())
		})
	}
}
