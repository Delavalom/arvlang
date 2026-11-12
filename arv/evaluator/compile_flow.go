package evaluator

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)

// compileFor is evalFor: the loop itself is forEach, shared.
func compileFor(node *ast.ForExpression) runFunc {
	iter, body, name := compile(node.Iterable), compile(node.Body), node.Var.Value
	return func(in *Interpreter, env *value.Environment) value.Object {
		return in.forEach(name, in.exec(iter, env), env, func(loopEnv *value.Environment) value.Object {
			return in.exec(body, loopEnv)
		})
	}
}

// compileMatch is evalMatch: the choice of arm is matchArm, shared.
func compileMatch(node *ast.MatchExpression) runFunc {
	subject, ok, err := compile(node.Subject), compile(node.Ok), compile(node.Err)
	return func(in *Interpreter, env *value.Environment) value.Object {
		body, armEnv, done := in.matchArm(node, env, in.exec(subject, env))
		if done != nil {
			return done
		}
		if body == node.Ok {
			return in.exec(ok, armEnv)
		}
		return in.exec(err, armEnv)
	}
}

// compileScope is evalScope: the scope itself is openScope, shared; its
// options run on the tree-walker (once per scope, not per node).
func compileScope(node *ast.ScopeExpression) runFunc {
	body := compileBlockRun(node.Body)
	return func(in *Interpreter, env *value.Environment) value.Object {
		return in.openScope(node, env, func() value.Object { return body(in, env) })
	}
}
