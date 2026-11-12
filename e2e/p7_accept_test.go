package e2e

import (
	"strings"
	"testing"
)

// The acceptance tests of 14-phase7-agents.md, through the binary and
// the real Anthropic provider code, against a local fake of the Messages
// API. Tool runs are counted by the gate server, so "the tool ran once"
// and "the tool never ran" are seen from outside the process. The replay
// row is TestP67Phase7AcceptanceReplays (Phase 6x7); the live row is
// TestLiveAnthropic, opt-in, never run here.

// agentProg spawns one agent holding grants and prints its answer.
func agentProg(grants string) string {
	return "let m = " + sonnet + ";\n" + weatherTools +
		"puts(scope {\n  await spawn agent(m, \"Should I bike in Lisbon today?\", tools) with [" + grants + "]\n})\n"
}

func TestPhase7Acceptance(t *testing.T) {
	t.Parallel()
	runLLM(t, phase7Cases())
}

// phase7Cases are the design's rows; TestP67Phase7AcceptanceReplays
// records and replays each one.
func phase7Cases() []llmCase {
	denied := `"content":"capability denied: tool(\"notes\")","is_error":true`
	return []llmCase{
		{name: "ask without llm is denied before any request",
			src:   "puts(ask(" + sonnet + ", \"hi\"))",
			args:  []string{"--allow", "net=127.0.0.1"},
			err:   `error: capability denied: llm("anthropic:claude-sonnet-5-5") in task root (holds: [net("127.0.0.1")])`,
			calls: 0},
		{name: "allowed tool runs once with the model's arguments",
			src:    agentProg(`llm(m), tool("weather"), net("127.0.0.1")`),
			args:   []string{"--allow", allowLLM, "--allow", "tool=*", "--allow", "net=127.0.0.1"},
			script: []apiReply{okTools(40, 9, use("t1", "weather", `{"city":"Lisbon"}`)), okText("Yes, bike.", 60, 4)},
			stdout: "Yes, bike.\n", calls: 2,
			verify: all(
				wantArrivals(map[string]int{"w-Lisbon": 1, "notes": 0}),
				sentHas(0, `"name":"notes"`, `"name":"weather"`, `"required":["city"]`, `"model":"claude-sonnet-5-5"`),
				sentHas(1, `"tool_use_id":"t1","content":"sunny in Lisbon"}`, `"name":"weather","input":{"city":"Lisbon"}`),
				sentNone(`"is_error"`))},
		{name: "a tool the task does not hold is an error result and the run goes on",
			src:    agentProg(`llm(m), tool("weather"), net("127.0.0.1")`),
			args:   []string{"--allow", allowLLM, "--allow", "tool=*", "--allow", "net=127.0.0.1"},
			script: []apiReply{okTools(40, 9, use("t1", "notes", `{"path":"/etc/passwd"}`)), okText("I may not read notes.", 60, 4)},
			stdout: "I may not read notes.\n", calls: 2,
			verify: all(
				wantArrivals(map[string]int{"notes": 0}),
				sentHas(1, `"tool_use_id":"t1",`+denied),
				sentNone("holds", "root", "127.0.0.1"))},
		{name: "an allowed tool's own effect is checked",
			src:    agentProg(`llm(m), tool("weather")`),
			args:   []string{"--allow", allowLLM, "--allow", "tool=*", "--allow", "net=127.0.0.1"},
			script: []apiReply{okTools(40, 9, use("t1", "weather", `{"city":"Lisbon"}`)), okText("No forecast.", 60, 4)},
			stdout: "No forecast.\n", calls: 2,
			verify: all(
				wantArrivals(map[string]int{"w-Lisbon": 0}),
				sentHas(1, `"tool_use_id":"t1","content":"capability denied: net","is_error":true`),
				sentNone("holds", "root", "127.0.0.1"))},
		{name: "usd_cents 1: the second call is refused before the provider",
			// 1 input and 19998 output tokens of Haiku 5.5 cost exactly 1 cent
			// (10000 micro-dollars, rounded up), so the
			// first call spends the whole budget.
			src:    "let h = " + haiku + ";\nscope (usd_cents: 1) {\n  puts(ask(h, \"one\"));\n  puts(ask(h, \"two\"))\n}\n",
			args:   []string{"--allow", allowLLM},
			script: []apiReply{okText("first", 1, 19998), okText("second", 1, 1)},
			stdout: "first\n", err: "error: budget exceeded: usd_cents (limit 1) (at ", calls: 1},
		{name: "usd_cents from the CLI",
			src:    "let h = " + haiku + ";\nputs(ask(h, \"one\"));\nputs(ask(h, \"two\"))\n",
			args:   []string{"--usd-cents", "1", "--allow", allowLLM},
			script: []apiReply{okText("first", 1, 19998), okText("second", 1, 1)},
			stdout: "first\n", err: "error: budget exceeded: usd_cents (limit 1) (at ", calls: 1},
		{name: "max_turns 3 stops a model that always calls tools",
			src: "let m = " + sonnet + ";\n" + weatherTools +
				"let r = scope (max_turns: 3) {\n  match agent(m, \"loop\", tools) { ok(v) => v, err(e) => e[\"kind\"] + \": \" + e[\"message\"] }\n};\nputs(r)\n",
			args:   []string{"--allow", allowLLM, "--allow", "tool=weather", "--allow", "net=127.0.0.1"},
			script: []apiReply{okTools(40, 9, use("t", "weather", `{"city":"Lisbon"}`))},
			stdout: "budget: budget exceeded: max_turns (limit 3)\n", calls: 3,
			verify: wantArrivals(map[string]int{"w-Lisbon": 3})},
		{name: "max_turns from the CLI",
			src:    "let m = " + sonnet + ";\n" + weatherTools + "puts(agent(m, \"loop\", tools))\n",
			args:   []string{"--max-turns", "3", "--allow", allowLLM, "--allow", "tool=weather", "--allow", "net=127.0.0.1"},
			script: []apiReply{okTools(40, 9, use("t", "weather", `{"city":"Lisbon"}`))},
			err:    "error: budget exceeded: max_turns (limit 3) (at ", calls: 3},
		{name: "the default max_turns is 20",
			src:    "let m = " + sonnet + ";\nputs(agent(m, \"loop\", {\"t\": fn() { \"x\" }}))\n",
			args:   []string{"--allow", allowLLM, "--allow", "tool=t"},
			script: []apiReply{okTools(40, 9, use("t", "t", `{}`))},
			err:    "error: budget exceeded: max_turns (limit 20) (at ", calls: 20},
	}
}

