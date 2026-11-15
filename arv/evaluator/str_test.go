package evaluator

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// values are bound before each str case: one of every kind puts prints.
const values = `let i = -42; let s = "text"; let b = true; let n = if (false) { 1 };
let a = [1, "x", [true]]; let h = {"k": [1]}; let f = fn(x) { x + 1 };`

// str(v) is exactly the text puts prints for v.
func TestStrRendersAsPuts(t *testing.T) {
	for _, name := range []string{"i", "s", "b", "n", "a", "h", "f"} {
		_, printed := runWith(t, values+"puts("+name+")")
		got, _ := runWith(t, values+"str("+name+")")
		s, ok := got.(*value.String)
		if !ok || s.Value+"\n" != printed {
			t.Errorf("str(%s) = %s, puts printed %q", name, got.Inspect(), printed)
		}
	}
	got, _ := runWith(t, "str(1, 2)")
	wantErrorAt(t, got, "str: want 1 argument, got 2", "1:1")
}

// A string is returned as it is, so it costs nothing; any other value
// costs its text's bytes, charged once.
func TestStrChargesItsText(t *testing.T) {
	if n := fit(t, `str("abc")`, bytesLimit); n != 1 {
		t.Errorf("str of a string costs %d bytes, want nothing", n)
	}
	if n := fit(t, `str(12345)`, bytesLimit); n != 5 {
		t.Errorf("str(12345) costs %d bytes, want 5", n)
	}
	if n := fit(t, `str([1, 2])`, bytesLimit) - fit(t, `[1, 2]`, bytesLimit); n != 6 {
		t.Errorf("str([1, 2]) costs %d bytes more than the array, want 6 for \"[1, 2]\"", n)
	}
	// Past value.InspectChunk the text is charged as it is built; the
	// total is still its length, exactly.
	const build = `let d = fn(n, a) { if (n == 0) { a } else { d(n - 1, [a, a]) } }; let v = d(15, 7);`
	text := runLimited(t, build+"v", runtime.Limits{}).Inspect()
	if len(text) <= value.InspectChunk {
		t.Fatalf("text is %d bytes, want more than one chunk", len(text))
	}
	if n := fit(t, build+"str(v)", bytesLimit) - fit(t, build+"v", bytesLimit); n != int64(len(text)) {
		t.Errorf("str(v) costs %d bytes more than v, want %d", n, len(text))
	}
}

// A value that prints far larger than it is stops at the limit, and a
// cancelled task stops rendering at the next chunk.
func TestStrStopsAtTheLimitAndWhenCancelled(t *testing.T) {
	env := value.NewEnvironment()
	env.Set("v", doubling(20)) // about 14 MiB of text
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), nil)
	in := New(root)
	program := parse(t, `str(v)`)
	var got value.Object
	used := heapDuring(func() {
		got = in.withScope(frame{runtime.NewScope(root.Ctx), runtime.Root(runtime.Limits{AllocBytes: 1 << 20})},
			func() value.Object { return in.Eval(program, env) })
	})
	wantErrorAt(t, got, allocErr, "1:1")
	// Measured 5.0 MiB: the builder grows by a quarter at a time (a known
	// limit, see the notes), but nowhere near the 14 MiB of the full text.
	if used > 8<<20 {
		t.Errorf("str past a 1 MiB limit allocated %d bytes", used)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	got = str(runtime.NewRoot(ctx, runtime.NewCapSet(), io.Discard), func(int64) error { return nil }, doubling(40))
	if !isError(got) || got.(*value.Error).ErrKind() != runtime.KindCancelled {
		t.Fatalf("got %.80s, want a cancelled error", got.Inspect())
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("a cancelled str took %s", d)
	}
}

// Interpolation calls str by a second name, which no program can bind.
func TestStrHasAnUnbindableName(t *testing.T) {
	if builtins[ast.StrBuiltin] != builtins["str"] || builtins["str"] == nil {
		t.Fatalf("builtins[%q] is not str", ast.StrBuiltin)
	}
}
