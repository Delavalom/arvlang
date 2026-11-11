package evaluator

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// bigCaps is an Arv array of n copies of fs_read(dir), built in Go so the
// test measures only the spawn.
func bigCaps(t *testing.T, dir string, n int) *value.Array {
	t.Helper()
	c, err := runtime.Literal(runtime.FSRead, dir)
	if err != nil {
		t.Fatal(err)
	}
	cs := make([]value.Object, n)
	for i := range cs {
		cs[i] = &value.Capability{Cap: c}
	}
	return &value.Array{Elements: cs}
}

// spawn ... with copied every element of its array into a request slice
// before charging anything, so a spawn that failed at its first request,
// or at the charge that went over alloc_mb, still allocated 32 bytes (plus
// append's growth) per element, uncharged: about 80 MB for 1,000,000
// elements, and 100 tasks doing it at once reached a 6.9 GiB heap under
// alloc_mb: 1. It now takes the elements one at a time.
func TestSpawnWithDoesNotPreallocate(t *testing.T) {
	dir := t.TempDir()
	cs := bigCaps(t, dir, 1000000)

	// A cancelled task stops at its first request.
	env := value.NewEnvironment()
	env.Set("cs", cs)
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
	if e, ok := got.(*value.Error); !ok || !strings.Contains(e.Message, "cancelled") {
		t.Fatalf("got %s, want a cancellation error", got.Inspect())
	}
	if used > 1<<20 {
		t.Errorf("a cancelled spawn allocated %d KiB", used>>10)
	}

	// alloc_mb stops it about 10,000 requests in, so what it allocates
	// does not depend on how much longer the array is. (Canonicalising
	// each request it charges churns about 3 KB, a known limit.)
	src := `let f = fn() { 1 }; scope (alloc_mb: 1) { spawn f() with cs }`
	program = parse(t, src)
	usedBy := func(n int) uint64 {
		env := value.NewEnvironment()
		env.Set("cs", &value.Array{Elements: cs.Elements[:n]})
		root := runtime.NewRoot(context.Background(), runtime.NewCapSet(grant(t, runtime.FSRead, dir)), io.Discard)
		used := heapDuring(func() { got = New(root).Run(program, env) })
		wantBareAllocError(t, got, colOf(t, src, "spawn"))
		return used
	}
	short, long := usedBy(20000), usedBy(len(cs.Elements))
	if long > short+4<<20 {
		t.Errorf("with 1,000,000 requests a spawn stopped by alloc_mb: 1 allocated %d MiB, %d MiB with 20,000", long>>20, short>>20)
	}
}

// A bad element is still found before any request is canonicalised.
func TestSpawnWithChecksTypesFirst(t *testing.T) {
	got, _ := runWith(t, `let f = fn() { 1 }; scope { spawn f() with [fs_read("/nope"), 5] }`)
	wantErrorHas(t, got, "spawn ... with: want CAPABILITY, got INTEGER")
}
