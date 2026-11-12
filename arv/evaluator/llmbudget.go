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
// end where they ended. A resume's live steps wait for the logged ones
// (runtime.AwaitReservations, budget_llm_order.go).
const effectReserve = runtime.KindReserve

// reserve is b.Reserve for one model call, through the log when the
// outcome can depend on other tasks. In a resume, a live reservation
// waits until every logged one has been acted out, and a replayed grant
// that is its task's last record, whose call is then sent live, must be
// one the pools can cover (runtime.AwaitReservations).
func (in *Interpreter) reserve(b *runtime.Budget, tokens, usdMicros int64) (*runtime.Reservation, *value.Error) {
	t := in.Task
	if t.Log() == nil || !b.LimitsModels() {
		r, err := b.Reserve(tokens, usdMicros)
		if err != nil {
			return nil, errorOf(err, runtime.KindBudget)
		}
		return r, nil
	}
	var r *runtime.Reservation
	ran := false
	v := in.effect(effectReserve, amounts(tokens, usdMicros), func(func(int64) error) value.Object {
		ran = true
		if err := t.AwaitReservations(nil); err != nil {
			return errorOf(err, runtime.KindReplay)
		}
		got, err := b.Reserve(tokens, usdMicros)
		if err != nil {
			return errorOf(err, runtime.KindBudget)
		}
		r = got
		return NIL
	})
	switch v := v.(type) {
	case *value.Error:
		if !ran {
			t.ReplayedRefusal()
		}
		return nil, v
	case *value.Nil:
	default:
		return nil, errorOf(t.Log().Diverge("at task %s: the log's %s is %s, not nil or an error", t.ID, effectReserve, v.Type()), runtime.KindReplay)
	}
	if ran {
		return r, nil
	}
	// The recorded run's reservation was granted. If it is t's last
	// record, the call after it is sent live.
	r = t.ReplayedGrant(b, tokens, usdMicros)
	if t.Log().Mode() == runtime.Resume && !t.FromLog() {
		if err := t.AwaitReservations(r); err != nil {
			return nil, errorOf(err, runtime.KindReplay)
		}
		if err := b.Overdrawn(); err != nil {
			return nil, errorOf(t.Log().Diverge("at task %s: the log grants a reservation of %d tokens and %d micro-dollars, which the budget cannot cover (%s); the call after it would be sent live",
				t.ID, tokens, usdMicros, err), runtime.KindReplay)
		}
	}
	return r, nil
}

// amounts is a reservation's or a settlement's request: its tokens and
// micro-dollars.
func amounts(tokens, usdMicros int64) []value.Object {
	return []value.Object{&value.Integer{Value: tokens}, &value.Integer{Value: usdMicros}}
}

// A settlement within the reservation only gives back and cannot fail.
// One past it (the provider counted more than the estimate) charges the
// excess to every level, and fails if a level goes past its limit: again
// a matter of what other tasks spent first. With a log and a pool that
// limits it, it is the effect "llm_settle" (request: the tokens and
// micro-dollars used). A replay makes the settlement, so the pools end
// where they ended, and returns the logged outcome, not its own.
const effectSettle = "llm_settle"

// settle is r.Settle for a call that used tokens and usdMicros, through
// the log when its outcome can depend on other tasks.
func (in *Interpreter) settle(b *runtime.Budget, r *runtime.Reservation, tokens, usdMicros int64) *value.Error {
	if in.Task.Log() == nil || !b.LimitsModels() || r.Covers(tokens, usdMicros) {
		if err := r.Settle(tokens, usdMicros); err != nil {
			return errorOf(err, runtime.KindBudget)
		}
		return nil
	}
	ran := false
	v := in.effect(effectSettle, amounts(tokens, usdMicros), func(func(int64) error) value.Object {
		ran = true
		if err := in.Task.AwaitReservations(r); err != nil {
			r.Settle(tokens, usdMicros)
			return errorOf(err, runtime.KindReplay)
		}
		if err := r.Settle(tokens, usdMicros); err != nil {
			return errorOf(err, runtime.KindBudget)
		}
		return NIL
	})
	if !ran { // replayed: the outcome is the log's
		_, failed := v.(*value.Error)
		r.SettleLogged(tokens, usdMicros, failed)
	}
	switch v := v.(type) {
	case *value.Error:
		return v
	case *value.Nil:
		return nil
	}
	return errorOf(in.Task.Log().Diverge("at task %s: the log's %s is %s, not nil or an error", in.Task.ID, effectSettle, v.Type()), runtime.KindReplay)
}
