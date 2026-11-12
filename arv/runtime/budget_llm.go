package runtime

import (
	"math"
	"sync"
)

// DefaultMaxTurns is how many model calls one agent(...) may make when no
// level sets max_turns.
const DefaultMaxTurns = 20

// MaxUSDCents is the largest usd_cents limit: the pool counts
// micro-dollars, 10_000 to the cent, in an int64.
const MaxUSDCents int64 = math.MaxInt64 / microsPerCent // typed: it overflows a 32-bit int

const microsPerCent = 10_000

// models is a level's share of the model budgets: its own tokens and usd
// pools (nil: no limit at this level), the nearest levels at or above it
// that have one, and the max_turns in force there.
type models struct {
	tokens, usd     *pool
	tokensAt, usdAt *Budget
	turns           int
}

// newModels is the models part of level b, opened under parent with limits l. A
// max_turns can only shrink the enclosing one (I6).
func newModels(b, parent *Budget, l Limits) models {
	m := models{tokens: newPool(l.Tokens), usd: newPool(l.USDCents * microsPerCent), turns: l.MaxTurns}
	if parent != nil {
		m.tokensAt, m.usdAt = parent.tokensAt, parent.usdAt
		if m.turns == 0 || m.turns > parent.turns {
			m.turns = parent.turns
		}
	}
	if m.tokens != nil {
		m.tokensAt = b
	}
	if m.usd != nil {
		m.usdAt = b
	}
	return m
}

// MaxTurns is the max_turns in force at b.
func (b *Budget) MaxTurns() int { return b.turns }

// LimitsUSD reports whether some level at or above b limits usd_cents, so
// a model call there needs a price.
func (b *Budget) LimitsUSD() bool { return b.usdAt != nil }

// Reservation is what Reserve set aside at every level for one model call.
type Reservation struct {
	b           *Budget
	tokens, usd int64
	once        sync.Once
}

// Reserve sets aside tokens and usd micro-dollars at every level from b
// up that limits them, before a model call. If any level cannot cover its
// share, it sets nothing aside and fails with an *ExceededError naming
// that level's limit ("tokens", or "usd_cents" in cents), so the call is
// refused before any request goes out. Settle the reservation once the
// call returns.
//
// Each level is updated atomically, but not all levels at once: two calls
// reserving together near a limit can both be refused where one alone
// would fit. That errs on the side of the limit.
func (b *Budget) Reserve(tokens, usdMicros int64) (*Reservation, error) {
	if err := take(b.tokensAt, max(tokens, 0), "tokens", (*Budget).tokensPool); err != nil {
		return nil, err
	}
	if err := take(b.usdAt, max(usdMicros, 0), "usd_cents", (*Budget).usdPool); err != nil {
		give(b.tokensAt, max(tokens, 0), (*Budget).tokensPool)
		return nil, err
	}
	return &Reservation{b: b, tokens: max(tokens, 0), usd: max(usdMicros, 0)}, nil
}

// Settle replaces the reservation with what the call really used: the
// unused part goes back to every level, and any use beyond the
// reservation is charged to them. That excess can take a level past its
// limit; Settle then fails with an *ExceededError, and the level stays
// spent. Only the first Settle of a reservation counts. Settle(0, 0)
// gives a call that never ran everything back.
func (r *Reservation) Settle(tokens, usdMicros int64) error {
	var err error
	r.once.Do(func() {
		e1 := adjust(r.b.tokensAt, r.tokens, max(tokens, 0), "tokens", (*Budget).tokensPool)
		e2 := adjust(r.b.usdAt, r.usd, max(usdMicros, 0), "usd_cents", (*Budget).usdPool)
		if err = e1; err == nil {
			err = e2
		}
	})
	return err
}

func (b *Budget) tokensPool() (*pool, *Budget) { return b.tokens, b.parent.tokensLevel() }
func (b *Budget) usdPool() (*pool, *Budget)    { return b.usd, b.parent.usdLevel() }

func (b *Budget) tokensLevel() *Budget {
	if b == nil {
		return nil
	}
	return b.tokensAt
}

func (b *Budget) usdLevel() *Budget {
	if b == nil {
		return nil
	}
	return b.usdAt
}

// take subtracts n from the pool of every level from l up, or, if any
// would go below zero, from none, failing with the error for the level
// left furthest short.
func take(l *Budget, n int64, kind string, poolOf func(*Budget) (*pool, *Budget)) error {
	err := exceeded(kind, tripped(kind, l, n, poolOf))
	if err != nil {
		give(l, n, poolOf) // a spent level refuses even n == 0
	}
	return err
}

// give adds n back to the pool of every level from l up.
func give(l *Budget, n int64, poolOf func(*Budget) (*pool, *Budget)) {
	for l != nil {
		p, next := poolOf(l)
		p.left.Add(n)
		l = next
	}
}

// adjust moves every level from l up from having reserved r to having
// spent used.
func adjust(l *Budget, r, used int64, kind string, poolOf func(*Budget) (*pool, *Budget)) error {
	if used <= r {
		give(l, r-used, poolOf)
		return nil
	}
	return exceeded(kind, tripped(kind, l, used-r, poolOf))
}

// exceeded states a usd_cents error's limit in cents, as the scope option
// set it; the pool counts micro-dollars.
func exceeded(kind string, err error) error {
	if e, ok := err.(*ExceededError); ok && kind == "usd_cents" {
		return &ExceededError{Kind: kind, Limit: e.Limit / microsPerCent}
	}
	return err
}
