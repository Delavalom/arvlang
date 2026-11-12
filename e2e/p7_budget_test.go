package e2e

import (
	"strings"
	"testing"
)

// Phase 7 budgets through the binary (I6: budgets only shrink). A call
// reserves its input estimate (its bytes plus 512, 16 per block) and 4096
// output tokens before the request: ask(h, "q") reserves 4625 tokens. The
// reservation is settled to the reported usage after the reply, kept
// whole when the call may have been billed, and freed only when the call
// provably was not processed (a 4xx or 529).

// tryAsk prints each ask's text, or its error's kind and message.
const tryAsk = `let h = "anthropic:claude-haiku-5-5";
let try = fn(p) { match ask(h, p) { ok(v) => v, err(e) => e["kind"] + ": " + e["message"] } };
`

func repeatTry(n int) string {
	return tryAsk + strings.Repeat("puts(try(\"q\"));\n", n)
}

func TestPhase7Budgets(t *testing.T) {
	t.Parallel()
	alwaysTool := []apiReply{okTools(40, 9, use("t", "t", `{}`))}
	toolsT := `{"t": fn() { "x" }}`
	runLLM(t, []llmCase{
		{name: "tokens refuses a call it cannot cover, before the request",
			src: repeatTry(1), args: []string{"--tokens", "1000", "--allow", allowLLM},
			stdout: "budget: budget exceeded: tokens (limit 1000)\n", calls: 0},
		{name: "a larger inner tokens limit does not raise the run's",
			src:  tryAsk + "puts(scope (tokens: 1000000) { try(\"q\") })\n",
			args: []string{"--tokens", "1000", "--allow", allowLLM},
			// The run's level is the one short, so its limit is named.
			stdout: "budget: budget exceeded: tokens (limit 1000)\n", calls: 0},
		{name: "a larger inner usd_cents limit does not raise the run's",
			src:    "puts(scope (usd_cents: 500) { match ask(" + sonnet + ", \"q\") { ok(v) => v, err(e) => e[\"message\"] } })\n",
			args:   []string{"--usd-cents", "1", "--allow", allowLLM},
			stdout: "budget exceeded: usd_cents (limit 1)\n", calls: 0},
		{name: "a smaller inner tokens limit binds inside it only",
			src:    tryAsk + "puts(scope (tokens: 1000) { try(\"q\") });\nputs(try(\"q\"))\n",
			args:   []string{"--tokens", "100000", "--allow", allowLLM},
			script: []apiReply{okText("outer", 10, 10)},
			stdout: "budget: budget exceeded: tokens (limit 1000)\nouter\n", calls: 1},
		{name: "settlement frees the unused reservation",
			// 10000 - 3000 - 3000 = 4000 left: the third reservation does not fit.
			src: repeatTry(3), args: []string{"--tokens", "10000", "--allow", allowLLM},
			script: []apiReply{okText("a", 1500, 1500), okText("b", 1500, 1500)},
			stdout: "a\nb\nbudget: budget exceeded: tokens (limit 10000)\n", calls: 2},
		{name: "usage past the limit is charged, and the budget stays spent",
			src: repeatTry(3), args: []string{"--tokens", "6000", "--allow", allowLLM},
			script: []apiReply{okText("a", 3000, 4000)},
			stdout: strings.Repeat("budget: budget exceeded: tokens (limit 6000)\n", 3), calls: 1},
		{name: "a call that may have been billed keeps its reservation",
			// Two 500s keep 4625 each: 750 left, the third is refused.
			src: repeatTry(3), args: []string{"--tokens", "10000", "--allow", allowLLM},
			script: []apiReply{apiError(500, "api_error", "boom")},
			stdout: strings.Repeat("effect: ask: anthropic: server (HTTP 500): api_error: boom\n", 2) +
				"budget: budget exceeded: tokens (limit 10000)\n", calls: 2},
		{name: "a call that was provably not processed costs nothing",
			src: repeatTry(5), args: []string{"--tokens", "10000", "--allow", allowLLM},
			script: []apiReply{apiError(429, "rate_limit_error", "slow down")},
			stdout: strings.Repeat("effect: ask: anthropic: rate_limit (HTTP 429): rate_limit_error: slow down\n", 5), calls: 5},
		{name: "an unreadable 200 is charged in full",
			src: repeatTry(3), args: []string{"--tokens", "10000", "--allow", allowLLM},
			script: []apiReply{okReply(`{"content":[{"type":"text","text":"x"}],"stop_reason":"end_turn"}`)},
			stdout: strings.Repeat("effect: ask: anthropic: decode (HTTP 200): reply has no usage\n", 2) +
				"budget: budget exceeded: tokens (limit 10000)\n", calls: 2},
		{name: "an agent's turns spend one tokens budget",
			// Each turn reserves about 6000 and settles to 4000: two fit in 12000.
			src:    "puts(agent(" + sonnet + ", \"g\", " + toolsT + "))\n",
			args:   []string{"--tokens", "12000", "--allow", allowLLM, "--allow", "tool=t"},
			script: []apiReply{okTools(2000, 2000, use("t", "t", `{}`))},
			err:    "error: budget exceeded: tokens (limit 12000) (at ", calls: 2},
		{name: "a larger inner max_turns does not raise the run's",
			src:    "puts(scope (max_turns: 10) { agent(" + sonnet + ", \"g\", " + toolsT + ") })\n",
			args:   []string{"--max-turns", "2", "--allow", allowLLM, "--allow", "tool=t"},
			script: alwaysTool, err: "error: budget exceeded: max_turns (limit 2) (at ", calls: 2},
		{name: "max_turns counts per agent call, not per scope",
			src: "let a = fn() { match agent(" + sonnet + ", \"g\", " + toolsT + ") { ok(v) => v, err(e) => e[\"message\"] } };\n" +
				"scope (max_turns: 2) { puts(a()); puts(a()) }\n",
			args:   []string{"--allow", allowLLM, "--allow", "tool=t"},
			script: alwaysTool, stdout: strings.Repeat("budget exceeded: max_turns (limit 2)\n", 2), calls: 4},
		{name: "parallel calls share the budget: exactly four of six fit",
			// Each holds 4625 while in flight and settles to 4000. Any four
			// reservations fit in 20000, and no fifth can, whatever the order.
			src: `let h = "anthropic:claude-haiku-5-5";
let r = scope (supervise: "collect") {
  let ts = for i in [1, 2, 3, 4, 5, 6] { spawn ask(h, "q") with [llm(h)] };
  for t in ts { match await t { ok(v) => "ok", err(e) => e["kind"] } }
};
let count = fn(want) {
  let go = fn(i, n) { if (i == len(r)) { n } else { go(i + 1, if (r[i] == want) { n + 1 } else { n }) } };
  go(0, 0)
};
puts(count("ok"), count("budget"))
`,
			args:   []string{"--tokens", "20000", "--allow", allowLLM},
			script: []apiReply{okText("a", 2000, 2000)},
			stdout: "4\n2\n", calls: 4},
	})
}
