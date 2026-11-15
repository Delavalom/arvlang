package evaluator

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// runDiscard runs input as the root task, printing to io.Discard, so what
// puts allocates is its own, not the test's buffer's.
func runDiscard(t *testing.T, input string) value.Object {
	t.Helper()
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	return New(root).Run(parse(t, input), value.NewEnvironment())
}

// puts of a string printed it with fmt.Fprintln, which copies it into
// fmt's buffer (a new one past 64 KiB, which fmt does not pool), uncharged:
// 1000 puts of a 1 MB string allocated 961 MiB under alloc_mb: 1, and the
// scope succeeded. It now writes the string itself.
func TestPutsDoesNotCopyAString(t *testing.T) {
	s := strings.Repeat("A", 1000000)
	src := `let s = "` + s + `"; scope (alloc_mb: 1) { for i in ` + zeros(1000) + ` { puts(s) }; "ok" }`
	var got value.Object
	used := heapDuring(func() { got = runDiscard(t, src) })
	if got.Inspect() != "ok" {
		t.Fatalf("got %.200s, want ok", got.Inspect())
	}
	// The program's own literal is about 1 MB, parsed twice over.
	if used > 16<<20 {
		t.Errorf("1000 puts of a 1 MB string allocated %d MiB", used>>20)
	}
}

// The text puts builds for an array is charged as it is built, and was
// then copied again, uncharged, by Fprintln.
func TestPutsDoesNotCopyItsText(t *testing.T) {
	s := strings.Repeat("A", 1000000)
	src := `let a = ["` + s + `"]; scope (alloc_mb: 64) { for i in ` + zeros(20) + ` { puts(a) }; "ok" }`
	var got value.Object
	used := heapDuring(func() { got = runDiscard(t, src) })
	if got.Inspect() != "ok" {
		t.Fatalf("got %.200s, want ok", got.Inspect())
	}
	// Building the text allocates about its size; Fprintln doubled that.
	if used > 28<<20 {
		t.Errorf("20 puts of a 1 MB array allocated %d MiB", used>>20)
	}
}

// Lines from tasks printing at once still never interleave.
func TestPutsLinesStayWhole(t *testing.T) {
	line := strings.Repeat("x", 100000)
	src := `let s = "` + line + `"; let f = fn() { sleep(50); puts(s) }; await for i in ` + zeros(50) + ` { spawn f() }; 1`
	got, out := runWith(t, src)
	if got.Inspect() != "1" {
		t.Fatalf("got %.200s", got.Inspect())
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 50 {
		t.Fatalf("got %d lines, want 50", len(lines))
	}
	for i, l := range lines {
		if l != line {
			t.Fatalf("line %d is %d bytes, want %d", i, len(l), len(line))
		}
	}
}
