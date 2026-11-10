package evaluator

import (
	"context"
	"fmt"
	"os"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

var (
	NIL   = &value.Nil{}
	TRUE  = &value.Boolean{Value: true}
	FALSE = &value.Boolean{Value: false}
)

// Interpreter evaluates Arv code on behalf of exactly one runtime.Task.
// It is not safe for concurrent use; every spawned task gets its own.
type Interpreter struct {
	Task      *runtime.Task
	depth     int              // Arv call depth, bounded by runtime.MaxDepth
	evalDepth int              // nested Eval calls, bounded by runtime.MaxEvalDepth
	scopes    []*runtime.Scope // innermost last; spawn adds tasks to the top one
}

// New returns an interpreter bound to task t.
func New(t *runtime.Task) *Interpreter { return &Interpreter{Task: t} }

// Eval runs node in a root scope with no capabilities, writing to stdout.
// Kept so the existing tests and the REPL keep working unchanged.
func Eval(node ast.Node, env *value.Environment) value.Object {
	root := runtime.NewRoot(context.Background(), runtime.NewCapSet(), os.Stdout)
	return New(root).Run(node, env)
}

// Eval evaluates node. It is the only way the evaluator recurses, so it
// bounds the total recursion: Arv calls and expression nesting multiply
// (a function nesting 200 levels deep, recursing 9999 times, is two
// million levels), and past runtime.MaxEvalDepth it fails with an Arv
// error instead of letting the Go stack overflow, which is fatal.
func (in *Interpreter) Eval(node ast.Node, env *value.Environment) value.Object {
	if in.evalDepth >= runtime.MaxEvalDepth {
		return newError("stack overflow: evaluation nested deeper than %d levels", runtime.MaxEvalDepth)
	}
	in.evalDepth++
	result := in.eval(node, env)
	in.evalDepth--
	return result
}

func (in *Interpreter) eval(node ast.Node, env *value.Environment) value.Object {
	if err := in.Task.Tick(); err != nil {
		return newError("task %s cancelled: %s", in.Task.ID, err)
	}
	switch node := node.(type) {
	case *ast.Program:
		return in.evalProgram(node, env)
	case *ast.ExpressionStatement:
		return in.Eval(node.Expression, env)
	case *ast.IntegerLiteral:
		return &value.Integer{Value: node.Value}
	case *ast.Boolean:
		return nativeBoolToBooleanObject(node.Value)
	case *ast.ReturnStatement:
		val := in.Eval(node.ReturnValue, env)
		if isError(val) {
			return val
		}
		return &value.ReturnValue{Value: val}
	case *ast.PrefixExpression:
		right := in.Eval(node.Right, env)
		if isError(right) {
			return right
		}
		return evalPrefixExpression(node.Operator, right)
	case *ast.InfixExpression:
		return in.evalInfixChain(node, env)
	case *ast.BlockStatement:
		return in.evalBlockStatement(node, env)
	case *ast.IfExpression:
		return in.evalIfExpression(node, env)
	case *ast.LetStatement:
		val := in.Eval(node.Value, env)
		if isError(val) {
			return val
		}
		env.Set(node.Name.Value, val)
	case *ast.Identifier:
		return evalIdentifier(node, env)
	case *ast.FunctionLiteral:
		return &value.Function{Parameters: node.Parameters, Env: env, Body: node.Body}
	case *ast.CallExpression:
		function := in.Eval(node.Function, env)
		if isError(function) {
			return function
		}
		args := in.evalExpressions(node.Arguments, env)
		if len(args) == 1 && isError(args[0]) {
			return args[0]
		}
		return in.applyFunction(function, args)
	case *ast.ArrayLiteral:
		elements := in.evalExpressions(node.Elements, env)
		if len(elements) == 1 && isError(elements[0]) {
			return elements[0]
		}
		return &value.Array{Elements: elements}
	case *ast.IndexExpression:
		left := in.Eval(node.Left, env)
		if isError(left) {
			return left
		}
		index := in.Eval(node.Index, env)
		if isError(index) {
			return index
		}
		return evalIndexExpression(left, index)
	case *ast.StringLiteral:
		return &value.String{Value: node.Value}
	case *ast.HashLiteral:
		return in.evalHashLiteral(node, env)
	case *ast.ScopeExpression:
		return in.evalScope(node, env)
	case *ast.SpawnExpression:
		return in.evalSpawn(node, env)
	case *ast.AwaitExpression:
		return in.evalAwait(node, env)
	}
	// let statements and any node without a value evaluate to nil. Eval
	// never returns Go nil, so builtins and operators need no nil checks.
	return NIL
}

// evalInfixChain evaluates node and the infix expressions down its left
// spine, as in 1 + 2 + 3 + ..., without recursing once per operator. The
// parser does not count such chains as nesting (see parser.MaxNesting), so
// they can be arbitrarily long; recursion could exhaust the Go stack. Each
// spine node still costs one Tick, as if Eval had visited it.
func (in *Interpreter) evalInfixChain(node *ast.InfixExpression, env *value.Environment) value.Object {
	spine := []*ast.InfixExpression{node}
	for {
		next, ok := spine[len(spine)-1].Left.(*ast.InfixExpression)
		if !ok {
			break
		}
		if err := in.Task.Tick(); err != nil {
			return newError("task %s cancelled: %s", in.Task.ID, err)
		}
		spine = append(spine, next)
	}
	left := in.Eval(spine[len(spine)-1].Left, env)
	for i := len(spine) - 1; i >= 0 && !isError(left); i-- {
		right := in.Eval(spine[i].Right, env)
		if isError(right) {
			return right
		}
		left = evalInfixExpression(spine[i].Operator, left, right)
	}
	return left
}

func (in *Interpreter) applyFunction(fn value.Object, args []value.Object) value.Object {
	switch fn := fn.(type) {
	case *value.Function:
		if len(args) != len(fn.Parameters) {
			return newError("wrong number of arguments: want=%d, got=%d", len(fn.Parameters), len(args))
		}
		if in.depth >= runtime.MaxDepth {
			return newError("stack overflow: call depth exceeded %d", runtime.MaxDepth)
		}
		in.depth++
		defer func() { in.depth-- }()
		extendedEnv := extendFunctionEnv(fn, args)
		evaluated := in.Eval(fn.Body, extendedEnv)
		return unwrapReturnValue(evaluated)
	case *value.Builtin:
		return fn.Fn(in.Task, args...)
	default:
		return newError("not a function: %s", fn.Type())
	}
}

func extendFunctionEnv(fn *value.Function, args []value.Object) *value.Environment {
	env := value.NewEnclosedEnvironment(fn.Env)
	for paramIdx, param := range fn.Parameters {
		env.Set(param.Value, args[paramIdx])
	}
	return env
}

func unwrapReturnValue(obj value.Object) value.Object {
	if returnValue, ok := obj.(*value.ReturnValue); ok {
		return returnValue.Value
	}
	return obj
}

func nativeBoolToBooleanObject(input bool) *value.Boolean {
	if input {
		return TRUE
	}
	return FALSE
}

func newError(format string, a ...interface{}) *value.Error {
	return &value.Error{Message: fmt.Sprintf(format, a...)}
}

func isError(obj value.Object) bool {
	if obj != nil {
		return obj.Type() == value.ERROR_VAL
	}
	return false
}
