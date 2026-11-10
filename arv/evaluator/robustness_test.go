package evaluator

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// runEnv is runWith with extra Go builtins bound in the global environment,
// so a test can hand a script a builtin that panics or ignores cancellation
// without touching the shared builtins table.
func runEnv(t *testing.T, input string, binds map[string]value.BuiltinFunction, caps ...runtime.Capability) (value.Object, string) {
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
	var out bytes.Buffer
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(caps...), &out)
	return New(root).Run(program, env), out.String()
}

func panics(*runtime.Task, ...value.Object) value.Object { panic("boom") }

// stubborn returns a builtin that ignores cancellation: it sleeps d, then
// sets done. If Run returns with done unset, a task outlived its scope.
func stubborn(d time.Duration, done *atomic.Bool) value.BuiltinFunction {
	return func(*runtime.Task, ...value.Object) value.Object {
		time.Sleep(d)
		done.Store(true)
		return NIL
	}
}

// I4: a task body that panics still waits for its own children, and the
// panic becomes an Arv error naming the task.
func TestTaskPanicWaitsForGrandchildren(t *testing.T) {
	var done atomic.Bool
	got, _ := runEnv(t, `let p = fn() { spawn stubborn(); boom() }; await spawn p()`,
		map[string]value.BuiltinFunction{"stubborn": stubborn(100*time.Millisecond, &done), "boom": panics})
	if !done.Load() {
		t.Fatal("Run returned before the panicking task's child finished")
	}
	wantErrorHas(t, got, "task root/0 panicked: boom")
}

// I4 at the root: a panic in the program body still waits for its tasks.
func TestRootPanicWaitsForTasks(t *testing.T) {
	var done atomic.Bool
	got, _ := runEnv(t, `spawn stubborn(); boom()`,
		map[string]value.BuiltinFunction{"stubborn": stubborn(100*time.Millisecond, &done), "boom": panics})
	if !done.Load() {
		t.Fatal("Run returned before the root's task finished")
	}
	wantErrorHas(t, got, "task root panicked: boom")
}

// A panic inside a nested scope is an ordinary error from that scope on.
func TestPanicInNestedScope(t *testing.T) {
	var done atomic.Bool
	got, _ := runEnv(t, `let f = fn() { scope { spawn stubborn(); boom() } }; await spawn f()`,
		map[string]value.BuiltinFunction{"stubborn": stubborn(50*time.Millisecond, &done), "boom": panics})
	if !done.Load() {
		t.Fatal("nested scope returned before its task finished")
	}
	wantErrorHas(t, got, "task root/0 panicked: boom")
}

// deaf waits until its task is cancelled, then reports its own error
// instead of the cancellation, like a builtin that ignores the cause.
func deaf(t *runtime.Task, _ ...value.Object) value.Object {
	<-t.Ctx.Done()
	return newError("body failed second")
}

// I5: the first failure wins, even when the scope body then fails with an
// error of its own. Ordering is forced: the body cannot fail until the
// child's failure has cancelled it.
func TestFirstErrorWinsOverBodyError(t *testing.T) {
	for _, src := range []string{
		`let bad = fn() { child_failed_first }; scope { spawn bad(); deaf() }`,
		`let bad = fn() { child_failed_first }; spawn bad(); deaf()`,
		`let bad = fn() { child_failed_first }; let t = fn() { spawn bad(); deaf() }; await spawn t()`,
	} {
		got, _ := runEnv(t, src, map[string]value.BuiltinFunction{"deaf": deaf})
		wantErrorHas(t, got, "identifier not found: child_failed_first")
	}
}
