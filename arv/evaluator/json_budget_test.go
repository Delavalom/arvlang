package evaluator

import (
	"context"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// json's text costs its bytes: "abc" encodes as 5.
func TestJSONChargesItsText(t *testing.T) {
	if got := runCapsIn(t, 5, runtime.NewCapSet(), `json("abc")`); isError(got) {
		t.Fatalf("json under 5 bytes: %s", got.Inspect())
	}
	got := runCapsIn(t, 4, runtime.NewCapSet(), `json("abc")`)
	if !isError(got) || got.(*value.Error).ErrKind() != runtime.KindBudget {
		t.Fatalf("json under 4 bytes: got %s, want a budget error", got.Inspect())
	}
}

// doubling is a value of 2^levels "xxxxxxxxxx" strings, built in levels
// arrays of two: about 14 bytes of JSON per string.
func doubling(levels int) value.Object {
	var v value.Object = &value.String{Value: "xxxxxxxxxx"}
	for i := 0; i < levels; i++ {
		v = &value.Array{Elements: []value.Object{v, v}}
	}
	return v
}

// A value that encodes far larger than it is stops at the limit: the
// text is charged before it is written, one chunk at a time.
func TestJSONBlowupStopsAtTheLimit(t *testing.T) {
	env := value.NewEnvironment()
	env.Set("v", doubling(20)) // about 14 MiB of JSON
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), nil)
	in := New(root)
	b := runtime.Root(runtime.Limits{AllocBytes: 1 << 20})
	program := parse(t, `json(v)`)
	var got value.Object
	used := heapDuring(func() {
		got = in.withScope(frame{runtime.NewScope(root.Ctx), b}, func() value.Object {
			return in.Eval(program, env)
		})
	})
	wantErrorAt(t, got, allocErr, "1:1")
	// 1 MiB of chunks and at most one uncharged chunk: measured 1.07 MiB.
	// A strings.Builder, growing by a quarter at a time, allocated 5 MiB.
	if used > 2<<20 {
		t.Errorf("json past a 1 MiB limit allocated %d bytes, want under 2 MiB", used)
	}
}

// With no alloc limit, a cancelled task stops encoding at the next chunk.
func TestJSONStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	got := encode(t, ctx, doubling(40)) // about 15 TB of JSON
	if !isError(got) || got.(*value.Error).ErrKind() != runtime.KindCancelled {
		t.Fatalf("got %.80s, want a cancelled error", got.Inspect())
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("a cancelled json took %s", d)
	}
}
