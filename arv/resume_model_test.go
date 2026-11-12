package arv

import (
	"bytes"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// resumeModel resumes crashed for src with a fresh model host that gives
// replies, and returns what it printed and its error.
func resumeModel(t *testing.T, src string, crashed []byte, caps []runtime.Capability, replies ...llm.Reply) (string, error) {
	t.Helper()
	l, err := runtime.ReadLog(bytes.NewReader(crashed))
	if err != nil || l.End != nil {
		t.Fatalf("crashed log: %v, end %v", err, l.End)
	}
	host := &llm.Host{Providers: map[string]llm.Provider{"anthropic": llm.NewFake(replies...)}}
	var out bytes.Buffer
	_, err = Run(src, Options{LLM: host, Caps: caps, Resume: l, Record: bytes.NewBuffer(append([]byte(nil), crashed[:l.Size]...)), Out: &out})
	return out.String(), err
}

// Review round 4 (Critical): a crash during a model call leaves llm_model
// logged and the llm call not. The resume replayed llm_model, which left
// no provider, and the live call then panicked on it. The live call now
// asks the host for the provider itself.
func TestResumeInsideAModelCall(t *testing.T) {
	caps := []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}, {Kind: runtime.Tool, Scope: "lookup"}}
	t.Run("ask", func(t *testing.T) {
		src := `puts(ask("anthropic:claude-haiku-5-5", "Capital of Portugal?"))`
		host := &llm.Host{Providers: map[string]llm.Provider{"anthropic": llm.NewFake(llm.TextReply("Lisbon", 5, 2))}}
		out, log, err := recordRun(t, src, Options{LLM: host, Caps: caps})
		if err != nil || out != "Lisbon\n" {
			t.Fatalf("recording: %v, %q", err, out)
		}
		got, err := resumeModel(t, src, cutAfter(t, log, `"kind":"llm_model"`), caps, llm.TextReply("Lisbon", 5, 2))
		if err != nil || got != out {
			t.Fatalf("resume: %v, printed %q, want %q", err, got, out)
		}
	})
	t.Run("agent between turns", func(t *testing.T) {
		src := `let lookup = fn(city) { "population of " + city + ": 545k" };
puts(agent("anthropic:claude-haiku-5-5", "How many live in Lisbon?", {"lookup": lookup}))`
		turn2 := llm.TextReply("done: 545k", 20, 4)
		host := &llm.Host{Providers: map[string]llm.Provider{"anthropic": llm.NewFake(
			llm.ToolUseReply(10, 3, llm.ToolUse("u1", "lookup", `{"city": "Lisbon"}`)), turn2)}}
		out, log, err := recordRun(t, src, Options{LLM: host, Caps: caps})
		if err != nil || out != "done: 545k\n" {
			t.Fatalf("recording: %v, %q", err, out)
		}
		got, err := resumeModel(t, src, cutAfter(t, log, `"kind":"llm","req"`), caps, turn2)
		if err != nil || got != out {
			t.Fatalf("resume: %v, printed %q, want %q", err, got, out)
		}
	})
}
