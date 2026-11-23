package evaluator

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)

// compileInfixChain is evalInfixChain: the infix expressions down node's
// left spine, flattened once here. Each spine node below the first costs
// one tick, as there.
func compileInfixChain(node *ast.InfixExpression) runFunc {
	spine := []*ast.InfixExpression{node}
	for {
		next, ok := spine[len(spine)-1].Left.(*ast.InfixExpression)
		if !ok {
			break
		}
		spine = append(spine, next)
	}
	first := compile(spine[len(spine)-1].Left)
	// ops and rights in evaluation order: the innermost operator first.
	ops := make([]string, len(spine))
	rights := make([]*code, len(spine))
	for i := range spine {
		s := spine[len(spine)-1-i]
		ops[i], rights[i] = s.Operator, compile(s.Right)
	}
	ticks := len(spine) - 1
	return func(in *Interpreter, env *value.Environment) value.Object {
		for i := 0; i < ticks; i++ {
			if e := in.tick(); e != nil {
				return e
			}
		}
		left := in.exec(first, env)
		for i := 0; i < len(ops) && !abrupt(left); i++ {
			right := in.exec(rights[i], env)
			if abrupt(right) {
				return right
			}
			if n, ok := concatSize(ops[i], left, right); ok {
				if e := in.charge(n); e != nil {
					return e
				}
			}
			left = evalInfixExpression(ops[i], left, right)
		}
		return left
	}
}

func compileFunction(node *ast.FunctionLiteral) runFunc {
	body := compile(node.Body)
	return func(in *Interpreter, env *value.Environment) value.Object {
		fn := &value.Function{Parameters: node.Parameters, Env: env, Body: node.Body, Code: body}
		return in.alloc(fn, functionSize)
	}
}

// execAll is evalExpressions for compiled code: the values in order, or a
// one-element slice holding the first error.
func (in *Interpreter) execAll(cs []*code, env *value.Environment) []value.Object {
	var result []value.Object
	if len(cs) > 0 {
		result = make([]value.Object, 0, len(cs))
	}
	for _, c := range cs {
		v := in.exec(c, env)
		if abrupt(v) {
			return []value.Object{v}
		}
		result = append(result, v)
	}
	return result
}

func compileCall(node *ast.CallExpression) runFunc {
	fn, args := compile(node.Function), compileAll(node.Arguments)
	return func(in *Interpreter, env *value.Environment) value.Object {
		function := in.exec(fn, env)
		if abrupt(function) {
			return function
		}
		vals := in.execAll(args, env)
		if len(vals) == 1 && abrupt(vals[0]) {
			return vals[0]
		}
		return in.applyFunction(function, vals)
	}
}

func compileArray(node *ast.ArrayLiteral) runFunc {
	elems := compileAll(node.Elements)
	return func(in *Interpreter, env *value.Environment) value.Object {
		elements := in.execAll(elems, env)
		if len(elements) == 1 && abrupt(elements[0]) {
			return elements[0]
		}
		return in.alloc(&value.Array{Elements: elements}, arrayElemSize*int64(len(elements)))
	}
}

func compileIndex(node *ast.IndexExpression) runFunc {
	left, index := compile(node.Left), compile(node.Index)
	return func(in *Interpreter, env *value.Environment) value.Object {
		l := in.exec(left, env)
		if abrupt(l) {
			return l
		}
		i := in.exec(index, env)
		if abrupt(i) {
			return i
		}
		return evalIndexExpression(l, i)
	}
}

// compileHash is evalHashLiteral: keys and values in source order.
func compileHash(node *ast.HashLiteral) runFunc {
	keys := compileAll(node.Keys)
	vals := make([]*code, len(node.Keys))
	for i, k := range node.Keys {
		vals[i] = compile(node.Pairs[k])
	}
	return func(in *Interpreter, env *value.Environment) value.Object {
		h := value.NewHashSize(len(keys))
		for i, kc := range keys {
			key := in.exec(kc, env)
			if abrupt(key) {
				return key
			}
			hashKey, ok := key.(value.Hashable)
			if !ok {
				return newError("unusable as hash key: %s", key.Type())
			}
			val := in.exec(vals[i], env)
			if abrupt(val) {
				return val
			}
			h.Set(hashKey.HashKey(), value.HashPair{Key: key, Value: val})
		}
		return in.alloc(h, hashPairSize*int64(len(h.Pairs)))
	}
}
