package evaluator

import (
	"context"
	"io"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// push charges its copy before it makes it, as a concatenation does, so a
// refused push builds nothing. It used to copy the whole array (16 bytes
// an element) and only then fail: 16 MB for a 1,000,000-element array
// built outside the scope, under alloc_mb: 1, and about 380 MiB more with
// 100 tasks pushing at once.
func TestPushOverLimitBuildsNothing(t *testing.T) {
	env := value.NewEnvironment()
	env.Set("a", &value.Array{Elements: make([]value.Object, 1000000)})
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	in := New(root)
	b := runtime.Root(runtime.Limits{AllocBytes: 1 << 20})
	program := parse(t, `len(push(a, 0))`)
	var got value.Object
	used := heapDuring(func() {
		got = in.withScope(frame{runtime.NewScope(root.Ctx), b}, func() value.Object {
			return in.Eval(program, env)
		})
	})
	wantErrorAt(t, got, allocErr, "1:5")
	if used > 1<<20 {
		t.Errorf("a refused push allocated %d bytes, want under 1 MiB", used)
	}
}

// push still costs 16 bytes an element of its result (here after the
// literal's three).
func TestPushCostsItsResult(t *testing.T) {
	cost := int64(arrayElemSize * (3 + 4))
	if got := runCapsIn(t, cost, runtime.NewCapSet(), `len(push([1, 2, 3], 4))`); got.Inspect() != "4" {
		t.Errorf("push under %d bytes: got %s, want 4", cost, got.Inspect())
	}
	if got := runCapsIn(t, cost-1, runtime.NewCapSet(), `len(push([1, 2, 3], 4))`); !isError(got) {
		t.Errorf("push under %d bytes: got %s, want the alloc error", cost-1, got.Inspect())
	}
}
