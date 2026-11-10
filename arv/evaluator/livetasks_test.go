package evaluator

import (
	"context"
	"fmt"
	"io"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// spawnDeep nests tasks n deep, each awaiting the next, so n tasks are
// alive at once at the bottom. It stops by itself, so a missing cap shows
// up as "no cap", not as a run that eats all memory.
const spawnDeep = "let f = fn(n) { if (n == 0) { \"no cap\" } else { await spawn f(n - 1) } };\nf(%d)"

// Each task starts at call depth 0, so spawn recursion is bounded only by
// the run's live-task cap: past it, spawn is an error, not a crash.
func TestSpawnRecursionHitsTheLiveTaskCap(t *testing.T) {
	before := goruntime.NumGoroutine()
	p := parser.New(lexer.New(fmt.Sprintf(spawnDeep, 200)))
	program := p.ParseProgram()
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	got := New(root).RunLimited(program, value.NewEnvironment(), runtime.Limits{LiveTasks: 50})
	wantErrorAt(t, got, "budget exceeded: tasks (limit 50 live tasks)", "1:55")
	settled(t, before)
}

// With no limits set, the default cap still applies.
func TestDefaultLiveTaskCap(t *testing.T) {
	before := goruntime.NumGoroutine()
	defer settled(t, before)
	got := runBounded(t, 60*time.Second, fmt.Sprintf(spawnDeep, 2000), nil)
	wantErrorHas(t, got, "budget exceeded: tasks (limit 1000 live tasks)")
}

// runIn evaluates input as the root task with b as the root scope's budget,
// so a test can inspect b afterwards.
func runIn(t *testing.T, b *runtime.Budget, input string, binds map[string]value.BuiltinFunction) value.Object {
	t.Helper()
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	env := value.NewEnvironment()
	for name, fn := range binds {
		env.Set(name, &value.Builtin{Fn: fn})
	}
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), io.Discard)
	in := New(root)
	return in.withScope(frame{runtime.NewScope(root.Ctx), b}, func() value.Object {
		return in.Eval(program, env)
	})
}

// A slot and a live task are given back however the task ends: a value,
// an error, a panic, or a spawn that gave up waiting for a slot.
func TestSlotsReleasedOnEveryPath(t *testing.T) {
	binds := map[string]value.BuiltinFunction{"boom": panics}
	for _, input := range []string{
		`let ok = fn() { 1 }; await spawn ok()`,
		`await spawn len(1)`,
		`await spawn boom()`,
		`scope (timeout_ms: 50) { spawn sleep(10000); spawn sleep(1) }`,
	} {
		b := runtime.Root(runtime.Limits{Tasks: 1, LiveTasks: 1})
		runIn(t, b, input, binds)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := b.Acquire(ctx)
		cancel()
		if err != nil {
			t.Fatalf("%s: slot or live task still held afterwards: %v", input, err)
		}
	}
}

// A panic that escapes runTask itself (not one in the task body, which
// runTask's own scope turns into an error) still gives back the slot and
// the live task, finishes the handle so await does not hang, and fails
// the scope.
func TestSlotsReleasedWhenRunTaskPanics(t *testing.T) {
	old := startTask
	startTask = func(*runtime.Task, *runtime.Budget, value.Object, []value.Object) value.Object {
		panic("runTask itself failed")
	}
	defer func() { startTask = old }()

	b := runtime.Root(runtime.Limits{Tasks: 1, LiveTasks: 1})
	got := runIn(t, b, `let ok = fn() { 1 }; await spawn ok()`, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Acquire(ctx); err != nil {
		t.Fatalf("slot or live task still held after runTask panicked: %v", err)
	}
	// Which failure the scope records first races: the panic, or the
	// awaiter seeing the unfinished handle.
	if e, ok := got.(*value.Error); !ok || !strings.Contains(e.Message, "panic in task: runTask itself failed") &&
		!strings.Contains(e.Message, "task root/0 did not finish") {
		t.Fatalf("got %s, want the panic or the unfinished task as the error", got.Inspect())
	}
}
