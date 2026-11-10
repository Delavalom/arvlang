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
	if isError(iter) {
		return iter
	}
	arr, ok := iter.(*value.Array)
	if !ok {
		return newError("for: want ARRAY, got %s", iter.Type())
	}
	out := make([]value.Object, 0, len(arr.Elements))
	for _, el := range arr.Elements {
		loopEnv := value.NewEnclosedEnvironment(env)
		loopEnv.Set(node.Var.Value, el)
		v := in.Eval(node.Body, loopEnv)
		if t := v.Type(); t == value.RETURN_VALUE_VAL || t == value.ERROR_VAL {
			return v
		}
		out = append(out, v)
	}
	return &value.Array{Elements: out}
}
