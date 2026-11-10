package evaluator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Run evaluates a whole program inside the root scope, so spawn works at
// top level and the program does not finish before its tasks do.
func (in *Interpreter) Run(node ast.Node, env *value.Environment) value.Object {
	return in.RunLimited(node, env, runtime.Limits{})
}

// RunLimited is Run with l as the whole run's budget: the root of the
// budget tree, with l.Timeout as a deadline on the root scope.
//
// A nested scope's own errors (its last steps charge, its timeout once
// the body has returned) get a position from the Eval of the scope node,
// and a task's from its spawn node. The root scope has no enclosing node,
// so its unplaced errors are placed at the program's last statement, where
// the run ended.
func (in *Interpreter) RunLimited(node ast.Node, env *value.Environment, l runtime.Limits) value.Object {
	root := frame{newScope(in.Task.Ctx, l), runtime.Root(l)}
	result := in.withScope(root, func() value.Object {
		return in.Eval(node, env)
	})
	in.releaseDepth(root.budget, 0) // the next run (in the REPL) has a new pool
	if e, ok := result.(*value.Error); ok && e.Pos == "" {
		return placeAtEnd(e, node)
	}
	return result
}

// placeAtEnd stamps e with the position of node's last statement if node
// is a program, else of node; "1:1" for an empty program.
func placeAtEnd(e *value.Error, node ast.Node) *value.Error {
	if p, ok := node.(*ast.Program); ok && len(p.Statements) > 0 {
		node = p.Statements[len(p.Statements)-1]
	}
	if line, _ := ast.Pos(node); line == 0 {
		return &value.Error{Message: e.Message, StackTrace: e.StackTrace, Pos: "1:1"}
	}
	return at(e, node)
}

// newScope opens a scope under ctx, with a deadline if l sets a timeout.
// The error states the limit in whole ms, rounded up, so a host timeout
// under 1 ms does not read as "limit 0 ms".
func newScope(ctx context.Context, l runtime.Limits) *runtime.Scope {
	if l.Timeout <= 0 {
		return runtime.NewScope(ctx)
	}
	ms := l.Timeout.Milliseconds()
	if l.Timeout%time.Millisecond != 0 {
		ms++
	}
	cause := &runtime.ExceededError{Kind: "timeout", Limit: ms}
	return runtime.NewTimedScope(ctx, l.Timeout, cause)
}

// withScope runs body with f as the innermost scope and the task's context
// switched to f's scope's context, so a failing child also stops body. It
// returns the scope's first failure (I5), whether that was a child or body
// itself, else body's value; if the scope's own deadline ended it, the
// timeout error instead. body's error goes through s.Fail like a child's:
// if a child failed first, body's error is usually just the cancellation,
// and the child's error wins.
//
// A Go panic in body cancels s, waits for its tasks (I4: nothing started
// inside s outlives it) and becomes an Arv error naming the task.
func (in *Interpreter) withScope(f frame, body func() value.Object) (result value.Object) {
	s := f.scope
	// Steps evaluated so far belong to the enclosing scope, not to this one.
	if e := in.chargeSteps(); e != nil {
		s.Wait() // nothing started yet: this only releases the scope
		return e
	}
	prev := in.Task.Ctx
	in.Task.Ctx = s.Context()
	in.scopes = append(in.scopes, f)
	defer func() {
		in.scopes = in.scopes[:len(in.scopes)-1]
		in.Task.Ctx = prev
	}()
	depth, evalDepth := in.depth, in.evalDepth
	defer func() {
		if r := recover(); r != nil {
			in.depth, in.evalDepth = depth, evalDepth // the panic skipped their unwinding
			s.Fail(fmt.Errorf("task %s panicked: %v", in.Task.ID, r))
			result = newError("%s", s.Wait())
		}
	}()

	result = body()
	if e := in.chargeSteps(); e != nil && !isError(result) {
		result = e // the last few steps passed the limit
	}
	var bodyErr error
	if e, ok := result.(*value.Error); ok {
		bodyErr = &failure{e}
		s.Fail(bodyErr)
	}
	switch err := s.Wait(); {
	case s.Expired() != nil:
		// The scope's own deadline passed before it closed: report the
		// timeout itself, even if everything in it succeeded late, where
		// the body was when time ran out (if it got that far).
		e := newError("%s", s.Expired())
		if bodyErr != nil {
			e.Pos = result.(*value.Error).Pos
		}
		return e
	case err == nil:
		return result
	case err == bodyErr:
		return result // body failed first: keep its error value as is
	default:
		return errorValue(err)
	}
}

