package evaluator

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// counted is a host builtin "effect" that counts its calls.
func counted(calls *atomic.Int64) map[string]value.BuiltinFunction {
	return map[string]value.BuiltinFunction{
		"effect": func(*runtime.Task, ...value.Object) value.Object {
			calls.Add(1)
			return NIL
		},
	}
}

// Steps are charged in chunks of 256, but a task charges what it owes
// before any builtin, so no effect starts once the steps limit is spent.
func TestNoEffectAfterStepsSpent(t *testing.T) {
	var calls atomic.Int64
	src := "scope (steps: 10) { " + strings.Repeat("let a = 1; ", 30) + "effect() }"
	got := runLimitedEnv(t, src, counted(&calls), runtime.Limits{})
	wantErrorHas(t, got, "budget exceeded: steps (limit 10)")
	if n := calls.Load(); n != 0 {
		t.Fatalf("effect ran %d times after the steps limit was spent", n)
	}
}

// spawn charges the spawning task's steps first: the scope spends its 10
// steps on the array literal, so the first spawn fails and no task starts.
// (A task spawned before a sibling spends the budget is covered by
// TestNoEffectAfterSiblingSpent.)
func TestNoSpawnedEffectAfterStepsSpent(t *testing.T) {
	for _, body := range []string{
		"spawn fn() { effect() }()", // the task runs Arv code first
		"spawn effect()",            // the task runs the builtin directly
	} {
		var calls atomic.Int64
		src := "scope (steps: 10) { for i in " + zeros(50) + " { " + body + " } }"
		got := runLimitedEnv(t, src, counted(&calls), runtime.Limits{})
		wantErrorHas(t, got, "budget exceeded: steps (limit 10)")
		if n := calls.Load(); n != 0 {
			t.Fatalf("%s: effect ran %d times after the steps limit was spent", body, n)
		}
	}
}

// Within the limit, the charges before builtins and spawns change nothing.
func TestEffectsWithinSteps(t *testing.T) {
	var calls atomic.Int64
	src := "scope (steps: 10000) { await for i in " + zeros(20) + " { spawn fn() { effect() }() }; effect(); 1 }"
	got := runLimitedEnv(t, src, counted(&calls), runtime.Limits{})
	if got.Inspect() != "1" {
		t.Fatalf("got %s, want 1", got.Inspect())
	}
	if n := calls.Load(); n != 21 {
		t.Fatalf("effect ran %d times, want 21", n)
	}
}
