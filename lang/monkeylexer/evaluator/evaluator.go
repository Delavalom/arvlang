package evaluator

import (
	"fmt"

	"github.com/delavalom/arvlang/lang/monkeylexer/ast"
	"github.com/delavalom/arvlang/lang/monkeylexer/value"
)

var (
	NIL   = &value.Nil{}
	TRUE  = &value.Boolean{Value: true}
	FALSE = &value.Boolean{Value: false}
)

func Eval(node ast.Node, env *value.Environment) value.Object {
	switch node := node.(type) {
	case *ast.Program:
		return evalProgram(node, env)
		// return evalStatements(node.Statements)
	case *ast.ExpressionStatement:
		return Eval(node.Expression, env)
		// Expressions
	case *ast.IntegerLiteral:
		return &value.Integer{Value: node.Value}
	case *ast.Boolean:
		return nativeBoolToBooleanObject(node.Value)
	case *ast.ReturnStatement:
		val := Eval(node.ReturnValue, env)
		if isError(val) {
			return val
		}
		return &value.ReturnValue{Value: val}
	case *ast.PrefixExpression:
		right := Eval(node.Right, env)
		if isError(right) {
			return right
		}
		return evalPrefixExpression(node.Operator, right)
	case *ast.InfixExpression:
		left := Eval(node.Left, env)
		if isError(left) {
			return left
		}
		right := Eval(node.Right, env)
		if isError(right) {
			return right
		}
		return evalInfixExpression(node.Operator, left, right)
	case *ast.BlockStatement:
		return evalBlockStatement(node, env)
	case *ast.IfExpression:
		return evalIfExpression(node, env)
	case *ast.LetStatement:
		val := Eval(node.Value, env)
		if isError(val) {
			return val
		}
		env.Set(node.Name.Value, val)
	case *ast.Identifier:
		return evalIdentifier(node, env)
	case *ast.FunctionLiteral:
		params := node.Parameters
		body := node.Body
		return &value.Function{Parameters: params, Env: env, Body: body}
	case *ast.CallExpression:
		function := Eval(node.Function, env)
		if isError(function) {
			return function
		}
		args := evalExpressions(node.Arguments, env)
		if len(args) == 1 && isError(args[0]) {
			return args[0]
		}
		return applyFunction(function, args)
	case *ast.ArrayLiteral:
		elements := evalExpressions(node.Elements, env)
		if len(elements) == 1 && isError(elements[0]) {
			return elements[0]
		}
		return &value.Array{Elements: elements}
	case *ast.IndexExpression:
		left := Eval(node.Left, env)
		if isError(left) {
			return left
		}
		index := Eval(node.Index, env)
		if isError(index) {
			return index
		}
		return evalIndexExpression(left, index)
	case *ast.StringLiteral:
		return &value.String{Value: node.Value}
	case *ast.HashLiteral:
		return evalHashLiteral(node, env)
	}

	return nil
}

func evalProgram(program *ast.Program, env *value.Environment) value.Object {
	var result value.Object
	for _, statement := range program.Statements {
		result = Eval(statement, env)
		switch result := result.(type) {
		case *value.ReturnValue:
			return result.Value
		case *value.Error:
			return result
		}
	}
	return result
}

func evalStatements(stmts []ast.Statement, env *value.Environment) value.Object {
	var result value.Object
	for _, statement := range stmts {
		result = Eval(statement, env)
		if returnValue, ok := result.(*value.ReturnValue); ok {
			return returnValue.Value
		}
	}
	return result
}

func evalBlockStatement(block *ast.BlockStatement, env *value.Environment) value.Object {
	var result value.Object
	for _, statement := range block.Statements {
		result = Eval(statement, env)
		if result != nil {
			rt := result.Type()
			if rt == value.RETURN_VALUE_VAL || rt == value.ERROR_VAL {
				return result
			}
		}
	}
	return result
}

func nativeBoolToBooleanObject(input bool) *value.Boolean {
	if input {
		return TRUE
	}
	return FALSE
}

func evalPrefixExpression(operator string, right value.Object) value.Object {
	switch operator {
	case "!":
		return evalBangOperatorExpression(right)
	case "-":
		return evalMinusPrefixOperatorExpression(right)
	default:
		return newError("unknown operator: %s%s", operator, right.Type())

	}
}

func evalBangOperatorExpression(right value.Object) value.Object {
	switch right {
	case TRUE:
		return FALSE
	case FALSE:
		return TRUE
	case NIL:
		return TRUE
	default:
		return FALSE
	}
}

func evalMinusPrefixOperatorExpression(right value.Object) value.Object {
	if right.Type() != value.INTEGER_VAL {
		return NIL
	}
	val := right.(*value.Integer).Value
	return &value.Integer{Value: -val}
}

