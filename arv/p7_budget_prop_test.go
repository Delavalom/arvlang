package arv

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// I6 across model calls, through the language: random sequences of ask
// and agent calls inside random nests of tokens, usd_cents and max_turns
// scopes (the run's own limits as the outermost level, sometimes).
//
//   - What the provider reports spending never passes the smallest tokens
//     or usd_cents limit in the nest, and no agent makes more turns than
//     the smallest max_turns.
//   - The nest behaves exactly like one scope holding, per kind, its
//     smallest limit: same outputs, same errors, same provider calls. A
//     level can only take away, never add.
//   - Adding an innermost scope with limits far above every other
//     changes nothing.

// level is one scope's model limits; 0 is none.
type level struct{ tokens, usd, turns int64 }

func (l level) options() string {
	var o []string
	if l.tokens > 0 {
		o = append(o, fmt.Sprintf("tokens: %d", l.tokens))
	}
	if l.usd > 0 {
		o = append(o, fmt.Sprintf("usd_cents: %d", l.usd))
	}
	if l.turns > 0 {
		o = append(o, fmt.Sprintf("max_turns: %d", l.turns))
	}
	if len(o) == 0 {
		return ""
	}
	return "(" + strings.Join(o, ", ") + ") "
}

func minLimit(a, b int64) int64 {
	if a == 0 || (b > 0 && b < a) {
		return b
	}
	return a
}

// modelCall is one call of the sequence: an ask with a prompt, or an
// agent whose model calls its tool turns times before answering.
type modelCall struct {
	agent  bool
	prompt string
	turns  int
}

func (c modelCall) src() string {
	if c.agent {
		return fmt.Sprintf(`agent(sonnet, "g%d", {"t": fn() { "x" }})`, c.turns)
	}
	return fmt.Sprintf(`ask(haiku, %q)`, c.prompt)
}

const callPrelude = `let haiku = "anthropic:claude-haiku-5-5";
let sonnet = "anthropic:claude-sonnet-5-5";
`

// program nests calls in levels, outermost first, printing each call's
// text or error message.
func program(levels []level, calls []modelCall) string {
	var b strings.Builder
	b.WriteString(callPrelude)
	for _, l := range levels {
		b.WriteString("scope " + l.options() + "{\n")
	}
	for _, c := range calls {
		fmt.Fprintf(&b, "puts(match %s { ok(v) => v, err(e) => e[\"kind\"] + \": \" + e[\"message\"] });\n", c.src())
	}
	b.WriteString("1")
	for range levels {
		b.WriteString("\n}")
	}
	return b.String()
}

// scriptedModel answers deterministically from seed and the call's
// position: random usage below any reservation, and for an agent "g<k>"
// a tool call until it has made k turns.
func scriptedModel(seed int64) *llm.Fake {
	rng := rand.New(rand.NewSource(seed))
	f := llm.NewFake()
	f.Then = func(ctx context.Context, req llm.Request) (llm.Response, error) {
		in, out := 1+rng.Intn(300), 1+rng.Intn(300)
		if len(req.Tools) > 0 {
			goal := req.Messages[0].Content[0].Text
			k, _ := strconv.Atoi(strings.TrimPrefix(goal, "g"))
			if turn := (len(req.Messages) + 1) / 2; turn < k {
				return llm.ToolUseReply(in, out, llm.ToolUse("u"+strconv.Itoa(turn), "t", `{}`)).Response, nil
			}
		}
		return llm.TextReply("answer", in, out).Response, nil
	}
	return f
}

// runCalls runs a program under root limits with a fresh scriptedModel
// and returns its output and the requests the model received.
func runCalls(t *testing.T, src string, root level, seed int64) (string, []llm.Request) {
	t.Helper()
	f := scriptedModel(seed)
	var out bytes.Buffer
	_, err := Run(src, Options{
		Out:    &out,
		Caps:   []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}, {Kind: runtime.Tool, Scope: "*"}},
		LLM:    &llm.Host{Providers: map[string]llm.Provider{"anthropic": f}},
		Limits: runtime.Limits{Tokens: root.tokens, USDCents: root.usd, MaxTurns: int(root.turns)},
	})
	if err != nil {
		t.Fatalf("run: %v\n%s", err, src)
	}
	return out.String(), f.Calls()
}

