package evaluator

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// session runs programs one after another on one interpreter and one
// environment, as the REPL does. A handle bound in an earlier run outlives
// that run's scope, so a later run can await a task that has already
// failed without that failure cancelling the later run. Within one run,
// fail-fast (I5) cancels the awaiter as soon as an awaited task fails,
// which hides how await [..] itself treats an error.
type session struct {
	in  *Interpreter
	env *value.Environment
}

func newSession(binds map[string]value.BuiltinFunction) *session {
	env := value.NewEnvironment()
	for name, fn := range binds {
		env.Set(name, &value.Builtin{Fn: fn})
	}
	var out bytes.Buffer
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), &out)
	return &session{New(root), env}
}

func (s *session) run(t *testing.T, input string) value.Object {
	t.Helper()
	return s.in.Run(parse(t, input), s.env)
}

func parse(t *testing.T, input string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(input))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	return program
}

// failedHandles leaves ok (a finished task, 10) and bad (a failed task,
// division by zero at 1:17) bound in s.
func failedHandles(t *testing.T, s *session) {
	t.Helper()
	if got := s.run(t, "let f = fn(n) { 10 / n };\nlet ok = spawn f(1);"); isError(got) {
		t.Fatalf("setup run: %s", got.Inspect())
	}
	wantErrorAt(t, s.run(t, "let bad = spawn f(0);"), "division by zero", "1:17")
}

// await [..] yields the first error in array order and stops there: it
// neither puts the error into a result array nor lets the program go on.
func TestAwaitArrayStopsAtFirstError(t *testing.T) {
	var marked atomic.Bool
	s := newSession(map[string]value.BuiltinFunction{
		"mark": func(*runtime.Task, ...value.Object) value.Object {
			marked.Store(true)
			return NIL
		},
	})
	failedHandles(t, s)

	got := s.run(t, "let r = await [ok, bad, ok];\nmark();\nr")
	wantErrorAt(t, got, "division by zero", "1:17")
	if marked.Load() {
		t.Fatal("the statement after the failed await ran")
	}
}

// await [..] returns at the first error in array order without waiting for
// the elements after it. Here the element after the failed handle can only
// finish when its scope is cancelled, which happens once the await has
// returned the error: if await waited for it, the run would never end.
func TestAwaitArrayDoesNotWaitPastAnError(t *testing.T) {
	g := gate()
	s := newSession(g)
	failedHandles(t, s)

	done := make(chan value.Object, 1)
	program := parse(t, "await [bad, spawn wait()]")
	go func() { done <- s.in.Run(program, s.env) }()
	var got value.Object
	select {
	case got = <-done:
	case <-time.After(30 * time.Second):
		g["open"](nil) // let the run end, so no goroutine outlives the test
		<-done
		t.Fatal("await [bad, spawn wait()] kept waiting after bad's error")
	}
	wantErrorAt(t, got, "division by zero", "1:17")
}
