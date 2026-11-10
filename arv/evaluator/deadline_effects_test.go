package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// No builtin runs in a task whose scope's deadline has passed. The context
// is checked only every 256 nodes, so without a check at each builtin call
// a write that came after slow pure work (here a host builtin that ignores
// cancellation) still happened, and only then was the timeout reported.
// The deadline itself is checked too, not only the context, because the
// timer that cancels the context can fire late on a loaded machine.
func TestNoEffectAfterDeadline(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct{ name, src string }{
		{"write in body", `scope (timeout_ms: 20) { slow(); write_file(%q, "late") }`},
		{"write in spawned task", `let w = fn(p) { slow(); write_file(p, "late") };
			scope (timeout_ms: 20) { spawn w(%q) with [fs_write("` + dir + `")] }`},
		{"write in nested scope", `scope (timeout_ms: 20) { scope { slow(); write_file(%q, "late") } }`},
		{"puts in body", `scope (timeout_ms: 20) { slow(); puts("late"); %q }`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := goruntime.NumGoroutine()
			path := filepath.Join(dir, "out.txt")
			os.Remove(path)
			got, out := runEnv(t, fmt.Sprintf(tt.src, path), slow(200*time.Millisecond), grant(t, runtime.FSWrite, dir))
			wantErrorHas(t, got, "budget exceeded: timeout (limit 20 ms)")
			if _, err := os.Stat(path); err == nil {
				t.Fatalf("the file was written after the deadline")
			}
			if out != "" {
				t.Fatalf("puts printed %q after the deadline", out)
			}
			settled(t, before)
		})
	}
}
