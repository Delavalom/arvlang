package main

import (
	"strings"
	"testing"
)

// examples/agent.arv runs as its header says, against a local fake of the
// Messages API: the model asks for both tools, weather runs with the
// agent's grants, notes is refused as a tool result, and the answer is
// printed.
func TestAgentExample(t *testing.T) {
	api := newFakeAPI(t, 200,
		`{"content":[{"type":"tool_use","id":"n","name":"notes","input":{"path":"/etc/hosts"}},`+
			`{"type":"tool_use","id":"w","name":"weather","input":{"city":"Lisbon"}}],"stop_reason":"tool_use","usage":{"input_tokens":300,"output_tokens":40}}`,
		textReply("Yes: it is sunny and dry in Lisbon."))
	code, out, stderr := cli("run", "--allow", "llm=anthropic:claude-sonnet-5-5", "--allow", "tool=weather",
		"--allow", "fs_read=examples/weather.txt", "examples/agent.arv")
	if code != 0 || out != "Yes: it is sunny and dry in Lisbon.\n" {
		t.Fatalf("exit %d, out %q, err %q", code, out, stderr)
	}
	if len(api.bodies) != 2 {
		t.Fatalf("%d requests, want 2", len(api.bodies))
	}
	second := api.bodies[1]
	for _, want := range []string{
		`"tool_use_id":"n"`, `capability denied: tool(\"notes\")`,
		`"tool_use_id":"w"`, `Forecast for Lisbon: Sunny, 22 C`,
		`"description":"Read a file of personal notes"`,
	} {
		if !strings.Contains(second, want) {
			t.Errorf("second request lacks %s:\n%s", want, second)
		}
	}
	noKey(t, append([]string{out, stderr}, api.bodies...)...)
	// Without the model grant the spawn is refused before any request.
	newFakeAPI(t, 200, textReply("unused"))
	code, _, stderr = cli("run", "--allow", "tool=weather", "--allow", "fs_read=examples/weather.txt", "examples/agent.arv")
	if code != 1 || !strings.Contains(stderr, `capability denied: llm("anthropic:claude-sonnet-5-5")`) {
		t.Fatalf("exit %d, err %q", code, stderr)
	}
}
