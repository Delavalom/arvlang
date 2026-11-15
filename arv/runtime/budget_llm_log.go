package runtime

// LimitsModels reports whether some level at or above b limits tokens or
// usd_cents. Only then can a model call's reservation be refused, or its
// settlement fail, and only then does that depend on what other tasks
// reserved first.
func (b *Budget) LimitsModels() bool { return b.tokensAt != nil || b.usdAt != nil }

// ReserveLogged is Reserve for a replayed call whose recorded reservation
// was granted: it sets aside tokens and usdMicros at every level from b up
// and never refuses. A replay runs tasks in its own order, so a level can
// hold less here, for a while, than it held when the recorded run
// reserved; once every logged reservation and settlement has been acted
// out, each level holds what it held in the recorded run.
func (b *Budget) ReserveLogged(tokens, usdMicros int64) *Reservation {
	b.ledger.RLock()
	defer b.ledger.RUnlock()
	tokens, usdMicros = max(tokens, 0), max(usdMicros, 0)
	tripped("tokens", b.tokensAt, tokens, (*Budget).tokensPool)
	tripped("usd_cents", b.usdAt, usdMicros, (*Budget).usdPool)
	return &Reservation{b: b, tokens: tokens, usd: usdMicros}
}

// Covers reports whether a call that used tokens and usdMicros stayed
// within r: settling it then only gives back, and cannot fail.
func (r *Reservation) Covers(tokens, usdMicros int64) bool {
	return max(tokens, 0) <= r.tokens && max(usdMicros, 0) <= r.usd
}

// SettleLogged is Settle for a replayed settlement past r. It makes the
// settlement, so the pools end where they ended, whatever its outcome in
// this run's order, and counts the excess as a failed settlement's
// (Overdrawn) when the recorded one failed, and only then.
func (r *Reservation) SettleLogged(tokens, usdMicros int64, failed bool) {
	r.settle(tokens, usdMicros, func(error) bool { return failed })
}