// failure carries an Arv error value through runtime.Scope, which stores
// Go errors, so the value (with its position) comes out the other side.
type failure struct{ v *value.Error }

func (f *failure) Error() string { return f.v.Message }

// errorValue turns a scope's failure back into an Arv error value.
func errorValue(err error) *value.Error {
	var f *failure
	if errors.As(err, &f) {
		return f.v
	}
	return newError("%s", err)
}

// evalScope opens a scope with the limits its options set, as a new level
// of the budget tree under the enclosing scope's (I6).
func (in *Interpreter) evalScope(node *ast.ScopeExpression, env *value.Environment) value.Object {
	limits, errv := in.scopeLimits(node.Options, env)
	if errv != nil {
		return errv
	}
	f := frame{newScope(in.Task.Ctx, limits), in.top().budget.Child(limits)}
	return in.withScope(f, func() value.Object {
		return in.evalBlockStatement(node.Body, env)
	})
}

// top is the innermost open scope. Run opens the first, so evaluation
// always has one; only spawn checks, to fail cleanly if misused.
func (in *Interpreter) top() frame { return in.scopes[len(in.scopes)-1] }

func (in *Interpreter) evalSpawn(node *ast.SpawnExpression, env *value.Environment) value.Object {
	if len(in.scopes) == 0 {
		return newError("spawn outside a scope")
	}
	fn := in.Eval(node.Call.Function, env)
	if isError(fn) {
		return fn
	}
	args := in.evalExpressions(node.Call.Arguments, env)
	if len(args) == 1 && isError(args[0]) {
		return args[0]
	}
	var reqs []runtime.Capability
	if node.Caps != nil {
		capsVal := in.Eval(node.Caps, env)
		if isError(capsVal) {
			return capsVal
		}
		arr, ok := capsVal.(*value.Array)
		if !ok {
			return newError("spawn ... with: want ARRAY of capabilities, got %s", capsVal.Type())
		}
		for _, el := range arr.Elements {
			c, ok := el.(*value.Capability)
			if !ok {
				return newError("spawn ... with: want CAPABILITY, got %s", el.Type())
			}
			reqs = append(reqs, c.Cap)
		}
	}
	// Each request is canonicalised (reading the filesystem) into a new
	// scope the child keeps: charge it, and stop once the task is
	// cancelled, since a program can pass any number of requests.
	var stop *value.Error
	caps, err := in.Task.Caps.AttenuateEach(reqs, func(c runtime.Capability) error {
		if err := in.Task.Err(); err != nil {
			stop = newError("spawn in task %s cancelled: %s", in.Task.ID, err)
		} else {
			stop = in.charge(capabilitySize + int64(len(c.Scope)))
		}
		if stop != nil {
			return errors.New(stop.Message)
		}
		return nil
	})
	if stop != nil {
		return stop
	}
	if err != nil {
		return newError("spawn in task %s: %s", in.Task.ID, err)
	}

	if e := in.charge(handleSize); e != nil {
		return e
	}
	// The new task's effects follow every step that led to it.
	if e := in.settleSteps(); e != nil {
		return e
	}

	// A full scope (tasks:) makes spawn wait for a slot; cancellation ends
	// the wait. Past the run's live-task cap, spawn fails instead.
	top := in.top()
	if err := top.budget.Acquire(in.Task.Ctx); err != nil {
		var ex *runtime.ExceededError
		if errors.As(err, &ex) {
			return newError("%s", ex)
		}
		return newError("spawn in task %s cancelled: %s", in.Task.ID, err)
	}
	scope := top.scope
	child := in.Task.Child(scope.Context(), caps)
	handle := value.NewTaskHandle(child.ID, in.Task)
	scope.Go(func() error {
		// Both run even if runTask panics: no slot or live task leaks, and
		// no awaiter is left waiting. Release runs first: an awaiter that
		// spawns again as soon as it wakes must not find this finished
		// task still counted against the live-task cap.
		var result value.Object = newError("task %s did not finish", child.ID)
		defer func() { handle.Finish(result) }()
		defer top.budget.Release()
		result = startTask(child, top.budget.Task(), fn, args)
		if e, ok := result.(*value.Error); ok {
			if e.Pos == "" { // e.g. spawn getenv("X"): no Arv node ran
				e = at(e, node)
				result = e
			}
			return &failure{e}
		}
		return nil
	})
	return handle
}

