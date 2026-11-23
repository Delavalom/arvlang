package evaluator

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)

// compile turns node and everything under it into code. Each case does
// what eval's case for the node type does, with its children compiled
// once here instead of dispatched on every evaluation. A node type with
// no case here runs on the tree-walker: its code calls eval's case
// itself, after the same checks (enter), so it costs the same.
func compile(node ast.Node) *code {
	c := &code{node: node}
	switch node := node.(type) {
	case *ast.Program:
		c.run = compileProgram(node)
	case *ast.ExpressionStatement:
		c.run = compileExpressionStatement(node)
	case *ast.IntegerLiteral:
		v := node.Value
		c.run = func(*Interpreter, *value.Environment) value.Object { return value.Int(v) }
	case *ast.Boolean:
		b := nativeBoolToBooleanObject(node.Value)
		c.run = func(*Interpreter, *value.Environment) value.Object { return b }
	case *ast.StringLiteral:
		// A new value each time, as eval makes: the effect log's codec
		// numbers long strings by identity (value.Encode).
		s := node.Value
		c.run = func(*Interpreter, *value.Environment) value.Object { return &value.String{Value: s} }
	case *ast.ReturnStatement:
		c.run = compileReturn(node)
	case *ast.PrefixExpression:
		c.run = compilePrefix(node)
	case *ast.BlockStatement:
		c.run = compileBlockRun(node)
	case *ast.IfExpression:
		c.run = compileIf(node)
	case *ast.LetStatement:
		c.run = compileLet(node)
	case *ast.Identifier:
		c.run = compileIdentifier(node)
	case *ast.InfixExpression:
		c.run = compileInfixChain(node)
	case *ast.FunctionLiteral:
		c.run = compileFunction(node)
	case *ast.CallExpression:
		c.run = compileCall(node)
	case *ast.ArrayLiteral:
		c.run = compileArray(node)
	case *ast.IndexExpression:
		c.run = compileIndex(node)
	case *ast.HashLiteral:
		c.run = compileHash(node)
	case *ast.ForExpression:
		c.run = compileFor(node)
	case *ast.MatchExpression:
		c.run = compileMatch(node)
	case *ast.ScopeExpression:
		c.run = compileScope(node)
	default:
		// Any other node type (spawn, await) runs the tree-walker's case.
		return walked(node)
	}
	return c
}

// compileAll compiles each of nodes.
func compileAll[T ast.Node](nodes []T) []*code {
	cs := make([]*code, len(nodes))
	for i, n := range nodes {
		cs[i] = compile(n)
	}
	return cs
}

type runFunc = func(in *Interpreter, env *value.Environment) value.Object

func compileProgram(node *ast.Program) runFunc {
	stmts := compileAll(node.Statements)
	return func(in *Interpreter, env *value.Environment) value.Object {
		var result value.Object = NIL
		for _, s := range stmts {
			result = in.exec(s, env)
			switch r := result.(type) {
			case *value.ReturnValue:
				return r.Value
			case *value.Error:
				return r
			}
		}
		return result
	}
}

func compileExpressionStatement(node *ast.ExpressionStatement) runFunc {
	e := compile(node.Expression)
	return func(in *Interpreter, env *value.Environment) value.Object { return in.exec(e, env) }
}

// compileBlockRun is evalBlockStatement: the block's statements, without
// the block's own Eval (evalScope runs a body that way).
func compileBlockRun(node *ast.BlockStatement) runFunc {
	stmts := compileAll(node.Statements)
	return func(in *Interpreter, env *value.Environment) value.Object {
		var result value.Object = NIL
		for _, s := range stmts {
			result = in.exec(s, env)
			if rt := result.Type(); rt == value.RETURN_VALUE_VAL || rt == value.ERROR_VAL {
				return result
			}
		}
		return result
	}
}

func compileReturn(node *ast.ReturnStatement) runFunc {
	e := compile(node.ReturnValue)
	return func(in *Interpreter, env *value.Environment) value.Object {
		val := in.exec(e, env)
		if abrupt(val) {
			return val
		}
		return &value.ReturnValue{Value: val}
	}
}

func compilePrefix(node *ast.PrefixExpression) runFunc {
	right, op := compile(node.Right), node.Operator
	return func(in *Interpreter, env *value.Environment) value.Object {
		r := in.exec(right, env)
		if abrupt(r) {
			return r
		}
		return evalPrefixExpression(op, r)
	}
}

func compileIf(node *ast.IfExpression) runFunc {
	cond, yes := compile(node.Condition), compile(node.Consequence)
	var no *code
	if node.Alternative != nil {
		no = compile(node.Alternative)
	}
	return func(in *Interpreter, env *value.Environment) value.Object {
		condition := in.exec(cond, env)
		if abrupt(condition) { // as evalIfExpression
			return condition
		}
		if condition == TRUE {
			return in.exec(yes, env)
		} else if no != nil {
			return in.exec(no, env)
		}
		return NIL
	}
}

func compileLet(node *ast.LetStatement) runFunc {
	val, name := compile(node.Value), node.Name.Value
	return func(in *Interpreter, env *value.Environment) value.Object {
		v := in.exec(val, env)
		if abrupt(v) {
			return v
		}
		if e := in.charge(bindingSize); e != nil {
			return e
		}
		env.Set(name, v)
		return NIL
	}
}

func compileIdentifier(node *ast.Identifier) runFunc {
	return func(in *Interpreter, env *value.Environment) value.Object {
		return evalIdentifier(node, env, in.Task)
	}
}
