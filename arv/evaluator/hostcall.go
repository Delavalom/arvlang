package evaluator

import (
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// hostEffect is the builtin that runs e as name. applyFunction has already
// charged the steps that led here and checked that the task may go on; it
// finds the call's scope, admits it against the task's grants, and only
// then calls Fn.
func hostEffect(name string, e Effect) value.ChargingFunction {
	return func(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
		scope, err := e.Scope(args)
		if err != nil {
			return newError("%s: %s", name, err)
		}
		if err := runtime.CheckScopeLen(e.Kind, scope); err != nil {
			return newError("%s: %s", name, err)
		}
		if _, err := t.Admit(runtime.Capability{Kind: e.Kind, Scope: scope}, nil); err != nil {
			return errorOf(err, runtime.KindCapability)
		}
		v, err := e.Fn(t, args...)
		return hostResult(t, name, v, err, charge)
	}
}

// hostResult turns what Fn returned into an Arv value: an error as
// readError reports a failed read, a result that is not data as a runtime
// error, and data, charged, as itself.
func hostResult(t *runtime.Task, name string, v value.Object, err error, charge func(int64) error) value.Object {
	if err != nil {
		return readError(t, name, err)
	}
	out, size, err := hostValue(v)
	if err != nil {
		return newError("%s: %s", name, err)
	}
	if charge != nil {
		if err := charge(size); err != nil {
			return errorOf(err, runtime.KindBudget)
		}
	}
	return out
}