func evalInfixExpression(operator string,
	left, right value.Object,
) value.Object {
	switch {
	case left.Type() == value.INTEGER_VAL && right.Type() == value.INTEGER_VAL:
		return evalIntegerInfixExpression(operator, left, right)
	case left.Type() == value.STRING_VAL && right.Type() == value.STRING_VAL:
		return evalStringInfixExpression(operator, left, right)
	case operator == "==":
		return nativeBoolToBooleanObject(left == right)
	case operator == "!=":
		return nativeBoolToBooleanObject(left != right)
	case left.Type() != right.Type():
		return newError("type mismatch: %s %s %s",
			left.Type(), operator, right.Type())
	default:
		return newError("unknown operator: %s %s %s", left.Type(), operator, right.Type())

	}
}

func evalIntegerInfixExpression(operator string,
	left, right value.Object,
) value.Object {
	leftVal := left.(*value.Integer).Value
	rightVal := right.(*value.Integer).Value
	switch operator {
	case "+":
		return &value.Integer{Value: leftVal + rightVal}
	case "-":
		return &value.Integer{Value: leftVal - rightVal}
	case "*":
		return &value.Integer{Value: leftVal * rightVal}
	case "/":
		return &value.Integer{Value: leftVal / rightVal}
	case "<":
		return nativeBoolToBooleanObject(leftVal < rightVal)
	case ">":
		return nativeBoolToBooleanObject(leftVal > rightVal)
	case "==":
		return nativeBoolToBooleanObject(leftVal == rightVal)
	case "!=":
		return nativeBoolToBooleanObject(leftVal != rightVal)
	default:
		return newError("unknown operator: %s %s %s",
			left.Type(), operator, right.Type())
	}
}

func evalStringInfixExpression(operator string,
	left, right value.Object,
) value.Object {
	if operator != "+" {
		return newError("unknown operator: %s %s %s", left.Type(), operator, right.Type())
	}
	leftVal := left.(*value.String).Value
	rightVal := right.(*value.String).Value
	return &value.String{Value: leftVal + rightVal}
}

func evalIfExpression(ie *ast.IfExpression, env *value.Environment) value.Object {
	condition := Eval(ie.Condition, env)
	if isError(condition) {
		return condition
	}
	if condition == TRUE {
		return Eval(ie.Consequence, env)
	} else if ie.Alternative != nil {
		return Eval(ie.Alternative, env)
	} else {
		return NIL
	}
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

func evalIdentifier(
	node *ast.Identifier, env *value.Environment,
) value.Object {
	if val, ok := env.Get(node.Value); ok {
		return val
	}
	if builtin, ok := builtins[node.Value]; ok {
		return builtin
	}
	return newError("identifier not found: " + node.Value)
}

func evalExpressions(
	exps []ast.Expression, env *value.Environment,
) []value.Object {
	var result []value.Object
	for _, e := range exps {
		evaluated := Eval(e, env)
		if isError(evaluated) {
			return []value.Object{evaluated}
		}
		result = append(result, evaluated)
	}
	return result
}

func extendFunctionEnv(fn *value.Function, args []value.Object,
) *value.Environment {
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

func applyFunction(fn value.Object, args []value.Object) value.Object {
	switch fn := fn.(type) {
	case *value.Function:
		extendedEnv := extendFunctionEnv(fn, args)
		evaluated := Eval(fn.Body, extendedEnv)
		return unwrapReturnValue(evaluated)
	case *value.Builtin:
		return fn.Fn(args...)
	default:
		return newError("not a function: %s", fn.Type())
	}
}

func evalIndexExpression(left, index value.Object) value.Object {
	switch {
	case left.Type() == value.ARRAY_VAL && index.Type() == value.INTEGER_VAL:
		return evalArrayIndexExpression(left, index)
	case left.Type() == value.HASH_VAL:
		return evalHashIndexExpression(left, index)
	default:
		return newError("index operator not supported: %s", left.Type())
	}
}

func evalArrayIndexExpression(array, index value.Object) value.Object {
	arrayObject := array.(*value.Array)
	idx := index.(*value.Integer).Value
	max := int64(len(arrayObject.Elements) - 1)
	if idx < 0 || idx > max {
		return newError("index out of bounds: %d", idx)
	}
	return arrayObject.Elements[idx]
}

func evalHashLiteral(
	node *ast.HashLiteral, env *value.Environment,
) value.Object {
	pairs := make(map[value.HashKey]value.HashPair)
	for keyNode, valueNode := range node.Pairs {
		key := Eval(keyNode, env)
		if isError(key) {
			return key
		}
		hashKey, ok := key.(value.Hashable)
		if !ok {
			return newError("unusable as hash key: %s", key.Type())
		}
		val := Eval(valueNode, env)
		if isError(val) {
			return val
		}
		hashed := hashKey.HashKey()
		pairs[hashed] = value.HashPair{Key: key, Value: val}
	}
	return &value.Hash{Pairs: pairs}
}

func evalHashIndexExpression(hash, index value.Object) value.Object {
	hashObject := hash.(*value.Hash)
	key, ok := index.(value.Hashable)
	if !ok {
		return newError("unusable as hash key: %s", index.Type())
	}
	pair, ok := hashObject.Pairs[key.HashKey()]
	if !ok {
		return NIL
	}
	return pair.Value
}
