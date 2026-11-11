package evaluator

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// longPath is a path of about 4000 bytes under dir, missing below "x".
func longPath(dir string) string {
	return dir + "/x" + strings.Repeat("/a", (4000-len(dir))/2)
}

// capsOf is the Arv array literal [c, c, ..., c] of n elements.
func capsOf(n int) string {
	return "[" + strings.TrimSuffix(strings.Repeat("c, ", n), ", ") + "]"
}

// spawn ... with charges every request's canonical form, plus a
// capability's header, to the spawning scope: canonicalising builds a new
// scope string per request and the child keeps it. It charged nothing, so
// 300 copies of one long capability made one spawn allocate 1.2 GiB under
// alloc_mb: 1 (with the old, quadratic Canonical), and the scope succeeded.
func TestSpawnCapsAreCharged(t *testing.T) {
	dir := t.TempDir()
	src := `let c = fs_read("` + longPath(dir) + `"); let f = fn() { 1 }; ` +
		`scope (alloc_mb: 1) { await spawn f() with ` + capsOf(300) + ` }`
	var got value.Object
	used := heapDuring(func() { got, _ = runWith(t, src, grant(t, runtime.FSRead, dir)) })
	wantBareAllocError(t, got, colOf(t, src, "spawn"))
	if used > 32<<20 {
		t.Errorf("a refused spawn allocated %d MiB", used>>20)
	}

	// The same request once fits, and so does a short one many times.
	// Through a symlink (t.TempDir is under one on macOS), a request is
	// charged as if every component of it existed, since what the walk
	// meets past a symlink must not change what a denial costs (review
	// round 11): about 5.5 MB for this path of 2000 components.
	canon, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		dir   string
		limit int
	}{{canon, 1}, {dir, 8}} {
		one := fmt.Sprintf(`let c = fs_read(%q); let f = fn() { 1 }; `+
			`scope (alloc_mb: %d) { await spawn f() with [c] }`, longPath(c.dir), c.limit)
		if got, _ := runWith(t, one, grant(t, runtime.FSRead, dir)); got.Inspect() != "1" {
			t.Errorf("one long request under %.40s: got %s, want 1", c.dir, got.Inspect())
		}
	}
	many := `let c = fs_read("` + dir + `/s"); let f = fn() { 1 }; ` +
		`scope (alloc_mb: 1) { await spawn f() with ` + capsOf(300) + ` }`
	if got, _ := runWith(t, many, grant(t, runtime.FSRead, dir)); got.Inspect() != "1" {
		t.Errorf("300 short requests: got %s, want 1", got.Inspect())
	}
}

// The cost of spawn ... with [c] is the cost of spawn ... with [] plus a
// capability's header, its canonical scope's bytes and what
// canonicalising it charges, exactly.
func TestSpawnCapCostIsExact(t *testing.T) {
	dir := t.TempDir()
	caps := runtime.NewCapSet(grant(t, runtime.FSRead, dir))
	canon := grant(t, runtime.FSRead, dir+"/s").Scope
	base := `let c = fs_read("` + dir + `/s"); let f = fn() { 1 }; await spawn f() with `
	free := cheapest(t, caps, base+`[]`)
	want := free + arrayElemSize + capabilitySize + int64(len(canon)) + canonCost(t, dir+"/s")
	if got := cheapest(t, caps, base+`[c]`); got != want {
		t.Errorf("spawn with [c] costs %d bytes, want %d (%d without it)", got, want, free)
	}
}

// cheapest is the smallest alloc limit, in bytes, under which input
// succeeds, found by bisection (cost is monotonic in the limit).
func cheapest(t *testing.T, caps runtime.CapSet, input string) int64 {
	t.Helper()
	lo, hi := int64(1), int64(1<<20)
	if got := runCapsIn(t, hi, caps, input); isError(got) {
		t.Fatalf("%s under 1 MiB: %s", input, got.Inspect())
	}
	for lo < hi {
		mid := (lo + hi) / 2
		if isError(runCapsIn(t, mid, caps, input)) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// spawn stops canonicalising its requests once its task is cancelled,
// as a builtin does not start: each request reads the filesystem, and
// there can be any number of them. With timeout_ms: 100, 2000 copies of a
// long capability ran for 19 s before the scope reported its timeout.
func TestSpawnCapsStopWhenCancelled(t *testing.T) {
	dir := t.TempDir()
	c, err := runtime.Literal(runtime.FSRead, longPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	cs := make([]value.Object, 2000)
	for i := range cs {
		cs[i] = &value.Capability{Cap: c}
	}
	env := value.NewEnvironment()
	env.Set("cs", &value.Array{Elements: cs})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := runtime.NewRoot(ctx, runtime.NewCapSet(grant(t, runtime.FSRead, dir)), io.Discard)
	in := New(root)
	program := parse(t, `let f = fn() { 1 }; spawn f() with cs`)
	var got value.Object
	used := heapDuring(func() {
		got = in.withScope(frame{runtime.NewScope(root.Ctx), runtime.Root(runtime.Limits{})}, func() value.Object {
			return in.Eval(program, env)
		})
	})
	e, ok := got.(*value.Error)
	if !ok || !strings.Contains(e.Message, "cancelled") {
		t.Fatalf("got %s, want a cancellation error", got.Inspect())
	}
	if used > 1<<20 {
		t.Errorf("a cancelled spawn allocated %d KiB canonicalising its requests", used>>10)
	}
}
