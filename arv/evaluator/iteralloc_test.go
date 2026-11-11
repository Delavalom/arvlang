package evaluator

import (
	"context"
	"io"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// runOverBigArray evaluates input with a, a 1,000,000-element array built
// outside the scope (so uncharged), bound, under alloc_mb: 1, and returns
// the result and how many bytes the evaluation allocated.
func runOverBigArray(t *testing.T, input string) (value.Object, uint64) {
	t.Helper()
	env := value.NewEnvironment()
	elems := make([]value.Object, 1000000)
	for i := range elems {
		elems[i] = &value.Integer{}
	}
	env.Set("a", &value.Array{Elements: elems})
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	in := New(root)
	program := parse(t, input)
	var got value.Object
	used := heapDuring(func() {
		got = in.withScope(frame{runtime.NewScope(root.Ctx), runtime.Root(runtime.Limits{AllocBytes: 1 << 20})}, func() value.Object {
			return in.Eval(program, env)
		})
	})
	return got, used
}

// for ... in grows its result as it goes. It sized it for the whole
// iterable first, uncharged, so a loop that returned at once still
// allocated 16 bytes per element: one task calling such a function 5000
// times churned 14 GiB under alloc_mb: 1.
func TestForDoesNotPreallocate(t *testing.T) {
	src := `let g = fn() { for y in a { return 0 } }; g(); g(); g(); g(); g()`
	got, used := runOverBigArray(t, src)
	if got.Inspect() != "0" {
		t.Fatalf("got %s, want 0", got.Inspect())
	}
	if used > 4<<20 {
		t.Errorf("5 loops that return at once allocated %d MiB", used>>20)
	}
	src = `for x in a { 0 }`
	got, used = runOverBigArray(t, src)
	wantBareAllocError(t, got, "1:1")
	if used > 4<<20 {
		t.Errorf("a loop stopped by alloc_mb: 1 allocated %d MiB", used>>20)
	}
}

// await on an array checks its elements as it collects them. It
// allocated the handle slice for every element first, uncharged, so
// awaiting a 1,000,000-element array of integers allocated 8 MB before
// failing on element 0.
func TestAwaitArrayDoesNotPreallocate(t *testing.T) {
	got, used := runOverBigArray(t, `await a`)
	wantErrorAt(t, got, "await: want TASK, got INTEGER (element 0)", "1:1")
	if used > 1<<20 {
		t.Errorf("a refused await allocated %d KiB", used>>10)
	}
}