func TestModelBudgetsOnlyShrinkThroughCallsProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	randLevel := func() level {
		var l level
		if rng.Intn(2) == 0 {
			l.tokens = 3000 + rng.Int63n(40000)
		}
		if rng.Intn(3) == 0 {
			l.usd = 1 + rng.Int63n(30)
		}
		if rng.Intn(3) == 0 {
			l.turns = 1 + rng.Int63n(6)
		}
		return l
	}
	rounds := 120
	if testing.Short() {
		rounds = 30
	}
	refused := 0
	for round := 0; round < rounds; round++ {
		var root level
		if rng.Intn(2) == 0 {
			root = randLevel()
		}
		levels := make([]level, rng.Intn(4))
		for i := range levels {
			levels[i] = randLevel()
		}
		calls := make([]modelCall, 2+rng.Intn(9))
		for i := range calls {
			calls[i] = modelCall{agent: rng.Intn(3) == 0, prompt: strings.Repeat("p", rng.Intn(3000)), turns: 1 + rng.Intn(7)}
		}
		tight := root
		for _, l := range levels {
			tight = level{minLimit(tight.tokens, l.tokens), minLimit(tight.usd, l.usd), minLimit(tight.turns, l.turns)}
		}
		seed := rng.Int63()
		src := program(levels, calls)
		out, reqs := runCalls(t, src, root, seed)
		refused += strings.Count(out, "budget exceeded")

		// What the provider reports spending stays within the tightest limits.
		var tokens, micros int64
		turns, maxTurns := 0, 0
		for i, r := range reqs {
			p := llm.DefaultPrices["anthropic:"+r.Model]
			usage := scriptedUsage(seed, i)
			tokens += int64(usage[0] + usage[1])
			micros += p.Cost(usage[0], usage[1])
			if len(r.Messages) == 1 {
				turns = 0
			}
			turns++
			maxTurns = max(maxTurns, turns)
		}
		if tight.tokens > 0 && tokens > tight.tokens {
			t.Fatalf("round %d: %d tokens spent under a tokens limit of %d\n%s", round, tokens, tight.tokens, src)
		}
		if tight.usd > 0 && micros > tight.usd*10_000 {
			t.Fatalf("round %d: %d micro-dollars spent under usd_cents %d\n%s", round, micros, tight.usd, src)
		}
		if limit := cmpOr(tight.turns, runtime.DefaultMaxTurns); int64(maxTurns) > limit {
			t.Fatalf("round %d: an agent made %d turns under max_turns %d\n%s", round, maxTurns, limit, src)
		}

		// The nest is its tightest level, and a far larger level adds nothing.
		flat, _ := runCalls(t, program([]level{tight}, calls), level{}, seed)
		if flat != out {
			t.Fatalf("round %d: the nest and its tightest level differ\nnest:\n%s\noutput:\n%s\nflat %+v output:\n%s", round, src, out, tight, flat)
		}
		huge := append(append([]level(nil), levels...), level{1 << 40, runtime.MaxUSDCents, 10000})
		bigger, _ := runCalls(t, program(huge, calls), root, seed)
		if bigger != out {
			t.Fatalf("round %d: a larger inner level changed the run\nnest:\n%s\noutput:\n%s\nwith it:\n%s", round, src, out, bigger)
		}
	}
	if refused == 0 {
		t.Fatal("no call was ever refused: the limits are too loose to test anything")
	}
}

// scriptedUsage is the usage scriptedModel(seed) reports for its i-th call.
func scriptedUsage(seed int64, i int) [2]int {
	rng := rand.New(rand.NewSource(seed))
	var u [2]int
	for j := 0; j <= i; j++ {
		u = [2]int{1 + rng.Intn(300), 1 + rng.Intn(300)}
	}
	return u
}

func cmpOr(a int64, b int) int64 {
	if a > 0 {
		return a
	}
	return int64(b)
}
