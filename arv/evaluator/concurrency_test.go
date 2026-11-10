package evaluator

import (
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func TestTasksRunInParallel(t *testing.T) {
	start := time.Now()
	got, _ := runWith(t, `
		let work = fn(n) { sleep(200); n * 10 };
		scope {
			let a = spawn work(1);
			let b = spawn work(2);
			let c = spawn work(3);
			[await a, await b, await c]
		}`)
	if got.Inspect() != "[10, 20, 30]" {
		t.Fatalf("got %s", got.Inspect())
	}
	if elapsed := time.Since(start); elapsed > 450*time.Millisecond {
		t.Fatalf("took %s; tasks did not run in parallel", elapsed)
	}
}

func TestScopeWaitsForUnawaitedTasks(t *testing.T) {
	_, out := runWith(t, `
		let late = fn() { sleep(50); puts("child") };
		scope { spawn late(); };
		puts("after");`)
	if out != "child\nafter\n" {
		t.Fatalf("output = %q; scope returned before its child", out)
	}
}

func TestProgramWaitsForTopLevelTasks(t *testing.T) {
	_, out := runWith(t, `let late = fn() { sleep(30); puts("done") }; spawn late();`)
	if out != "done\n" {
		t.Fatalf("output = %q", out)
	}
}

func TestFailFastCancelsSiblings(t *testing.T) {
	t.Setenv("ARV_SECRET", "x")
	start := time.Now()
	got, _ := runWith(t, `
		let slow = fn() { sleep(10000); "finished" };
		let bad = fn() { getenv("ARV_SECRET") };
		scope {
			spawn slow();
			spawn bad();
			sleep(10000);
		}`)
	e, ok := got.(*value.Error)
	if !ok || !strings.Contains(e.Message, "capability denied") {
		t.Fatalf("got %v, want the child's capability error", got)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("sibling and scope body were not cancelled")
	}
}

func TestChildrenHoldNothingByDefault(t *testing.T) {
	t.Setenv("ARV_X", "x")
	envX := grant(t, runtime.Env, "ARV_X")

	got, _ := runWith(t, `let t = spawn getenv("ARV_X"); await t`, envX)
	wantDenied(t, got, "ARV_X")

	got, _ = runWith(t, `let t = spawn getenv("ARV_X") with [env("ARV_X")]; await t`, envX)
	if got.Inspect() != "x" {
		t.Fatalf("handed capability: got %s", got.Inspect())
	}
}

func TestSpawnCannotEscalate(t *testing.T) {
	got, _ := runWith(t, `spawn getenv("HOME") with [env("HOME")]`)
	wantDenied(t, got, `env("HOME")`)
}

func TestNestedTasksAttenuateFurther(t *testing.T) {
	t.Setenv("ARV_A", "a")
	t.Setenv("ARV_B", "b")
	caps := []runtime.Capability{grant(t, runtime.Env, "ARV_A"), grant(t, runtime.Env, "ARV_B")}
	got, _ := runWith(t, `
		let inner = fn() { getenv("ARV_B") };
		let outer = fn() {
			let t = spawn inner() with [env("ARV_B")];
			getenv("ARV_A") + await t
		};
		await spawn outer() with [env("ARV_A")]`, caps...)
	// outer never held ARV_B, so it cannot hand it to inner.
	wantDenied(t, got, "ARV_B")
}

func TestAwaitNeedsATask(t *testing.T) {
	got, _ := runWith(t, `await 5`)
	if e, ok := got.(*value.Error); !ok || !strings.Contains(e.Message, "want TASK") {
		t.Fatalf("got %v", got)
	}
}