// The design's own example program: the agent holds tool("weather") only,
// so the model asking for both tools gets weather's forecast and a denial
// for notes, in one user message, in the order it asked.
func TestPhase7DesignExample(t *testing.T) {
	t.Parallel()
	runLLMCase(t, llmCase{
		src: weatherTools + `let answer = scope (tokens: 50000, usd_cents: 25, timeout_ms: 60000) {
  await spawn agent("anthropic:claude-sonnet-5-5", "Should I bike to work in Lisbon today?", tools)
    with [llm("anthropic:claude-sonnet-5-5"), tool("weather"), net("127.0.0.1")]
};
puts(answer)
`,
		args: []string{"--allow", allowLLM, "--allow", "tool=*", "--allow", "net=127.0.0.1"},
		script: []apiReply{
			okTools(300, 40, use("n", "notes", `{"path":"/etc/hosts"}`), use("w", "weather", `{"city":"Lisbon"}`)),
			okText("Yes: sunny in Lisbon.", 400, 10)},
		stdout: "Yes: sunny in Lisbon.\n", calls: 2,
		verify: func(t *testing.T, res result, api *llmAPI) {
			wantArrivals(map[string]int{"w-Lisbon": 1, "notes": 0})(t, res, api)
			second := api.requests()[1]
			n := strings.Index(second, `"tool_use_id":"n","content":"capability denied: tool(\"notes\")","is_error":true`)
			w := strings.Index(second, `"tool_use_id":"w","content":"sunny in Lisbon"`)
			if n < 0 || w < n {
				t.Fatalf("want the notes denial, then weather's result, in the second request:\n%s", second)
			}
			if strings.Count(second, `"role":"user"`) != 2 {
				t.Fatalf("want both results in one user message after the goal:\n%s", second)
			}
		},
	})
}
