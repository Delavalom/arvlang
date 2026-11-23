package evaluator

import (
	"context"
	"fmt"
	"io"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// bigProgram is about 4 MB of uncalled functions, then a loop no test
// waits for, so only a cancel or a deadline ends a run of it. Compiling
// it takes hundreds of milliseconds; it is parsed once, outside every
// timed part.
var bigProgram = sync.OnceValue(func() *ast.Program {
	var b strings.Builder
	for i := 0; b.Len() < 4<<20; i++ {
		fmt.Fprintf(&b, "let f%d = fn(a, b) { if (a < b) { [a, b, {\"k\": a + b * 2}] } else { f%d(b, a) } };\n", i, i)
	}
	b.WriteString("let big = [" + strings.TrimSuffix(strings.Repeat("0, ", 1000), ", ") + "];\n")
	b.WriteString("for a in big { for b in big { for c in big { a + b + c } } }\n")
	p := parser.New(lexer.New(b.String()))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		panic(fmt.Sprint(p.Errors()))
	}
	return prog
})

// runBig runs bigProgram on the given engine with ctx and l, calling at
// once the run starts (before compiling), and returns its value and when
// it returned.
func runBig(t *testing.T, walk bool, ctx context.Context, l runtime.Limits, at func()) (value.Object, time.Time) {
	t.Helper()
	prog := bigProgram()
	old := SetTreeWalk(walk)
	defer SetTreeWalk(old)
	startHook = at
	defer func() { startHook = nil }()
	in := New(runtime.NewRoot(ctx, runtime.NewCapSet(), io.Discard))
	result := in.RunLimited(prog, value.NewEnvironment(), l)
	return result, time.Now()
}

// smallMessage is the error a tiny endless program gets on the same
// engine with ctx and l: what a cancelled or timed-out run says today.
func smallMessage(t *testing.T, walk bool, ctx context.Context, l runtime.Limits) (string, string) {
	t.Helper()
	old := SetTreeWalk(walk)
	defer SetTreeWalk(old)
	p := parser.New(lexer.New("let big = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0];\nlet spin = fn(n) { for a in big { spin(n) } };\nfor a in big { for b in big { for c in big { for d in big { for e in big { for f in big { for g in big { for h in big { a } } } } } } } }\n"))
	prog := p.ParseProgram()
	in := New(runtime.NewRoot(ctx, runtime.NewCapSet(), io.Discard))
	e, ok := in.RunLimited(prog, value.NewEnvironment(), l).(*value.Error)
	if !ok {
		t.Fatalf("walk=%v: the small program did not fail", walk)
	}
	return e.Message, e.ErrKind()
}

// A7: a run whose context is cancelled while its program compiles stops
// within 50 ms of the cancel, with the error a cancelled run gives; so
// does a run whose deadline passes while it compiles. On the tree-walker
// (no compile) the same holds. A machine under heavy load can delay any
// goroutine past 50 ms, so each case has three tries and passes on the
// first within the bound; before A7 every try took the whole compile
// (about 400 ms here).
func TestCompileIsCancellable(t *testing.T) {
	const bound, tries = 50 * time.Millisecond, 3
	for _, walk := range []bool{false, true} {
		t.Run(fmt.Sprintf("walk=%v/cancel", walk), func(t *testing.T) {
			done2, stop := context.WithCancel(context.Background())
			stop()
			msg, kind := smallMessage(t, walk, done2, runtime.Limits{})
			var took []time.Duration
			for try := 0; try < tries; try++ {
				goruntime.GC()
				ctx, cancel := context.WithCancel(context.Background())
				var mu sync.Mutex
				var cancelled time.Time
				result, done := runBig(t, walk, ctx, runtime.Limits{}, func() {
					time.AfterFunc(20*time.Millisecond, func() {
						mu.Lock()
						cancelled = time.Now()
						mu.Unlock()
						cancel()
					})
				})
				cancel()
				e, ok := result.(*value.Error)
				if !ok || e.Message != msg || e.ErrKind() != kind {
					t.Fatalf("result %v, want %q (%s)", result, msg, kind)
				}
				mu.Lock()
				d := done.Sub(cancelled)
				mu.Unlock()
				if d <= bound {
					return
				}
				took = append(took, d)
			}
			t.Errorf("returned %v after the cancel, want at most %v", took, bound)
		})
		t.Run(fmt.Sprintf("walk=%v/deadline", walk), func(t *testing.T) {
			const timeout = 20 * time.Millisecond
			l := runtime.Limits{Timeout: timeout}
			msg, kind := smallMessage(t, walk, context.Background(), l)
			var took []time.Duration
			for try := 0; try < tries; try++ {
				goruntime.GC()
				var started time.Time
				result, done := runBig(t, walk, context.Background(), l, func() { started = time.Now() })
				e, ok := result.(*value.Error)
				if !ok || e.Message != msg || e.ErrKind() != kind {
					t.Fatalf("result %v, want %q (%s)", result, msg, kind)
				}
				d := done.Sub(started)
				if d <= timeout+bound {
					return
				}
				took = append(took, d)
			}
			t.Errorf("returned %v after the start, want at most %v", took, timeout+bound)
		})
	}
}

// A program of many times pollEvery nodes whose run is not cancelled
// runs wholly compiled: the polls never abandon a live compile.
func TestBigProgramsRunCompiled(t *testing.T) {
	src := strings.Repeat("let f = fn(a, b) { if (a < b) { [a, b, {\"k\": a + b * 2}] } else { f(b, a) } };\n", 2000) + "f(2, 1)"
	prog := parsed(t, src)
	if prog.Nodes < 10*pollEvery {
		t.Fatalf("premise: %d nodes", prog.Nodes)
	}
	onEngine(false, func() {
		in := New(runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard))
		got := in.Run(prog, value.NewEnvironment())
		if isError(got) || in.walks != 0 {
			t.Errorf("result %s, %d nodes ran on the tree-walker", got.Inspect(), in.walks)
		}
	})
}

// compileCtx recovers only its own abandon: a panic of any other kind
// in compile goes on up, to Run's recover, as before A7.
func TestCompileCtxRepanicsOtherPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("compileCtx swallowed a panic that was not its own")
		}
	}()
	compileCtx(context.Background(), (*ast.PrefixExpression)(nil)) // compilePrefix reads node.Right
}
