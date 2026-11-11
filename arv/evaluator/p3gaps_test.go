package evaluator

// Tests added by a mutation-testing pass over the Phase 3 hardening code
// (review rounds 7-11): each one fails under a small change to the code
// that the other tests let through.

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
	"unsafe"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// TestSpawnCapsStopWhenCancelled passes 2000 copies of one request, which
// AttenuateEach canonicalises once, so it holds even if spawn never
// checks for cancellation. With 2000 distinct requests, a cancelled
// spawn must still stop after the first.
func TestCancelledSpawnStopsAtDistinctRequests(t *testing.T) {
	dir := t.TempDir()
	cs := make([]value.Object, 2000)
	for i := range cs {
		c, err := runtime.Literal(runtime.FSRead, fmt.Sprintf("%s/%d", longPath(dir), i))
		if err != nil {
			t.Fatal(err)
		}
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
		t.Fatalf("got %.200s, want a cancellation error", got.Inspect())
	}
	if used > 1<<20 {
		t.Errorf("a cancelled spawn allocated %d KiB canonicalising distinct requests", used>>10)
	}
}

// await on an array charges its result only once every task is done, so
// an await that fails on a task's error costs nothing: under a budget
// with no room for the result array, it still reports the task's error.
// The task fails late (the sleep, which costs nothing), so a charge made
// before waiting would fail the scope first. The outcome does not depend
// on timing unless that charge comes first.
func TestFailedAwaitArrayCostsNothing(t *testing.T) {
	caps := runtime.NewCapSet()
	pre := `let f = fn(n) { %s10 / n }; let a = [spawn f(1), spawn f(%d)]; `
	limit := cheapest(t, caps, fmt.Sprintf(pre, "", 1)+`0`)
	got := runCapsIn(t, limit, caps, fmt.Sprintf(pre, "sleep(200); ", 0)+`await a`)
	if e, ok := got.(*value.Error); !ok || !strings.Contains(e.Message, "division by zero") {
		t.Errorf("under %d bytes: got %s, want the task's error", limit, got.Inspect())
	}
}

// read_file stats every other fs grant the task holds (confiningRoot),
// and each stat copies the grant's path: it is charged by its length, so
// a grant 1000 bytes longer costs exactly 1000 bytes more per call.
func TestFSEffectChargesEachGrantByLength(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(dir+"/a", 0o755); err != nil {
		t.Fatal(err)
	}
	f := dir + "/a/small"
	if err := os.WriteFile(f, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	cost := func(other string) int64 {
		caps := runtime.NewCapSet(grant(t, runtime.FSRead, dir+"/a"), grant(t, runtime.FSRead, other))
		return cheapest(t, caps, fmt.Sprintf(`len(read_file(%q))`, f))
	}
	short := cost(dir + "/b/x")
	long := cost(dir + "/b/x" + strings.Repeat("/"+strings.Repeat("c", 199), 5))
	if long-short != 1000 {
		t.Errorf("a grant 1000 bytes longer costs %d bytes more (%d against %d)", long-short, long, short)
	}
}

// A URL one byte over maxURLLen is refused before it is parsed.
func TestHTTPGetRefusesURLJustOverCap(t *testing.T) {
	head := "http://127.0.0.1/"
	u := head + strings.Repeat("a", maxURLLen-len(head)+1)
	got, _ := runWith(t, fmt.Sprintf(`http_get(%q)`, u))
	want := fmt.Sprintf("http_get: URL is %d bytes long (max %d)", maxURLLen+1, maxURLLen)
	if e, ok := got.(*value.Error); !ok || e.Message != want {
		t.Errorf("got %.200s, want %q", got.Inspect(), want)
	}
}

// Clipped error text is cut at a rune boundary, so it stays valid UTF-8
// and quoting it shows no stray byte.
func TestClipCutsAtARuneBoundary(t *testing.T) {
	s := strings.Repeat("a", clipLen-1) + "é" + strings.Repeat("b", 100)
	if got := clipped(s); !utf8.ValidString(got) || !strings.HasPrefix(got, strings.Repeat("a", clipLen-1)+"...") {
		t.Errorf("clipped = %q", got)
	}
	if got := quoteClipped(s); strings.Contains(got, `\x`) {
		t.Errorf("quoteClipped = %q", got)
	}
}

// Only + is charged as a concatenation: another operator on two strings
// builds nothing and fails as unknown, even with no room left for what +
// would have built.
func TestStringOperatorOtherThanPlusCostsNothing(t *testing.T) {
	caps := runtime.NewCapSet()
	pre := fmt.Sprintf(`let s = %q; `, strings.Repeat("x", 1000))
	free := cheapest(t, caps, pre+`true`)
	for _, op := range []string{"==", "-"} {
		got := runCapsIn(t, free, caps, pre+"s "+op+" s")
		if e, ok := got.(*value.Error); !ok || !strings.HasPrefix(e.Message, "unknown operator: STRING "+op) {
			t.Errorf("s %s s under %d bytes: got %.200s, want unknown operator", op, free, got.Inspect())
		}
	}
}

// A capability value's charge covers at least the value itself.
func TestCapabilitySizeCoversTheValue(t *testing.T) {
	if n := int64(unsafe.Sizeof(value.Capability{})); capabilitySize < n {
		t.Errorf("capabilitySize is %d, a capability value takes %d bytes", capabilitySize, n)
	}
}
