package evaluator

import (
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
	root := frame{newScope(in.Task, l, true), runtime.Root(l)}
	result := in.withScope(root, func() value.Object {
		return in.Eval(node, env)
	})
	in.releaseDepth(root.budget, 0) // the next run (in the REPL) has a new pool
	if e, ok := result.(*value.Error); ok && (e.Pos == "" || e.Task == "") {
		return at(placeAtEnd(e, node), nil, in.Task.ID)
	}
	return result
}

// placeAtEnd stamps e, unless it has a position, with the position of
// node's last statement if node is a program, else of node; "1:1" for an
// empty program.
func placeAtEnd(e *value.Error, node ast.Node) *value.Error {
	if p, ok := node.(*ast.Program); ok && len(p.Statements) > 0 {
		node = p.Statements[len(p.Statements)-1]
	}
	if line, _ := ast.Pos(node); line == 0 && e.Pos == "" {
		c := *e
		c.Pos = "1:1"
		return &c
	}
	return at(e, node, e.Task)
}

// newScope opens a scope under t's context, with a deadline if l sets a
// timeout: in a resumed run, what the crashed run had left of it
// (runtime.Task.TimeoutLeft; atStart for the root scope). The error
// states the limit in whole ms, rounded up, so a host timeout under 1 ms
// does not read as "limit 0 ms".
func newScope(t *runtime.Task, l runtime.Limits, atStart bool) *runtime.Scope {
	ctx := t.Ctx
	if l.Timeout <= 0 {
		return runtime.NewScope(ctx)
	}
	ms := l.Timeout.Milliseconds()
	if l.Timeout%time.Millisecond != 0 {
		ms++
	}
	cause := &runtime.ExceededError{Kind: "timeout", Limit: ms}
	return runtime.NewTimedScope(ctx, t.TimeoutLeft(l.Timeout, atStart), cause)
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
		s.Wait()               // nothing started yet: this only releases the scope
		delete(in.collects, s) // evalScope registered it, if it collects
		return e
	}
	prev := in.Task.Ctx
	in.Task.Ctx = s.Context()
	in.scopes = append(in.scopes, f)
	defer func() {
		in.scopes = in.scopes[:len(in.scopes)-1]
		in.Task.Ctx = prev
		delete(in.collects, s) // a no-op unless s collects
	}()
	depth, evalDepth := in.depth, in.evalDepth
	defer func() {
		if r := recover(); r != nil {
			in.depth, in.evalDepth = depth, evalDepth // the panic skipped their unwinding
			s.Fail(fmt.Errorf("task %s panicked: %v", in.Task.ID, r))
			result = errorValue(s.Wait())
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
	err := s.Wait()
	expired := in.Task.Expired(s) // a check: a replay times out where the recording did
	switch {
	case expired != nil:
		// The scope's own deadline passed before it closed: report the
		// timeout itself, even if everything in it succeeded late, where
		// the body was when time ran out (if it got that far).
		e := errorOf(expired, runtime.KindBudget)
		if bodyErr != nil {
			e.Pos = result.(*value.Error).Pos
		}
		return e
	case err == nil:
		if e := in.unawaited(s); e != nil {
			return e // a collect scope's failures are never dropped
		}
		return result
	case err == bodyErr:
		return result // body failed first: keep its error value as is
	default:
		return errorValue(err)
	}
}

// failure carries an Arv error value through runtime.Scope, which stores
// Go errors, so the value (with its position, task and kind) comes out the
// other side. It tells the runtime the value's kind (runtime.Kinded), so a
// supervisor can decide whether to restart.
//
// Error() is the message clipped: it becomes the scope's cancellation
// cause, which every task the failure cancels formats into its own error
// ("task X cancelled: ..."). A user error's message is as long as the
// program likes and was charged once; copying it whole into each of N
// cancelled tasks' errors would be N uncharged copies. The value itself,
// read back through errorValue, keeps the whole message.
type failure struct{ v *value.Error }

func (f *failure) Error() string     { return clipped(f.v.Message) }
func (f *failure) ErrorKind() string { return f.v.ErrKind() }

// errorValue turns a scope's failure back into an Arv error value. A Go
// error that did not start as one (a panic, a timeout) keeps the kind it
// has, if any: a panic is a runtime error, a timeout a budget one.
func errorValue(err error) *value.Error {
	var f *failure
	if errors.As(err, &f) {
		return f.v
	}
	return errorOf(err, runtime.KindRuntime)
}

// evalScope opens a scope with the limits its options set, as a new level
// of the budget tree under the enclosing scope's (I6), supervising its
// tasks as its supervise option says.
func (in *Interpreter) evalScope(node *ast.ScopeExpression, env *value.Environment) value.Object {
	limits, policy, errv := in.scopeOptions(node.Options, env)
	if errv != nil {
		return errv
	}
	s := newScope(in.Task, limits, false)
	s.SetPolicy(policy)
	in.collecting(s)
	f := frame{s, in.top().budget.Child(limits)}
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
	var reqs []value.Object
	if node.Caps != nil {
		capsVal := in.Eval(node.Caps, env)
		if isError(capsVal) {
			return capsVal
		}
		arr, ok := capsVal.(*value.Array)
		if !ok {
			return newError("spawn ... with: want ARRAY of capabilities, got %s", capsVal.Type())
		}
		// Check every element's type first, allocating nothing: the
		// requests are not copied, since the array can be far larger
		// than what the scope may still charge.
		for _, el := range arr.Elements {
			if _, ok := el.(*value.Capability); !ok {
				return newError("spawn ... with: want CAPABILITY, got %s", el.Type())
			}
		}
		reqs = arr.Elements
	}
	caps, errv := in.attenuate(reqs)
	if errv != nil {
		return errv
	}

	// The handle, and the child's horizon: one spawn time per generation.
	if e := in.charge(handleSize + horizonEntrySize*int64(in.Task.HorizonLen()+1)); e != nil {
		return e
	}
	// The new task's effects follow every step that led to it.
	if e := in.settleSteps(); e != nil {
		return e
	}

	// A full scope (tasks:) makes spawn wait for a slot; cancellation ends
	// the wait. Past the run's live-task cap, spawn fails instead.
	top := in.top()
	if err := in.Task.Acquire(top.budget); err != nil {
		var ex *runtime.ExceededError
		if errors.As(err, &ex) {
			return errorOf(ex, runtime.KindBudget)
		}
		return newErrorKind(runtime.KindCancelled, "spawn in task %s cancelled: %s", in.Task.ID, err)
	}
	scope := top.scope
	child := in.Task.Child(scope.Context(), caps)
	handle := value.NewTaskHandle(child.ID, in.Task)
	in.track(top.scope, handle)
	// The scope's policy decides what a failure does (runtime.Supervise).
	// Under restart, each attempt is a fresh task and interpreter (IDs
	// root/0, root/0#2, ...) running fn(args) again, against the
	// environments as they were at this spawn (I8), in the same slot and
	// the same budgets: nothing is refunded between attempts.
	var result value.Object
	var last *runtime.Task // the attempt that ran last
	scope.SuperviseWith(child.ID, func(n int) error {
		t := child.Attempt(n)
		last = t
		result = newError("task %s did not finish", t.ID) // if runTask panics
		result = startTask(t, top.budget.Task(), fn, args)
		if e, ok := result.(*value.Error); ok {
			if e.Pos == "" || e.Task == "" { // e.g. spawn getenv("X"): no Arv node ran
				e = at(e, node, t.ID)
				result = e
			}
			return &failure{e}
		}
		return nil
	}, func(n int, wait func() bool) bool {
		return last.Backoff(child.AttemptID(n+1), wait) // a check of the attempt that failed
	}, func() {
		// Runs even if runTask panics: no slot or live task leaks, and no
		// awaiter is left waiting. Release comes first: an awaiter that
		// spawns again as soon as it wakes must not find this finished
		// task still counted against the live-task cap.
		top.budget.Release()
		handle.Finish(result)
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
// result. Nothing is allocated for the array until every task is done, and
// the result is charged before it is built: an array built outside the
// scope may be large, and a failed await must cost nothing.
func (in *Interpreter) evalAwait(node *ast.AwaitExpression, env *value.Environment) value.Object {
	v := in.Eval(node.Task, env)
	if isError(v) {
		return v
	}
	switch v := v.(type) {
	case *value.TaskHandle:
		if err := in.checkAwait(v); err != nil {
			return err
		}
		return in.wait(v)
	case *value.Array:
		for i, el := range v.Elements {
			h, ok := el.(*value.TaskHandle)
			if !ok {
				return newError("await: want TASK, got %s (element %d)", el.Type(), i)
			}
			if err := in.checkAwait(h); err != nil {
				return err
			}
		}
		for _, el := range v.Elements {
			if r := in.wait(el.(*value.TaskHandle)); isError(r) {
				return r
			}
		}
		if e := in.charge(arrayElemSize * int64(len(v.Elements))); e != nil {
			return e
		}
		results := make([]value.Object, len(v.Elements))
		for i, el := range v.Elements {
			results[i] = el.(*value.TaskHandle).Result()
		}
		return &value.Array{Elements: results}
	default:
		return newError("await: want TASK, got %s", v.Type())
	}
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
// It is a check (runtime.Task.WaitFor): a replay ends it as the recorded
// run's did.
func (in *Interpreter) wait(h *value.TaskHandle) value.Object {
	if err := in.Task.WaitFor(h.Done()); err != nil {
		return newErrorKind(runtime.KindCancelled, "task %s cancelled while awaiting %s: %s", in.Task.ID, h.ID, err)
	}
	h.MarkAwaited()
	return h.Result()
}
