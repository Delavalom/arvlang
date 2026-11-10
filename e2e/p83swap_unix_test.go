//go:build unix

package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// flipper swaps dir between a real directory and a symlink to elsewhere,
// over and over, until stop (or the test's cleanup) is called. stop returns
// how many swaps it made.
// Like an attacker who can write inside the grant, it renames the real
// directory aside, plants the link, then puts the directory back.
func flipper(t *testing.T, dir, elsewhere string) (stop func() int) {
	t.Helper()
	aside := dir + ".real"
	var done atomic.Bool
	var n int
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !done.Load() {
			if os.Rename(dir, aside) != nil || os.Symlink(elsewhere, dir) != nil {
				t.Error("flipper: cannot plant the link")
				return
			}
			runtime.Gosched()
			if os.Remove(dir) != nil || os.Rename(aside, dir) != nil {
				t.Error("flipper: cannot restore the directory")
				return
			}
			n++
			time.Sleep(time.Millisecond) // let some reads find the real directory
		}
	}()
	stop = func() int { done.Store(true); wg.Wait(); return n }
	t.Cleanup(func() { stop() })
	return stop
}

// The check/use race through the real binary: while a writer inside the
// grant keeps swapping g/d for a symlink to out/d, a script reads (or
// writes) g/d/f in a loop. Every read either sees the file inside the
// grant or fails (the check sees the link, or os.Root refuses it); none
// may ever see out/d/f, and no write may land in out/d. The unit tests
// (arv/evaluator/*swap_unix_test.go) pin the window deterministically with
// hooks; this runs the shipped binary against a real concurrent writer.
func TestP83SwapRaceCannotEscape(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"read", "write"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, "g", "d", "f"), "inside")
			mustWrite(t, filepath.Join(root, "out", "d", "f"), "SECRET")
			kind, script, line := "fs_read=", "p83_read_loop.arv", "inside\n"
			if mode == "write" {
				kind, script, line = "fs_write=", "p83_write_loop.arv", "ok\n"
			}
			stop := flipper(t, filepath.Join(root, "g", "d"), filepath.Join(root, "out", "d"))
			ok, failed := 0, 0
			deadline := time.Now().Add(8 * time.Second)
			for runs := 0; runs < 25 && time.Now().Before(deadline); runs++ {
				res := run(t, spec{
					script: script, dir: root, allow: []string{kind + filepath.Join(root, "g")},
					vars: map[string]string{"PATH": filepath.Join(root, "g", "d", "f"), "N": "100"},
				})
				if strings.Contains(res.stdout, "SECRET") || res.code > 1 || containsAny(res.stderr, goCrash) {
					t.Fatalf("a read escaped the grant, or the run crashed:\n%s", res)
				}
				ok += strings.Count(res.stdout, line)
				if res.code != 0 {
					failed++
				}
			}
			swaps := stop()
			assertFile(t, filepath.Join(root, "out", "d", "f"), "SECRET")
			t.Logf("%d swaps; %d %ss inside the grant; %d runs stopped by a refused open", swaps, ok, mode, failed)
		})
	}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
