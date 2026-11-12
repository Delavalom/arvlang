package arv

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// recorder passes calls to p and keeps every response.
type recorder struct {
	p     llm.Provider
	mu    sync.Mutex
	resps []llm.Response
}

func (r *recorder) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	resp, err := r.p.Complete(ctx, req)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.resps = append(r.resps, resp)
	}
	return resp, err
}

// The one test that calls the real Anthropic Messages API. It costs money,
// so it runs only with ARV_LIVE_ANTHROPIC=1 and ANTHROPIC_API_KEY set.
func TestLiveAnthropic(t *testing.T) {
	if os.Getenv("ARV_LIVE_ANTHROPIC") != "1" {
		t.Skip("set ARV_LIVE_ANTHROPIC=1 and ANTHROPIC_API_KEY to call the real API (it costs money)")
	}
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Fatal("ARV_LIVE_ANTHROPIC=1 needs ANTHROPIC_API_KEY")
	}
	rec := &recorder{p: llm.NewAnthropic(key, "")}
	opts := Options{
		LLM:  &llm.Host{Providers: map[string]llm.Provider{"anthropic": rec}},
		Caps: []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:claude-haiku-5-5"}},
	}
	var out bytes.Buffer
	opts.Out = &out
	// Each call reserves about 4.6K tokens, so the second fits in 6000 only
	// if the first was settled to its real usage.
	src := `scope (tokens: 6000, usd_cents: 1) {
  let a = ask("anthropic:claude-haiku-5-5", "Reply with the word ok");
  let b = ask("anthropic:claude-haiku-5-5", "Reply with the word ok");
  a + " " + b
}`
	got, err := Run(src, opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.ToLower(got.Inspect()), "ok") < 2 {
		t.Fatalf("replies = %q", got.Inspect())
	}
	if len(rec.resps) != 2 || rec.resps[0].InputTokens <= 0 || rec.resps[0].OutputTokens <= 0 {
		t.Fatalf("usage = %+v", rec.resps)
	}
	if strings.Contains(got.Inspect(), key) || strings.Contains(out.String(), key) {
		t.Fatal("the key reached Arv output")
	}
}
