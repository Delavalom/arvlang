package evaluator

import (
	"context"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)

// pollEvery is how many nodes a compile turns into code between looks at
// its context (A7).
const pollEvery = 1024

// compiler is one compile of a program. Every pollEvery nodes it looks at
// ctx, and once ctx is done it stops (abandon): compiling a program of
// megabytes takes most of a second, and a cancelled or timed-out run must
// not wait for it.
type compiler struct {
	ctx   context.Context
	nodes int
}

// abandoned is the panic that unwinds a compile whose context is done;
// compileCtx recovers it.
type abandoned struct{}

// compileCtx compiles node, looking at ctx as it goes. It returns false
// if ctx was done before the compile finished, and then no code.
func compileCtx(ctx context.Context, node ast.Node) (c *code, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, is := r.(abandoned); !is {
				panic(r)
			}
			c, ok = nil, false
		}
	}()
	return (&compiler{ctx: ctx}).compile(node), true
}

// compile compiles node with no context to look at (for tests).
func compile(node ast.Node) *code { return (&compiler{}).compile(node) }

// compile turns node and everything under it into code. Each case does
// what eval's case for the node type does, with its children compiled
// once here instead of dispatched on every evaluation. A node type with
// no case here runs on the tree-walker: its code calls eval's case
// itself, after the same checks (enter), so it costs the same.
func (cc *compiler) compile(node ast.Node) *code {
	if cc.nodes++; cc.nodes%pollEvery == 0 && cc.ctx != nil && cc.ctx.Err() != nil {
		panic(abandoned{})
	}
	c := &code{node: node}
	switch node := node.(type) {
	case *ast.Program:
		c.run = cc.compileProgram(node)
	case *ast.ExpressionStatement:
		c.run = cc.compileExpressionStatement(node)
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
		c.run = cc.compileReturn(node)
	case *ast.PrefixExpression:
		c.run = cc.compilePrefix(node)
	case *ast.BlockStatement:
		c.run = cc.compileBlockRun(node)
	case *ast.IfExpression:
		c.run = cc.compileIf(node)
	case *ast.LetStatement:
		c.run = cc.compileLet(node)
	case *ast.Identifier:
		c.run = cc.compileIdentifier(node)
	case *ast.InfixExpression:
		c.run = cc.compileInfixChain(node)
	case *ast.FunctionLiteral:
		c.run = cc.compileFunction(node)
	case *ast.CallExpression:
		c.run = cc.compileCall(node)
	case *ast.ArrayLiteral:
		c.run = cc.compileArray(node)
	case *ast.IndexExpression:
		c.run = cc.compileIndex(node)
	case *ast.HashLiteral:
		c.run = cc.compileHash(node)
	case *ast.ForExpression:
		c.run = cc.compileFor(node)
	case *ast.MatchExpression:
		c.run = cc.compileMatch(node)
	case *ast.ScopeExpression:
		c.run = cc.compileScope(node)
	default:
		// Any other node type (spawn, await) runs the tree-walker's case.
		return walked(node)
	}
	return c
}

// compileAll compiles each of nodes.
func compileAll[T ast.Node](cc *compiler, nodes []T) []*code {
	cs := make([]*code, len(nodes))
	for i, n := range nodes {
		cs[i] = cc.compile(n)
	}
	return cs
}

type runFunc = func(in *Interpreter, env *value.Environment) value.Object

func (cc *compiler) compileProgram(node *ast.Program) runFunc {
	stmts := compileAll(cc, node.Statements)
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

func (cc *compiler) compileExpressionStatement(node *ast.ExpressionStatement) runFunc {
	e := cc.compile(node.Expression)
	return func(in *Interpreter, env *value.Environment) value.Object { return in.exec(e, env) }
}

// compileBlockRun is evalBlockStatement: the block's statements, without
// the block's own Eval (evalScope runs a body that way).
func (cc *compiler) compileBlockRun(node *ast.BlockStatement) runFunc {
	stmts := compileAll(cc, node.Statements)
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

func (cc *compiler) compileReturn(node *ast.ReturnStatement) runFunc {
	e := cc.compile(node.ReturnValue)
	return func(in *Interpreter, env *value.Environment) value.Object {
		val := in.exec(e, env)
		if abrupt(val) {
			return val
		}
		return &value.ReturnValue{Value: val}
	}
}

func (cc *compiler) compilePrefix(node *ast.PrefixExpression) runFunc {
	right, op := cc.compile(node.Right), node.Operator
	return func(in *Interpreter, env *value.Environment) value.Object {
		r := in.exec(right, env)
		if abrupt(r) {
			return r
		}
		return evalPrefixExpression(op, r)
	}
}

func (cc *compiler) compileIf(node *ast.IfExpression) runFunc {
	cond, yes := cc.compile(node.Condition), cc.compile(node.Consequence)
	var no *code
	if node.Alternative != nil {
		no = cc.compile(node.Alternative)
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

func (cc *compiler) compileLet(node *ast.LetStatement) runFunc {
	val, name := cc.compile(node.Value), node.Name.Value
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

func (cc *compiler) compileIdentifier(node *ast.Identifier) runFunc {
	return func(in *Interpreter, env *value.Environment) value.Object {
		return evalIdentifier(node, env, in.Task)
	}
}
