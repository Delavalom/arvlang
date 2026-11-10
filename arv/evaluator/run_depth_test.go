package evaluator

import (
	"fmt"
	goruntime "runtime"
	"sync"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// deep recurses n calls (about 7 levels of evaluator recursion each), then
// calls bottom().
const deep = "let deep = fn(n) { if (n == 0) { bottom() } else { deep(n - 1) } };\n"

// meet returns a bottom() builtin that holds each caller until n have
// arrived (or its task is cancelled), so n deep stacks exist at once.
func meet(n int) map[string]value.BuiltinFunction {
	var mu sync.Mutex
	arrived, all := 0, make(chan struct{})
	return map[string]value.BuiltinFunction{"bottom": func(t *runtime.Task, _ ...value.Object) value.Object {
		mu.Lock()
		if arrived++; arrived == n {
			close(all)
		}
		mu.Unlock()
		select {
		case <-all:
			return &value.Integer{Value: 1}
		case <-t.Ctx.Done():
			return newError("bottom cancelled")
		}
	}}
}

// Every task can recurse to runtime.MaxEvalDepth, and each level costs Go
// stack, so the live-task cap alone let one run hold up to 1000 deep
// stacks (tens of GB). The run's tasks share one pool of evaluator depth:
// eight tasks about 63000 levels deep at once pass MaxRunEvalDepth.
func TestRunDepthBoundsDeepTasksTogether(t *testing.T) {
	before := goruntime.NumGoroutine()
	got := runBounded(t, 2*time.Minute, deep+`await for i in `+zeros(8)+` { spawn deep(9000) }`, meet(8))
	wantErrorHas(t, got, fmt.Sprintf("stack overflow: the run's tasks together nested deeper than %d levels", runtime.MaxRunEvalDepth))
	settled(t, before)
}

// Depth is given back as stacks unwind and as tasks end, so deep tasks one
// after another, or one task going deep again and again, never run out.
func TestRunDepthIsGivenBack(t *testing.T) {
	for _, src := range []string{
		deep + `len(for i in ` + zeros(10) + ` { await spawn deep(9000) })`,
		deep + `len(for i in ` + zeros(10) + ` { deep(9000) })`,
		deep + `len(await for i in ` + zeros(200) + ` { spawn deep(20) })`,
	} {
		got := runBounded(t, 2*time.Minute, src, meet(1))
		if _, ok := got.(*value.Integer); !ok {
			t.Fatalf("got %s:\n%s", got.Inspect(), src)
		}
	}
}
