package e2e

import (
	"strings"
	"testing"
	"time"
)

// Agents are tasks: structured concurrency (I4, I5), timeouts and
// attenuation (I2) apply to them as to any task. Events are ordered by
// the servers (the fake's gate "llm", /wait, /slow), never by sleeps.

// boom waits until n model calls are in flight, then fails.
const boom = `let boom = fn(n) { http_get("{{SERVER}}/wait?id={{ID}}&k=llm&n=" + n); 1 / 0 };
`

func TestPhase7Concurrency(t *testing.T) {
	t.Parallel()
	hang := []apiReply{{hang: true}}
	gone := func(n int) func(*testing.T, result, *llmAPI) {
		return func(t *testing.T, res result, api *llmAPI) { api.waitGone(t, n, 30*time.Second) }
	}
	runLLM(t, []llmCase{
		{name: "a scope timeout cancels a model call in flight",
			src:    "puts(scope (timeout_ms: 300) { ask(" + haiku + ", \"q\") })\n",
			args:   []string{"--allow", allowLLM},
			script: hang, err: "error: budget exceeded: timeout (limit 300 ms) (at ", calls: 1, verify: gone(1)},
		{name: "a failing sibling cancels an agent's call in flight (I5)",
			src: boom + "let m = " + sonnet + ";\nscope {\n  spawn agent(m, \"g\", {}) with [llm(m)];\n" +
				"  spawn boom(\"1\") with [net(\"127.0.0.1\")]\n}\n",
			args:   []string{"--allow", allowLLM, "--allow", "net=127.0.0.1"},
			script: hang, err: "error: division by zero (at ", calls: 1, verify: gone(1)},
		{name: "a failing sibling cancels many parallel calls",
			src: boom + "let h = " + haiku + ";\nscope {\n  for i in [1, 2, 3, 4] { spawn ask(h, \"q\") with [llm(h)] };\n" +
				"  spawn boom(\"4\") with [net(\"127.0.0.1\")]\n}\n",
			args:   []string{"--allow", allowLLM, "--allow", "net=127.0.0.1"},
			script: hang, err: "error: division by zero (at ", calls: 4, verify: gone(4)},
		{name: "a failing sibling cancels an agent's tool in flight, and no turn follows",
			src: "let slow = fn() { http_get(\"{{SERVER}}/slow?id={{ID}}&ms=60000\")[\"body\"] };\n" +
				"let boom = fn() { http_get(\"{{SERVER}}/wait?id={{ID}}&k=slow&n=1\"); 1 / 0 };\n" +
				"let m = " + sonnet + ";\nscope {\n  spawn agent(m, \"g\", {\"slow\": slow}) with [llm(m), tool(\"slow\"), net(\"127.0.0.1\")];\n" +
				"  spawn boom() with [net(\"127.0.0.1\")]\n}\n",
			args:   []string{"--allow", allowLLM, "--allow", "tool=*", "--allow", "net=127.0.0.1"},
			script: []apiReply{okTools(5, 5, use("s", "slow", `{}`)), okText("never", 5, 5)},
			err:    "error: division by zero (at ", calls: 1,
			verify: func(t *testing.T, res result, api *llmAPI) {
				end, ok := waitSlowEnd(t, 30*time.Second)
				if !ok || !end.cancelled {
					t.Fatalf("the tool's request was not cancelled (seen %v, %+v)\n%s", ok, end, res)
				}
			}},
		{name: "the program waits for a spawned agent (I4)",
			src:    "let m = " + sonnet + ";\nscope { spawn agent(m, \"g\", {}) with [llm(m)] };\nputs(\"after\")\n",
			args:   []string{"--allow", allowLLM},
			script: []apiReply{okText("x", 5, 5)}, stdout: "after\n", calls: 1},
		{name: "parallel agents each use only their own tools",
			src: weatherTools + "let m = " + sonnet + ";\nlet r = scope {\n" +
				"  let a = spawn agent(m, \"g\", tools) with [llm(m), tool(\"weather\"), net(\"127.0.0.1\")];\n" +
				"  let b = spawn agent(m, \"g\", tools) with [llm(m), tool(\"notes\"), net(\"127.0.0.1\")];\n" +
				"  [await a, await b]\n};\nputs(r)\n",
			args: []string{"--allow", allowLLM, "--allow", "tool=*", "--allow", "net=127.0.0.1"},
			respond: func(body string) apiReply {
				if strings.Contains(body, "tool_result") {
					return okText("done", 5, 5)
				}
				return okTools(5, 5, use("n", "notes", `{"path":"p"}`), use("w", "weather", `{"city":"Lisbon"}`))
			},
			stdout: "[done, done]\n", calls: 4,
			verify: func(t *testing.T, res result, api *llmAPI) {
				wantArrivals(map[string]int{"w-Lisbon": 1, "notes": 1})(t, res, api)
				denials := 0
				for _, b := range api.requests() {
					denials += strings.Count(b, `capability denied: tool(\"notes\")`) + strings.Count(b, `capability denied: tool(\"weather\")`)
				}
				if denials != 2 {
					t.Fatalf("%d denials sent, want one to each agent", denials)
				}
			}},
		{name: "a task cannot give an agent a tool it does not hold (I2)",
			src: weatherTools + "let m = " + sonnet + ";\nputs(scope {\n" +
				"  await spawn agent(m, \"g\", tools) with [llm(m), tool(\"notes\")]\n})\n",
			args: []string{"--allow", allowLLM, "--allow", "tool=weather"},
			err:  `error: spawn in task root: capability denied: tool("notes") (holds: [llm("anthropic:*"), tool("weather")])`, calls: 0},
		{name: "a task cannot give an agent a model it does not hold (I2)",
			src:  "let m = " + sonnet + ";\nputs(scope { await spawn ask(m, \"q\") with [llm(m)] })\n",
			args: []string{"--allow", "llm=anthropic:claude-haiku-5-5"},
			err:  `capability denied: llm("anthropic:claude-sonnet-5-5")`, calls: 0},
		{name: "a spawned agent with no with clause holds nothing (I2)",
			src:  "let m = " + sonnet + ";\nputs(scope { await spawn ask(m, \"q\") })\n",
			args: []string{"--allow", allowLLM},
			err:  `capability denied: llm("anthropic:claude-sonnet-5-5") in task root/0 (holds: [])`, calls: 0},
		{name: "tool(\"*\") lets an agent call every tool",
			src:    weatherTools + "puts(agent(" + sonnet + ", \"g\", tools))\n",
			args:   []string{"--allow", allowLLM, "--allow", "tool=*", "--allow", "net=127.0.0.1"},
			script: []apiReply{okTools(5, 5, use("n", "notes", `{"path":"p"}`), use("w", "weather", `{"city":"Porto"}`)), okText("done", 5, 5)},
			stdout: "done\n", calls: 2, verify: wantArrivals(map[string]int{"w-Porto": 1, "notes": 1})},
	})
}

