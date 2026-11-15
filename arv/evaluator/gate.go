package evaluator

import (
	"math/rand/v2"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

func init() {
	builtins["now"] = &value.Builtin{Fn: now, Effect: "now"}
	builtins["random"] = &value.Builtin{Fn: random, Effect: "random"}
}

// now() is the wall-clock time in milliseconds since the Unix epoch. It
// needs no capability; it is an effect (I7), so a replay sees the time
// the recorded run saw.
func now(t *runtime.Task, args ...value.Object) value.Object {
	if len(args) != 0 {
		return newError("now: want 0 arguments, got %d", len(args))
	}
	return &value.Integer{Value: time.Now().UnixMilli()}
}

// random(n) is a random integer from 0 to n-1. It needs no capability;
// it is an effect (I7), so a replay draws the same numbers.
func random(t *runtime.Task, args ...value.Object) value.Object {
	if len(args) != 1 {
		return newError("random: want 1 argument, got %d", len(args))
	}
	n, ok := args[0].(*value.Integer)
	if !ok || n.Value < 1 {
		return newError("random: want a positive INTEGER bound")
	}
	return &value.Integer{Value: rand.Int64N(n.Value)}
}

// callEffect calls fn, an effect builtin, through the gate. A plain Fn's
// result is charged after it returns, replayed or not, as for any other
// builtin; a Charging one charges itself (the gate replays its charges).
func (in *Interpreter) callEffect(fn *value.Builtin, args []value.Object) value.Object {
	result := in.effect(fn.Effect, args, func(charge func(int64) error) value.Object {
		if fn.Charging != nil {
			return fn.Charging(in.Task, charge, args...)
		}
		return fn.Fn(in.Task, args...)
	})
	if fn.Charging != nil || fn.Shares || isError(result) {
		return result
	}
	return in.alloc(result, allocSize(result))
}

// effect is the gate every effect goes through (I7). Without a log it
// runs do, which charges what it allocates to charge. With one, the task's
// log records it (runtime.Task.Effect): its arguments, its result, the
// bytes it charged and whether it was cancelled; or a replay answers it
// from the log without running it, charging the recorded bytes and
// decoding the recorded result. A replay that diverges fails with kind
// replay, and the whole run stops.
func (in *Interpreter) effect(kind string, args []value.Object, do func(charge func(int64) error) value.Object) value.Object {
	if in.Task.Log() == nil {
		return do(in.chargeBytes)
	}
	var result value.Object
	o, err := in.Task.Effect(kind, value.EncodeRequest(args), func() runtime.Outcome {
		var charged int64
		result = do(func(n int64) error { charged += n; return in.chargeBytes(n) })
		res, err := value.Encode(result)
		if err != nil { // a bug: no effect returns what the codec refuses
			result = newError("%s: cannot log its result: %s", kind, err)
			res, _ = value.Encode(result)
		}
		e, failed := result.(*value.Error)
		return runtime.Outcome{Res: res, Charged: charged, Cancelled: failed && e.ErrKind() == runtime.KindCancelled}
	})
	if err != nil {
		return errorOf(err, runtime.KindReplay)
	}
	if !o.Replayed {
		return result
	}
	in.chargeBytes(o.Charged) // a failure here was the recorded result's own
	v, err := value.Decode(o.Res)
	if err != nil {
		return errorOf(in.Task.Log().Diverge("at task %s: the log's result for %s does not decode: %s", in.Task.ID, kind, err), runtime.KindReplay)
	}
	return v
}
