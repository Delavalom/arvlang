package runtime

import (
	"context"
	"sync"
)

// KindReserve is the effect kind of a model call's reservation, logged
// when a tokens or usd_cents level limits the call (arv/evaluator,
// llmbudget.go).
const KindReserve = "llm_reserve"

// A resume acts out the logged reservations (KindReserve records) in its
// own order, so until it has acted them all out, a level can hold more
// than the crashed run's did when it crashed. A live reservation, or a
// live settlement past its reservation, made then could be granted where
// the recorded run would have refused it, and a logged grant acted out
// after it is forced through (ReserveLogged): the level ends below zero,
// and more is spent than its limit allows, with no budget error (Phase
// 6x7, review round 1, Critical). Every logged record happened before the
// crash, so before any live step: a live step waits until every logged
// reservation has been acted out (AwaitReservations), as a live failure
// waits for the log to drain (Task.Fail). A logged reservation is acted
// out once it is settled (a granted one: its call's usage is then in the
// pools), once it is replayed (a refused one), or, granted and its task's
// last record, once it is replayed: its call is then sent live.

// modelLog is an EffectLog's count of logged reservations not yet acted
// out (Replay, Resume).
type modelLog struct {
	mu   sync.Mutex
	left int64
	done chan struct{} // closed once left is 0
}

// countModels starts the count at the log's KindReserve records.
func (l *EffectLog) countModels() {
	l.models.done = make(chan struct{})
	for _, recs := range l.log.tasks {
		for i := range recs {
			if recs[i].Kind == KindReserve && recs[i].Check == 0 {
				l.models.left++
			}
		}
	}
	if l.models.left == 0 {
		close(l.models.done)
	}
}

// modelActed counts one logged reservation acted out.
func (l *EffectLog) modelActed() {
	l.models.mu.Lock()
	defer l.models.mu.Unlock()
	if l.models.left > 0 {
		if l.models.left--; l.models.left == 0 {
			close(l.models.done)
		}
	}
}

// waitModels waits until every logged reservation has been acted out, or
// the run is aborted.
func (l *EffectLog) waitModels() error {
	var aborted <-chan struct{}
	if l.abort != nil {
		aborted = l.abort.Done()
	}
	select {
	case <-l.models.done:
		return nil
	default:
	}
	var stopped bool
	l.Park(func() { // a wait for other tasks (stall.go)
		select {
		case <-l.models.done:
		case <-aborted:
			stopped = true
		}
	})
	if stopped {
		return context.Cause(l.abort)
	}
	return nil
}

// ReplayedGrant is ReserveLogged for t's replayed KindReserve record
// that the recorded run granted: the reservation counts as acted out once
// it is settled.
func (t *Task) ReplayedGrant(b *Budget, tokens, usdMicros int64) *Reservation {
	r := b.ReserveLogged(tokens, usdMicros)
	if t.log.counts() {
		r.acted = t.log.modelActed
	}
	return r
}

// ReplayedRefusal counts t's replayed KindReserve record that the
// recorded run refused as acted out.
func (t *Task) ReplayedRefusal() {
	if t.log.counts() {
		t.log.modelActed()
	}
}

// AwaitReservations is for a live model-budget step of t in a resume (a
// reservation, a settlement past one, or a call a replayed grant sends
// live): it counts own, unless nil, as acted out, then waits until every
// logged reservation has been. The wait takes no time on the recorded
// run's clock (idle). It returns why the run was aborted, if it was.
func (t *Task) AwaitReservations(own *Reservation) error {
	if own != nil {
		own.actOut()
	}
	l := t.log
	if l == nil || l.mode != Resume {
		return nil
	}
	return t.idle(l.waitModels)
}

// FromLog reports that the log answers t's next effect: always in a
// replay, in a resume while t has records left, never in a recording.
func (t *Task) FromLog() bool {
	l := t.log
	switch {
	case l == nil || l.mode == Record:
		return false
	case l.mode == Replay:
		return true
	}
	return t.seq < uint64(len(l.log.tasks[t.ID]))
}

// Overdrawn is an *ExceededError for the tokens or usd_cents level at or
// above b that is below zero, or nil if none is. Once every logged
// reservation has been acted out, a level below zero after a replayed
// grant shows a grant the recorded run could not have made, unless a
// settlement past its reservation had spent the level (which fails, and
// is logged).
func (b *Budget) Overdrawn() error {
	if err := tripped("tokens", b.tokensAt, 0, (*Budget).tokensPool); err != nil {
		return err
	}
	return exceeded("usd_cents", tripped("usd_cents", b.usdAt, 0, (*Budget).usdPool))
}

// actOut counts r, a replayed reservation, as acted out, once.
func (r *Reservation) actOut() {
	r.actedOnce.Do(func() {
		if r.acted != nil {
			r.acted()
		}
	})
}
