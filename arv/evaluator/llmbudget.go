package evaluator

import (
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// A model call reserves from the tokens and usd_cents pools before it is
// sent, and settles after (complete). When concurrent tasks share a pool,
// whether a reservation fits depends on which task reserved first: that
// is scheduling, which a replay does not reproduce (I7). So when a pool
// limits the call and the run keeps a log, the reservation is the effect
// "llm_reserve" (request: the tokens and micro-dollars), whose result
// says whether it was granted. A replay acts the outcome out: a granted
// reservation is taken whatever the pool holds (Budget.ReserveLogged),
// and a refused one takes nothing and fails with the logged error. Every
// call is then refused where the recorded run refused it, and the pools
// end where they ended.
const effectReserve = "llm_reserve"

// reserve is b.Reserve for one model call, through the log when the
// outcome can depend on other tasks.
func (in *Interpreter) reserve(b *runtime.Budget, tokens, usdMicros int64) (*runtime.Reservation, *value.Error) {
	if in.Task.Log() == nil || !b.LimitsModels() {
		r, err := b.Reserve(tokens, usdMicros)
		if err != nil {
			return nil, errorOf(err, runtime.KindBudget)
		}
		return r, nil
	}
	var r *runtime.Reservation
	v := in.effect(effectReserve, amounts(tokens, usdMicros), func(func(int64) error) value.Object {
		got, err := b.Reserve(tokens, usdMicros)
		if err != nil {
			return errorOf(err, runtime.KindBudget)
		}
		r = got
		return NIL
	})
	switch v := v.(type) {
	case *value.Error:
		return nil, v
	case *value.Nil:
	default:
		return nil, errorOf(in.Task.Log().Diverge("at task %s: the log's %s is %s, not nil or an error", in.Task.ID, effectReserve, v.Type()), runtime.KindReplay)
	}
	if r == nil { // replayed: the recorded run's reservation was granted
		r = b.ReserveLogged(tokens, usdMicros)
	}
	return r, nil
}

// amounts is a reservation's or a settlement's request: its tokens and
// micro-dollars.
func amounts(tokens, usdMicros int64) []value.Object {
	return []value.Object{&value.Integer{Value: tokens}, &value.Integer{Value: usdMicros}}
}
