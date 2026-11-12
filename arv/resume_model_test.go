package arv

import (
	"bytes"
	"regexp"
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

// Review round 5 (Critical): a live call in a resume took the model ID
// and price from the log's llm_model, which a forged log can set to any
// model of the granted provider, priced at 0. The live call now asks the
// host for the model's ID and price, and diverges if they differ from the
// log's: no request is sent.
func TestResumeRefusesAForgedModel(t *testing.T) {
	caps := []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:claude-haiku-5-5"}}
	src := `puts(ask("anthropic:claude-haiku-5-5", "Capital of Portugal?"))`
	host := &llm.Host{Providers: map[string]llm.Provider{"anthropic": llm.NewFake(llm.TextReply("Lisbon", 5, 2))}}
	out, log, err := recordRun(t, src, Options{LLM: host, Caps: caps, Limits: runtime.Limits{USDCents: 1}})
	if err != nil || out != "Lisbon\n" {
		t.Fatalf("recording: %v, %q", err, out)
	}
	crashed := cutAfter(t, log, `"kind":"llm_model"`)
	free := func(b []byte) []byte {
		for _, k := range []string{"input", "output", "long_input", "long_output"} {
			b = regexp.MustCompile(`\["`+k+`",\d+\]`).ReplaceAll(b, []byte(`["`+k+`",0]`))
		}
		return b
	}
	edits := map[string]func([]byte) []byte{
		"model id and price": func(b []byte) []byte {
			return free(bytes.Replace(b, []byte(`["id","claude-haiku-5-5"]`), []byte(`["id","claude-opus-5-5"]`), 1))
		},
		"model id": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`["id","claude-haiku-5-5"]`), []byte(`["id","claude-opus-5-5"]`), 1)
		},
		"price": free,
		"unpriced": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`["priced",true]`), []byte(`["priced",false]`), 1)
		},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			forged := forge(t, crashed, func(i int, b []byte) []byte {
				if bytes.Contains(b, []byte(`"kind":"llm_model"`)) {
					if e := edit(b); !bytes.Equal(e, b) {
						return e
					}
					t.Fatalf("the edit changed nothing in %s", b)
				}
				return b
			})
			fake := llm.NewFake(llm.TextReply("Lisbon", 500000, 200000))
			l, err := runtime.ReadLog(bytes.NewReader(forged))
			if err != nil || l.End != nil {
				t.Fatalf("forged log: %v", err)
			}
			var got bytes.Buffer
			_, err = Run(src, Options{LLM: &llm.Host{Providers: map[string]llm.Provider{"anthropic": fake}}, Caps: caps,
				Resume: l, Record: bytes.NewBuffer(append([]byte(nil), forged[:l.Size]...)), Out: &got})
			if calls := fake.Calls(); len(calls) != 0 {
				t.Fatalf("the resume sent %d live calls, the first to model %q (err %v)", len(calls), calls[0].Model, err)
			}
			// Unpriced, the call is refused by usd_cents before it is
			// sent; otherwise the forged record is a divergence.
			var ae *Error
			if !errorsAs(err, &ae) || ae.Kind != runtime.KindReplay && name != "unpriced" || err == nil {
				t.Fatalf("resume: %v, printed %q, want a divergence", err, got.String())
			}
		})
	}
}
