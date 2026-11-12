package llm

import "math"

// Price is what a model charges, in micro-dollars (millionths of a US
// dollar) per million tokens: $4 per million is 4_000_000. Above
// LongAbove input tokens (when it is not 0) a request pays the Long
// prices instead, for input and output alike.
type Price struct {
	Input, Output         int64
	LongAbove             int
	LongInput, LongOutput int64
}

// Cost is what a request using in input and out output tokens costs, in
// micro-dollars, rounded up. Negative counts and prices cost nothing, and
// a cost too large for an int64 is math.MaxInt64 / 1_000_000 or more,
// never a wrapped-around negative.
func (p Price) Cost(in, out int) int64 {
	pin, pout := p.Input, p.Output
	if p.LongAbove > 0 && in > p.LongAbove {
		pin, pout = p.LongInput, p.LongOutput
	}
	sum := addSat(mulSat(int64(in), pin), mulSat(int64(out), pout))
	return sum/1_000_000 + min(sum%1_000_000, 1)
}

// mulSat is a*b for non-negative a and b (a negative one counts as 0),
// saturating at math.MaxInt64.
func mulSat(a, b int64) int64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a > math.MaxInt64/b {
		return math.MaxInt64
	}
	return a * b
}

// addSat is a+b for non-negative a and b, saturating at math.MaxInt64.
func addSat(a, b int64) int64 {
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}

// usd is dollars per million tokens as micro-dollars per million tokens.
func usd(dollars float64) int64 { return int64(dollars*1_000_000 + 0.5) }

// DefaultPrices are Anthropic's first-party API prices, by "provider:model".
//
// Source: the claude-api skill's model table (cached 2026-11-03, which
// mirrors https://platform.claude.com/docs/en/about-claude/pricing),
// copied on 2026-11-11. Update this table and that date together, from
// the pricing page, never from memory. Batch, cache and fast-mode prices
// do not apply: Arv sends plain Messages requests.
var DefaultPrices = map[string]Price{
	"anthropic:claude-fable-5-2":  {Input: usd(8), Output: usd(40)},
	"anthropic:claude-mythos-5-2": {Input: usd(8), Output: usd(40)},
	"anthropic:claude-fable-5-1":  {Input: usd(10), Output: usd(50)},
	"anthropic:claude-mythos-5-1": {Input: usd(10), Output: usd(50)},
	"anthropic:claude-fable-5":    {Input: usd(10), Output: usd(50)},
	"anthropic:claude-opus-5-5":   {Input: usd(4), Output: usd(20)},
	"anthropic:claude-opus-5":     {Input: usd(5), Output: usd(25)},
	"anthropic:claude-opus-4-8":   {Input: usd(5), Output: usd(25)},
	"anthropic:claude-opus-4-7":   {Input: usd(5), Output: usd(25)},
	"anthropic:claude-opus-4-6":   {Input: usd(5), Output: usd(25)},
	"anthropic:claude-sonnet-5-5": {Input: usd(2), Output: usd(10)},
	"anthropic:claude-sonnet-5":   {Input: usd(2), Output: usd(10)},
	"anthropic:claude-sonnet-4-6": {Input: usd(3), Output: usd(15)},
	// Haiku 5.5: $0.10 / $0.50 for prompts up to 100K tokens, $0.50 / $2.50 beyond.
	"anthropic:claude-haiku-5-5": {Input: usd(0.10), Output: usd(0.50), LongAbove: 100_000, LongInput: usd(0.50), LongOutput: usd(2.50)},
	"anthropic:claude-haiku-4-5": {Input: usd(1), Output: usd(5)},
}
