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

// hostGate runs do, which calls a host effect's Fn, once the calling
// task's grants have admitted it: the Phase 6 effect gate (I7) for host
// effects, logHost. A test replaces it. It wraps Fn alone: what it
// returns, whether do's or a logged value on replay, goes through
// hostResult (checked, normalised, charged) either way, so a replayed
// run's budgets match the recorded run's.
var hostGate = logHost

// logHost is hostGate: with no log it just runs do. With one, the call is
// the effect "host:<name>" (a host's name cannot hold ':', so it never
// clashes with a built-in effect or a record kind), its request the
// arguments as value.EncodeRequest writes them (a function or capability
// argument as its type alone). Recording, it logs what the call came to
// as an Arv value: the error, as hostResult would make it (readError's
// look at cancellation is live, so it is made here, inside the effect),
// or the result as Arv data (hostValue). Replaying, it runs nothing.
// Either way it returns the logged value, decoded. An error comes back as a loggedHostError,
// which hostResult returns as it is.
func logHost(t *runtime.Task, name string, args []value.Object, do func() (value.Object, error)) (value.Object, error) {
	l := t.Log()
	if l == nil {
		return do()
	}
	var out value.Object
	o, err := t.Effect("host:"+name, value.EncodeRequest(args), func() runtime.Outcome {
		v, ferr := do()
		if ev, ok := v.(*value.Error); ok && ev != nil && ferr == nil {
			ferr = errors.New(ev.Message)
		}
		switch {
		case ferr != nil:
			out = clipErr(readError(t, name, ferr))
		default:
			data, _, werr := hostValue(v)
			if werr != nil {
				out = newError("%s: %s", name, werr)
			} else {
				out = data
			}
		}
		res, eerr := value.Encode(out)
		if eerr != nil {
			out = newError("%s: cannot log its result: %s", name, eerr)
			res, _ = value.Encode(out)
		}
		e, failed := out.(*value.Error)
		return runtime.Outcome{Res: res, Cancelled: failed && e.ErrKind() == runtime.KindCancelled}
	})
	if err != nil {
		return nil, &loggedHostError{errorOf(err, runtime.KindReplay)}
	}
	// A recording goes on with the result as the log has it, decoded, as a
	// replay does, never with the host's own object: Fn may return one of
	// its arguments or a value it returned before, and a later request
	// that holds both it and the program's object would then encode a
	// shared value ({"ref":n}) where the replay's fresh copy encodes two.
	v, derr := value.Decode(o.Res)
	if derr != nil {
		return nil, &loggedHostError{errorOf(l.Diverge("at task %s: the log's result for %s does not decode: %s", t.ID, name, derr), runtime.KindReplay)}
	}
	out = v
	if e, ok := out.(*value.Error); ok {
		return nil, &loggedHostError{e}
	}
	return out, nil
}

// loggedHostError is a host call's error as logHost logged it: already
// the Arv error the call came to.
type loggedHostError struct{ v *value.Error }

func (e *loggedHostError) Error() string { return e.v.Message }

// hostEffect is the builtin that runs e as name. applyFunction has already
// charged the steps that led here and checked that the task may go on; it
// finds the call's scope, admits it against the task's grants, checks the
// task again (Scope and Covers may have been slow), and only then calls
// Fn, through hostGate.
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
		// Scope and Covers are host code and may be slow: a task cancelled
		// while they ran (its scope timed out) must not reach Fn.
		if err := t.Err(); err != nil {
			return newErrorKind(runtime.KindCancelled, "task %s cancelled: %s", t.ID, err)
		}
		v, err := hostGate(t, name, args, func() (value.Object, error) { return e.Fn(t, args...) })
		return hostResult(t, name, v, err, charge)
	}
}

// hostResult turns what Fn returned into an Arv value: an error (or an
// Arv error value) as readError reports a failed read, cut to maxHostErr
// bytes, a result that is not data as a runtime error, and data as
// itself. The walk (hostWalk) charges the result as it goes and stops
// once the task is cancelled or the charge fails.
func hostResult(t *runtime.Task, name string, v value.Object, err error, charge func(int64) error) value.Object {
	var logged *loggedHostError
	if errors.As(err, &logged) {
		return logged.v
	}
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
