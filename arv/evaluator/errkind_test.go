package evaluator

import (
	"bytes"
	"context"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// wantKind fails unless got is an error of the given kind.
func wantKind(t *testing.T, got value.Object, kind string) *value.Error {
	t.Helper()
	e, ok := got.(*value.Error)
	if !ok {
		t.Fatalf("got %s %s, want a %s error", got.Type(), got.Inspect(), kind)
	}
	if e.ErrKind() != kind {
		t.Fatalf("error %q has kind %q, want %q", e.Message, e.ErrKind(), kind)
	}
	return e
}

// Errors made by the evaluator itself carry the kind of their cause.
func TestEvaluatorErrorKinds(t *testing.T) {
	for _, tt := range []struct {
		name, src string
		l         runtime.Limits
		kind      string
	}{
		{"type error", `1 + "a"`, runtime.Limits{}, runtime.KindRuntime},
		{"division by zero", `1 / 0`, runtime.Limits{}, runtime.KindRuntime},
		{"unknown identifier", `nope`, runtime.Limits{}, runtime.KindRuntime},
		{"stack overflow", `let f = fn(n) { f(n + 1) }; f(0)`, runtime.Limits{}, runtime.KindRuntime},
		{"bad scope option", `scope (bogus: 1) { 1 }`, runtime.Limits{}, runtime.KindRuntime},
		{"steps", `scope (steps: 10) { for x in [1, 2, 3, 4, 5, 6] { x } }`, runtime.Limits{}, runtime.KindBudget},
		{"root steps", `let a = 1; let b = 2; a + b`, runtime.Limits{Steps: 3}, runtime.KindBudget},
		{"alloc", `let d = fn(s, n) { if (n == 0) { s } else { d(s + s, n - 1) } }; scope (alloc_mb: 1) { d("aaaaaaaaaaaaaaaa", 20) }`, runtime.Limits{}, runtime.KindBudget},
		{"timeout", `scope (timeout_ms: 20) { sleep(5000) }`, runtime.Limits{}, runtime.KindBudget},
		{"live tasks", `let f = fn() { sleep(5000) }; spawn f(); spawn f()`, runtime.Limits{LiveTasks: 1}, runtime.KindBudget},
		{"spawn denied", `let f = fn() { 1 }; spawn f() with [env("ARV_KIND_X")]`, runtime.Limits{}, runtime.KindCapability},
		{"child's timeout, through await", `let f = fn() { scope (timeout_ms: 10) { sleep(5000) } }; let t = spawn f(); await t`, runtime.Limits{}, runtime.KindBudget},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wantKind(t, runLimited(t, tt.src, tt.l), tt.kind)
		})
	}
}

// A cancelled task's errors are of kind cancelled.
func TestCancelledKind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := runtime.NewRoot(ctx, runtime.NewCapSet(), &bytes.Buffer{})
	wantKind(t, New(root).Run(parse(t, `puts(1)`), value.NewEnvironment()), runtime.KindCancelled)
}

// A panic in a task is a runtime error, not a lost kind.
func TestPanicKind(t *testing.T) {
	got, _ := runEnv(t, `let t = spawn boom(); await t`, map[string]value.BuiltinFunction{"boom": panics})
	wantKind(t, got, runtime.KindRuntime)
}
