package evaluator

import (
	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/value"
)

// evalMatch evaluates match <subject> { ok(v) => ..., err(e) => ... }.
// The subject is the one place an error value does not stop evaluation:
// an error runs the err arm, with e bound to a hash of its "kind",
// "message", "task" and "pos"; any other value runs the ok arm, with v
// bound to it. Each arm runs in a new environment enclosing env, so the
// name does not leak out. A return in the subject passes through.
//
// An error is not caught once the matching task itself has been cancelled
// (its scope failed or timed out): match must not let a cancelled task go
// on. So in a fail_fast scope, match cannot rescue a task's failure; the
// scope fails with it anyway (I5). Use supervise: "collect" for that.
func (in *Interpreter) evalMatch(node *ast.MatchExpression, env *value.Environment) value.Object {
	subject := in.Eval(node.Subject, env)
	if subject.Type() == value.RETURN_VALUE_VAL {
		return subject
	}
	name, body := node.OkName, node.Ok
	if e, ok := subject.(*value.Error); ok {
		if in.Task.Err() != nil {
			return e
		}
		name, body = node.ErrName, node.Err
		h, errv := in.errorHash(e)
		if errv != nil {
			return errv
		}
		subject = h
	}
	if e := in.charge(envSize + bindingSize); e != nil {
		return e
	}
	armEnv := value.NewEnclosedEnvironment(env)
	armEnv.Set(name.Value, subject)
	return in.Eval(body, armEnv)
}

// errorHash is e as the hash the err arm sees, charged before it is
// built. Its strings share e's, so a user error's long message is not
// copied (it was charged when the program built it).
func (in *Interpreter) errorHash(e *value.Error) (*value.Hash, *value.Error) {
	fields := map[string]string{"kind": e.ErrKind(), "message": e.Message, "task": e.Task, "pos": e.Pos}
	n := int64(len(fields)) * hashPairSize
	for _, v := range fields {
		n += int64(len(v))
	}
	if errv := in.charge(n); errv != nil {
		return nil, errv
	}
	h := make(map[string]value.Object, len(fields))
	for k, v := range fields {
		h[k] = &value.String{Value: v}
	}
	return value.NewHash(h), nil
}
