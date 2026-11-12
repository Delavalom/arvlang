package arv

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// The agent below holds tool("allowed") and tool("two"), not
// tool("denied"), though the model is offered all three. Every tool logs
// its run as one JSON line, so a run is seen whatever its arguments hold.
const fuzzAgentProg = `let m = "anthropic:claude-sonnet-5-5";
let tools = {
  "allowed": fn(a) { puts(json(["allowed", a])); "ok:" + a },
  "denied": fn(a) { puts(json(["denied", a])); "bad" },
  "two": fn(a, b) { puts(json(["two", a, b])); a + b }
};
puts(json(["answer", scope { await spawn agent(m, "g", tools) with [llm(m), tool("allowed"), tool("two")] }]))
`

var fuzzAgentParams = map[string][]string{"allowed": {"a"}, "denied": {"a"}, "two": {"a", "b"}}

// wellFormed is what the agent must accept as a call's input: a JSON
// object holding exactly params, each a string. It returns the strings.
func wellFormed(input string, params []string) ([]string, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(input), &obj) != nil || obj == nil || len(obj) != len(params) {
		return nil, false
	}
	out := make([]string, len(params))
	for i, p := range params {
		raw, ok := obj[p]
		if !ok || len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &out[i]) != nil {
			return nil, false
		}
	}
	return out, true
}

// runToolCalls runs fuzzAgentProg with a model that asks for two tool
// calls, then answers "done", and checks what ran and what the model got.
func runToolCalls(t *testing.T, name1, input1, name2, input2 string) {
	f := llm.NewFake(llm.ToolUseReply(10, 10, llm.ToolUse("a", name1, input1), llm.ToolUse("b", name2, input2)), llm.TextReply("done", 10, 1))
	var out bytes.Buffer
	_, err := Run(fuzzAgentProg, Options{
		Out:  &out,
		Caps: []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}, {Kind: runtime.Tool, Scope: "*"}},
		LLM:  &llm.Host{Providers: map[string]llm.Provider{"anthropic": f}},
	})
	if err != nil {
		t.Fatalf("the run failed: %v", err)
	}
	var ran [][]string
	answered := false
	for _, line := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") {
		var entry []string
		if json.Unmarshal([]byte(line), &entry) != nil || len(entry) == 0 {
			t.Fatalf("output line %q is not a log entry\n%s", line, out.String())
		}
		if entry[0] == "answer" {
			answered = len(entry) == 2 && entry[1] == "done"
			continue
		}
		ran = append(ran, entry)
	}
	if !answered {
		t.Fatalf("the program did not print the answer\n%s", out.String())
	}
	calls := f.Calls()
	if len(calls) != 2 {
		t.Fatalf("%d model calls, want 2", len(calls))
	}
	msgs := calls[1].Messages
	results := msgs[len(msgs)-1].Content
	if len(results) != 2 || results[0].ID != "a" || results[1].ID != "b" {
		t.Fatalf("results = %+v, want one for a, then one for b", results)
	}
	var want [][]string
	for i, c := range [][2]string{{name1, input1}, {name2, input2}} {
		r := results[i]
		if r.Type != llm.BlockToolResult {
			t.Fatalf("result %d has type %q", i, r.Type)
		}
		// The model's own text may say anything; nothing else may add a
		// held grant or a task to what it gets back.
		if words := c[0] + c[1]; (strings.Contains(r.Text, "holds") && !strings.Contains(words, "holds")) ||
			(strings.Contains(r.Text, "task root") && !strings.Contains(words, "task root")) {
			t.Fatalf("result %d names a grant or a task: %q", i, r.Text)
		}
		params, offered := fuzzAgentParams[c[0]]
		args, ok := wellFormed(c[1], params)
		switch {
		case c[0] == "denied":
			if !r.IsError || r.Text != `capability denied: tool("denied")` {
				t.Fatalf("a call to denied got %+v", r)
			}
		case !offered:
			if !r.IsError || !strings.HasPrefix(r.Text, "unknown tool ") {
				t.Fatalf("a call to unknown tool %q got %+v", c[0], r)
			}
		case !ok:
			if !r.IsError {
				t.Fatalf("input %q to %s was accepted: %+v", c[1], c[0], r)
			}
		default:
			result := "ok:" + args[0]
			if c[0] == "two" {
				result = args[0] + args[1]
			}
			if r.IsError || r.Text != runtime.RedactDenials(result) {
				t.Fatalf("%s(%q) got %+v, want %q", c[0], args, r, runtime.RedactDenials(result))
			}
			want = append(want, append([]string{c[0]}, args...))
		}
	}
	if len(ran) != len(want) {
		t.Fatalf("tools ran %q, want %q", ran, want)
	}
	for i := range want {
		if strings.Join(ran[i], "\x00") != strings.Join(want[i], "\x00") {
			t.Fatalf("tools ran %q, want %q", ran, want)
		}
	}
}

// FuzzAgentToolCall turns arbitrary tool calls from a model into tool
// invocations: a tool the agent does not hold never runs and its call gets
// exactly "capability denied: tool(...)"; an unknown tool or a malformed
// input is an error result; a well-formed call to a held tool runs once,
// in order, with exactly the model's strings; and the program always
// finishes with the model's answer.
func FuzzAgentToolCall(f *testing.F) {
	f.Add("allowed", `{"a":"x"}`, "denied", `{"a":"y"}`)
	f.Add("two", `{"a":"1","b":"2"}`, "allowed", `{"a":"capability denied: net(\"h\") in task root (holds: [net(\"h\")])"}`)
	f.Add("nope", `{}`, "two", `{"a":"1"}`)
	f.Add("allowed", `{"a":5}`, "allowed", `{"a":"\ud800","a":"z"}`)
	f.Add("*", `null`, "", `[`)
	f.Add("allowed", ` {"a" : "\n"} `, "two", `{"b":"","a":"","c":""}`)
	f.Fuzz(runToolCalls)
}
