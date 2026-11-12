package evaluator

import (
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// denialSrc binds d to the message of a denied getenv, which lists the
// task's grants (PRIVATE_GRANT among them).
const denialSrc = `let d = match getenv("SECRET_NAME") { ok(v) => v, err(e) => e["message"] };
`

// A tool description carrying a denial goes out redacted, like the
// prompt, the goal and every tool result (review round 1, Critical).
func TestAgentRedactsToolDescriptions(t *testing.T) {
	f := llm.NewFake(llm.TextReply("ok", 1, 1))
	src := denialSrc + `agent("anthropic:m", "go", {"t": fn(a) { a }}, {"t": d})`
	got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{},
		grant(t, runtime.LLM, "*"), grant(t, runtime.Tool, "*"), grant(t, runtime.Env, "PRIVATE_GRANT"))
	if got.Inspect() != "ok" {
		t.Fatalf("got %s", got.Inspect())
	}
	desc := f.Calls()[0].Tools[0].Description
	if desc != `capability denied: env("SECRET_NAME")` {
		t.Fatalf("description sent: %q", desc)
	}
}

// The agent's goal is redacted too (it shares userText with ask).
func TestAgentRedactsTheGoal(t *testing.T) {
	f := llm.NewFake(llm.TextReply("ok", 1, 1))
	src := denialSrc + `agent("anthropic:m", "fix this: " + d, {})`
	runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"), grant(t, runtime.Env, "PRIVATE_GRANT"))
	sent := userTextOf(f.Calls()[0])
	if sent != `fix this: capability denied: env("SECRET_NAME")` {
		t.Fatalf("goal sent: %q", sent)
	}
}

// No task ID reaches the model: not before a denial a tool's spawn
// wrapped, and not in a tool's other errors.
func TestAgentToolErrorsNameNoTask(t *testing.T) {
	for _, c := range []struct{ tool, want string }{
		{`fn() { scope { await spawn one() with [env("NOT_HELD")] } }`, `capability denied: env("NOT_HELD")`},
		{`fn() { scope { let a = spawn one(); await spawn (fn() { await a })() } }`, "await: "},
	} {
		f := llm.NewFake(llm.ToolUseReply(1, 1, llm.ToolUse("a", "s", `{}`)), llm.TextReply("ok", 1, 1))
		src := `let one = fn() { 1 }; agent("anthropic:m", "go", {"s": ` + c.tool + `})`
		got, _ := runLLM(t, src, fakeHost(f, nil), runtime.Limits{},
			grant(t, runtime.LLM, "*"), grant(t, runtime.Tool, "*"), grant(t, runtime.Env, "PRIVATE_GRANT"))
		if len(f.Calls()) != 2 {
			t.Fatalf("%s: got %s after %d calls", c.tool, got.Inspect(), len(f.Calls()))
		}
		res := toolResults(t, f.Calls()[1])[0]
		if strings.Contains(res.Text, "root") || strings.Contains(res.Text, "PRIVATE_GRANT") || !strings.Contains(res.Text, c.want) {
			t.Fatalf("%s: tool result %q", c.tool, res.Text)
		}
	}
}

// An fs denial names no host path: the CLI and read_file make a relative
// path absolute (working directory, user name) before the check.
func TestAgentFSDenialNamesNoPath(t *testing.T) {
	f := llm.NewFake(llm.ToolUseReply(1, 1, llm.ToolUse("a", "notes", `{"p": "notes.txt"}`)), llm.TextReply("ok", 1, 1))
	src := `agent("anthropic:m", "go", {"notes": fn(p) { read_file(p) }})`
	runLLM(t, src, fakeHost(f, nil), runtime.Limits{}, grant(t, runtime.LLM, "*"), grant(t, runtime.Tool, "*"))
	res := toolResults(t, f.Calls()[1])[0]
	if !res.IsError || res.Text != "capability denied: fs_read" {
		t.Fatalf("tool result %q", res.Text)
	}
}
