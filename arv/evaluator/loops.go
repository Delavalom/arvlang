package evaluator

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)

// evalFor evaluates for x in xs { body }: body once per element, each time
// in a fresh environment enclosing env with x bound, so neither x nor a
// let in the body leaks out and closures capture their own x. The result
// is the array of body values. An error or a return stops the loop and is
// the result. Only arrays are iterable.
func (in *Interpreter) evalFor(node *ast.ForExpression, env *value.Environment) value.Object {
	iter := in.Eval(node.Iterable, env)
	return in.forEach(node.Var.Value, iter, env, func(loopEnv *value.Environment) value.Object {
		return in.Eval(node.Body, loopEnv)
	})
}

// forEach is the loop of evalFor once the iterable is evaluated (iter):
// body runs in each element's environment, binding name. Compiled code
// (compileFor) shares it.
func (in *Interpreter) forEach(name string, iter value.Object, env *value.Environment, body func(*value.Environment) value.Object) value.Object {
	if abrupt(iter) {
		return iter
	}
	arr, ok := iter.(*value.Array)
	if !ok {
		return newError("for: want ARRAY, got %s", iter.Type())
	}
	// out grows as the loop goes, never sized for the whole iterable up
	// front: that was uncharged, even for a loop that stops at once. Each
	// iteration's charge covers the 16 bytes (and growth) it appends.
	var out []value.Object
	for _, el := range arr.Elements {
		if e := in.charge(envSize + bindingSize); e != nil {
			return e
		}
		loopEnv := value.NewEnclosedEnvironment(env)
		loopEnv.Set(name, el)
		v := body(loopEnv)
		if t := v.Type(); t == value.RETURN_VALUE_VAL || t == value.ERROR_VAL {
			return v
		}
		out = append(out, v)
	}
	return in.alloc(&value.Array{Elements: out}, arrayElemSize*int64(len(out)))
}