// What a model may send that is not a well-formed call to a held tool
// goes back to it as an error result; the run goes on.
func TestPhase7MalformedToolCalls(t *testing.T) {
	t.Parallel()
	prog := weatherTools + `let more = {"weather": tools["weather"], "notes": tools["notes"], "count": fn(xs) { [xs, len(xs)] }, "f": fn() { puts } };
puts(agent(` + sonnet + `, "g", more))
`
	grants := []string{"--allow", allowLLM, "--allow", "tool=*", "--allow", "net=127.0.0.1"}
	cases := []struct{ name, input, tool, want string }{
		{"unknown tool", `{}`, "nope", `"content":"unknown tool \"nope\"","is_error":true`},
		{"missing argument", `{}`, "weather", `"content":"missing argument \"city\"","is_error":true`},
		{"extra argument", `{"city":"x","y":"z"}`, "weather", `"content":"unknown argument \"y\"","is_error":true`},
		{"not a string", `{"city":5}`, "weather", `"content":"argument \"city\" must be a string","is_error":true`},
		{"not an object", `["x"]`, "weather", `"content":"arguments must be a JSON object","is_error":true`},
		{"a non-string result goes back as JSON", `{"xs":"abc"}`, "count", `"content":"[\"abc\",3]"}`},
		{"a result JSON cannot hold goes back printed", `{}`, "f", `"content":"builtin function"}`},
	}
	var specs []llmCase
	for _, c := range cases {
		specs = append(specs, llmCase{name: c.name, src: prog, args: grants,
			script: []apiReply{okTools(5, 5, use("x", c.tool, c.input)), okText("done", 5, 5)},
			stdout: "done\n", calls: 2,
			verify: all(sentHas(1, `"tool_use_id":"x",`+c.want), wantArrivals(map[string]int{"notes": 0}))})
	}
	runLLM(t, specs)
}
