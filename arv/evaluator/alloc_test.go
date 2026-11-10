package evaluator

import (
	"fmt"
	goruntime "runtime"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// push copies its array, so growing one to 5000 elements allocates
// 16 * (1 + 2 + ... + 5000) bytes, about 191 MiB.
const grow = `let grow = fn(n, acc) { if (n == 0) { acc } else { grow(n - 1, push(acc, "x")) } };`

func TestAllocLimit(t *testing.T) {
	got, _ := runWith(t, grow+"\nscope (alloc_mb: 1) { len(grow(5000, [])) }")
	wantErrorHas(t, got, "budget exceeded: alloc (limit 1048576 bytes)")
	if e := got.(*value.Error); e.Message != "budget exceeded: alloc (limit 1048576 bytes)" || e.Pos == "" {
		t.Fatalf("got %q at %q, want the bare budget error with a position", e.Message, e.Pos)
	}
	got, _ = runWith(t, grow+"scope (alloc_mb: 1024) { len(grow(5000, [])) }")
	if got.Inspect() != "5000" {
		t.Fatalf("under alloc_mb: 1024: got %s", got.Inspect())
	}
}

// What each construct costs, exactly: the program fits in that many bytes
// and fails with one byte less.
func TestAllocSizes(t *testing.T) {
	tests := []struct {
		input string
		bytes int64
	}{
		{`[1, 2, 3]`, 48},
		{`{"a": 1, "b": "long string literal"}`, 96},
		{`"ab" + "cde"`, 5},
		{`"a" + "b" + "c"`, 5}, // "ab", then "abc"
		{`for x in [1, 2] { x }`, 64},
		{`let f = fn() { 1 }; await [spawn f(), spawn f()]`, 64},
		{`push([1], 2)`, 48},
		{`let a = [[1, 2, 3, 4]]; last(a)`, 80}, // last shares, so costs nothing
		{`"literal"`, 0},
		{`len("abc")`, 0},
	}
	for _, tt := range tests {
		if tt.bytes == 0 {
			if got := runIn(t, runtime.Root(runtime.Limits{AllocBytes: 1}), tt.input, nil); value.Object(got).Type() == value.ERROR_VAL {
				t.Errorf("%s: %s, want it to allocate nothing", tt.input, got.Inspect())
			}
			continue
		}
		if got := runIn(t, runtime.Root(runtime.Limits{AllocBytes: tt.bytes}), tt.input, nil); isError(got) {
			t.Errorf("%s: %s under %d bytes", tt.input, got.Inspect(), tt.bytes)
		}
		got := runIn(t, runtime.Root(runtime.Limits{AllocBytes: tt.bytes - 1}), tt.input, nil)
		if !isError(got) || got.(*value.Error).Message != fmt.Sprintf("budget exceeded: alloc (limit %d bytes)", tt.bytes-1) {
			t.Errorf("%s under %d bytes: got %s, want alloc exceeded", tt.input, tt.bytes-1, got.Inspect())
		}
	}
}

// A builtin's new record counts its strings too: a 1000-byte body costs
// its bytes plus the hash's two pairs.
func TestAllocBuiltinRecord(t *testing.T) {
	big := make([]byte, 1000)
	for i := range big {
		big[i] = 'a'
	}
	binds := map[string]value.BuiltinFunction{
		"fetch": func(*runtime.Task, ...value.Object) value.Object {
			return value.NewHash(map[string]value.Object{"status": &value.Integer{Value: 200}, "body": &value.String{Value: string(big)}})
		},
	}
	if got := runIn(t, runtime.Root(runtime.Limits{AllocBytes: 1096}), `fetch()["status"]`, binds); got.Inspect() != "200" {
		t.Fatalf("under 1096 bytes: %s", got.Inspect())
	}
	got := runIn(t, runtime.Root(runtime.Limits{AllocBytes: 1095}), `fetch()["status"]`, binds)
	wantErrorHas(t, got, "budget exceeded: alloc (limit 1095 bytes)")
}

// I6 and tasks: the smaller limit wins, and a task's allocations count
// against the scope it was spawned in.
func TestNestedAllocShrinks(t *testing.T) {
	got, _ := runWith(t, grow+`scope (alloc_mb: 1) { scope (alloc_mb: 1024) { len(grow(5000, [])) } }`)
	wantErrorHas(t, got, "budget exceeded: alloc (limit 1048576 bytes)")
	got, _ = runWith(t, grow+`scope (alloc_mb: 1) { let t = spawn grow(5000, []); len(await t) }`)
	wantErrorHas(t, got, "budget exceeded: alloc (limit 1048576 bytes)")
}

// I4: running out of allocation fails the scope, which cancels and waits
// for its other tasks.
func TestAllocExhaustionCancelsTasks(t *testing.T) {
	before := goruntime.NumGoroutine()
	got, _ := runWith(t, grow+`scope (alloc_mb: 1) { spawn sleep(10000); len(grow(5000, [])) }`)
	wantErrorHas(t, got, "budget exceeded: alloc (limit 1048576 bytes)")
	settled(t, before)
}
