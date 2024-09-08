package evaluator

import (
	"github.com/delavalom/arvlang/lang/monkeylexer/ast"
	"github.com/delavalom/arvlang/lang/monkeylexer/value"
)

// evalProgram evaluates a program value from the value system
// this functions is recursive and calls Eval() to evaluate the
// program statements, it takes as input a program and an environment
// and returns a value.Object which has methods to get the type of the
// object and the value of the object
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

// evalBlockStatement evaluates a block statement value from the value system
// this functions is recursive and calls Eval() to evaluate the
// block statements, it takes as input a block statement and an environment
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

// evalPrefixExpression evaluates a prefix expression value from the value system
// this functions compares the operator and calls the corresponding function
// to evaluate the expression, it takes as input an operator and a value.Object
func evalPrefixExpression(operator string, right value.Object) value.Object {
	switch operator {
	case "!":
		return evalBangOperatorExpression(right)
	case "-":
		return evalMinusPrefixOperatorExpression(right, operator)
	default:
		return newError("unknown operator: %s%s", operator, right.Type())

	}
}

// evalBangOperatorExpression evaluates a bang operator expression value from the value system
// this functions compares the right value and returns the opposite value
// it takes as input a value.Object and returns a value.Object
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

// evalMinusPrefixOperatorExpression evaluates a minus prefix operator expression value from the value system
// this functions compares the right value and returns the negative value
func evalMinusPrefixOperatorExpression(right value.Object, operator string) value.Object {
	if right.Type() != value.INTEGER_VAL {
		return newError("unknown operator: %s%s", operator, right.Type())
	}
	val := right.(*value.Integer).Value
	return &value.Integer{Value: -val}
}

// evalInfixExpression evaluates an infix expression value from the value system
// this functions compares the operator and calls the corresponding function
// to evaluate the expression, it takes as input an operator and two value.Objects
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

// evalIntegerInfixExpression evaluates an integer infix expression value from the value system
// this functions compares the operator and calls the corresponding function
// to evaluate the expression, it takes as input an operator and two value.Objects
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

// evalStringInfixExpression evaluates a string infix expression value from the value system
// this functions compares the operator and calls the corresponding function
// to evaluate the expression, it takes as input an operator and two value.Objects
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

// evalIfExpression evaluates an if expression value from the value system
// this functions compares the condition and calls the corresponding function
// to evaluate the expression, it takes as input an if expression and an environment
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

// evalIdentifier evaluates an identifier value from the value system
// this functions compares the identifier and returns the value of the identifier
// it takes as input an identifier and an environment
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

// evalExpresions evaluates an expression value from the value system
// this functions is recursive and calls Eval() to evaluate the
// expressions, it takes as input an expression and an environment
func evalExpressions(exps []ast.Expression, env *value.Environment) []value.Object {
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

// evalIndexExpression evaluates a call expression value from the value system
// this functions compares the function and calls the corresponding function
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

// evalArrayIndexExpression evaluates an array index expression value from the value system
// this functions compares the array and index and returns the value of the index
func evalArrayIndexExpression(array, index value.Object) value.Object {
	arrayObject := array.(*value.Array)
	idx := index.(*value.Integer).Value
	max := int64(len(arrayObject.Elements) - 1)
	if idx < 0 || idx > max {
		return newError("index out of bounds: %d", idx)
	}
	return arrayObject.Elements[idx]
}

// evalHashLiteral evaluates a hash literal value from the value system
// this functions compares the pairs and returns the value of the hash
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

// evalHashIndexExpression evaluates a hash index expression value from the value system
// this functions compares the hash and index and returns the value of the index
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
