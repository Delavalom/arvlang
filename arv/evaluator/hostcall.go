package evaluator

import (
	"errors"
	"unicode/utf8"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// maxHostErr caps the message of an error from a host's Scope or Fn:
// error values are not charged to alloc_mb, so builtins keep them small.
const maxHostErr = 1024

// hostGate runs do, a host effect's call, once the calling task's grants
// have admitted it. SEAM(p6): this is where the Phase 6 effect gate goes
// in the 6x7 integration, so record, replay and resume see every host
// call by name and arguments, after the capability check and in its
// place in the task's sequence of effects (see 16-phase82.md).
var hostGate = func(t *runtime.Task, name string, args []value.Object, do func() value.Object) value.Object {
	return do()
}

// hostEffect is the builtin that runs e as name. applyFunction has already
// charged the steps that led here and checked that the task may go on; it
// finds the call's scope, admits it against the task's grants, and only
// then calls Fn, through hostGate.
func hostEffect(name string, e Effect) value.ChargingFunction {
	return func(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
		scope, err := e.Scope(args)
		if err != nil {
			return clipErr(newError("%s: %s", name, err))
		}
		if err := runtime.CheckScopeLen(e.Kind, scope); err != nil {
			return newError("%s: %s", name, err)
		}
		if _, err := t.Admit(runtime.Capability{Kind: e.Kind, Scope: scope}, nil); err != nil {
			return errorOf(err, runtime.KindCapability)
		}
		return hostGate(t, name, args, func() value.Object {
			v, err := e.Fn(t, args...)
			return hostResult(t, name, v, err, charge)
		})
	}
}

// hostResult turns what Fn returned into an Arv value: an error (or an
// Arv error value) as readError reports a failed read, cut to maxHostErr
// bytes, a result that is not data as a runtime error, and data as
// itself. The walk (hostWalk) charges the result as it goes and stops
// once the task is cancelled or the charge fails.
func hostResult(t *runtime.Task, name string, v value.Object, err error, charge func(int64) error) value.Object {
	if ev, ok := v.(*value.Error); ok && ev != nil && err == nil {
		err = errors.New(ev.Message)
	}
	if err != nil {
		return clipErr(readError(t, name, err))
	}
	w := hostWalk{check: func(n int64) *value.Error {
		if err := t.Err(); err != nil {
			return newErrorKind(runtime.KindCancelled, "task %s cancelled: %s", t.ID, err)
		}
		if charge != nil {
			if err := charge(n); err != nil {
				return errorOf(err, runtime.KindBudget)
			}
		}
		return nil
	}}
	out, _, err := w.data(v, 0)
	if err == nil {
		err = w.settle()
	}
	if w.stop != nil {
		return w.stop
	}
	if err != nil {
		return newError("%s: %s", name, err)
	}
	return out
}

// clipErr cuts e's message to at most maxHostErr bytes, at a rune
// boundary, so a host's UTF-8 text stays valid UTF-8.
func clipErr(e *value.Error) *value.Error {
	if len(e.Message) > maxHostErr {
		n := maxHostErr
		for n > 0 && !utf8.RuneStart(e.Message[n]) {
			n--
		}
		e.Message = e.Message[:n] + "..."
	}
	return e
}
