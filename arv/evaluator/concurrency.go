package evaluator

import (
	"context"
	"errors"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Run evaluates a whole program inside the root scope, so spawn works at
// top level and the program does not finish before its tasks do.
func (in *Interpreter) Run(node ast.Node, env *value.Environment) value.Object {
	return in.withScope(runtime.NewScope(in.Task.Ctx), func() value.Object {
		return in.Eval(node, env)
	})
}

// withScope runs body with s as the innermost scope and the task's context
// switched to s's context, so a failing child also stops body. It returns
// body's error if body failed, else the first child failure, else body's value.
func (in *Interpreter) withScope(s *runtime.Scope, body func() value.Object) value.Object {
	prev := in.Task.Ctx
	in.Task.Ctx = s.Context()
	in.scopes = append(in.scopes, s)
	defer func() {
		in.scopes = in.scopes[:len(in.scopes)-1]
		in.Task.Ctx = prev
	}()

	result := body()
	if e, ok := result.(*value.Error); ok {
		s.Cancel(errors.New(e.Message))
		s.Wait()
		return result
	}
	if err := s.Wait(); err != nil {
		return newError("%s", err)
	}
	return result
}

func (in *Interpreter) evalScope(node *ast.ScopeExpression, env *value.Environment) value.Object {
	return in.withScope(runtime.NewScope(in.Task.Ctx), func() value.Object {
		return in.evalBlockStatement(node.Body, env)
	})
}

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
	caps, err := in.Task.Caps.Attenuate(reqs)
	if err != nil {
		return newError("spawn in task %s: %s", in.Task.ID, err)
	}

	scope := in.scopes[len(in.scopes)-1]
	child := in.Task.Child(scope.Context(), caps)
	handle := value.NewTaskHandle(child.ID)
	scope.Go(func() error {
		result := runTask(child, fn, args)
		handle.Finish(result)
		if e, ok := result.(*value.Error); ok {
			return errors.New(e.Message)
		}
		return nil
	})
	return handle
}

// runTask runs fn(args) as task t on a fresh interpreter. The task body is
// its own implicit scope, so a task never finishes before its children.
func runTask(t *runtime.Task, fn value.Object, args []value.Object) value.Object {
	in := New(t)
	return in.withScope(runtime.NewScope(t.Ctx), func() value.Object {
		return in.applyFunction(fn, args)
	})
}

func (in *Interpreter) evalAwait(node *ast.AwaitExpression, env *value.Environment) value.Object {
	v := in.Eval(node.Task, env)
	if isError(v) {
		return v
	}
	h, ok := v.(*value.TaskHandle)
	if !ok {
		return newError("await: want TASK, got %s", v.Type())
	}
	select {
	case <-h.Done():
		return h.Result()
	case <-in.Task.Ctx.Done():
		return newError("task %s cancelled while awaiting %s: %s", in.Task.ID, h.ID, context.Cause(in.Task.Ctx))
	}
}