// startTask is runTask; a test replaces it to make runTask itself panic.
// It is set in init: runTask leads back to evalSpawn, an init cycle.
var startTask func(*runtime.Task, *runtime.Budget, value.Object, []value.Object) value.Object

func init() { startTask = runTask }

// runTask runs fn(args) as task t on a fresh interpreter, with b as the
// budget level of its body. The task body is its own implicit scope, so a
// task never finishes before its children, and withScope turns a panic in
// the body into "task <id> panicked: ..." there.
func runTask(t *runtime.Task, b *runtime.Budget, fn value.Object, args []value.Object) value.Object {
	in := New(t)
	defer in.releaseDepth(b, 0) // the task's stack is gone: give its depth back
	return in.withScope(frame{runtime.NewScope(t.Ctx), b}, func() value.Object {
		return in.applyFunction(fn, args)
	})
}

// evalAwait evaluates await t, which yields t's result, and await [t1, t2],
// which awaits each in order and yields the array of results. Every element
// is checked before anything is awaited; the first error, in order, is the
// result.
func (in *Interpreter) evalAwait(node *ast.AwaitExpression, env *value.Environment) value.Object {
	v := in.Eval(node.Task, env)
	if isError(v) {
		return v
	}
	var handles []*value.TaskHandle
	switch v := v.(type) {
	case *value.TaskHandle:
		if err := in.checkAwait(v); err != nil {
			return err
		}
		return in.wait(v)
	case *value.Array:
		handles = make([]*value.TaskHandle, len(v.Elements))
		for i, el := range v.Elements {
			h, ok := el.(*value.TaskHandle)
			if !ok {
				return newError("await: want TASK, got %s (element %d)", el.Type(), i)
			}
			if err := in.checkAwait(h); err != nil {
				return err
			}
			handles[i] = h
		}
	default:
		return newError("await: want TASK, got %s", v.Type())
	}
	results := make([]value.Object, len(handles))
	for i, h := range handles {
		r := in.wait(h)
		if isError(r) {
			return r
		}
		results[i] = r
	}
	return in.alloc(&value.Array{Elements: results}, arrayElemSize*int64(len(results)))
}

// checkAwait enforces the structured rule: a task may await only handles
// spawned by itself or by one of its descendants. Awaiting itself, a
// sibling or an ancestor could wait on a task that is itself waiting (via
// its scope) for the awaiter: a cycle nothing would break. With this rule
// every await points down the task tree, so no cycle can form.
func (in *Interpreter) checkAwait(h *value.TaskHandle) *value.Error {
	if h.Owner == in.Task || in.Task.IsAncestorOf(h.Owner) {
		return nil
	}
	if h.ID == in.Task.ID {
		return newError("await: task %s cannot await itself", h.ID)
	}
	return newError("await: task %s was not spawned by task %s or its descendants", h.ID, in.Task.ID)
}

// wait blocks until h finishes or the awaiting task is cancelled.
func (in *Interpreter) wait(h *value.TaskHandle) value.Object {
	select {
	case <-h.Done():
		return h.Result()
	case <-in.Task.Ctx.Done():
		return newError("task %s cancelled while awaiting %s: %s", in.Task.ID, h.ID, context.Cause(in.Task.Ctx))
	}
}
