package arv

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// Leak rule for Phase 7: no goroutine of a model call (the provider's
// transport, its connection, the evaluator's task) or of a tool an agent
// runs outlives arv.Run, whether the calls succeed, are refused by a
// budget, or are cancelled in flight by a timeout, a failing sibling or
// the host's context. The real Anthropic provider talks to a local fake.
func TestP7NoGoroutineOutlivesRun(t *testing.T) {
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		switch {
		case strings.Contains(s, "HANG"): // hold the call until the client gives up
			select {
			case <-r.Context().Done():
			case <-release:
			}
		case strings.Contains(s, `"tools"`) && !strings.Contains(s, "tool_result"):
			tool := "fast"
			if strings.Contains(s, "SLOWTOOL") {
				tool = "slow"
			}
			fmt.Fprintf(w, `{"content":[{"type":"tool_use","id":"u","name":%q,"input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":5}}`, tool)
		default:
			io.WriteString(w, `{"content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":5}}`)
		}
	})
	mux.HandleFunc("/hang", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer close(release)

	host := &llm.Host{Providers: map[string]llm.Provider{"anthropic": llm.NewAnthropic("sk-ant-leak-test", srv.URL+"/v1/messages")}}
	caps := []runtime.Capability{{Kind: runtime.LLM, Scope: "anthropic:*"}, {Kind: runtime.Tool, Scope: "*"}, {Kind: runtime.Net, Scope: "127.0.0.1"}}
	prelude := fmt.Sprintf(`let m = "anthropic:claude-haiku-5-5";
let tools = {"fast": fn() { "x" }, "slow": fn() { http_get(%q)["body"] }};
let g = [llm(m), tool("fast"), tool("slow"), net("127.0.0.1")];
`, srv.URL+"/hang")
	runs := []struct {
		name, src string
		limits    runtime.Limits
		cancel    time.Duration // cancel the host's context after this; 0: never
		errHas    string        // "" means the run succeeds
	}{
		{"agents in parallel, with tool calls",
			`[await spawn agent(m, "a", tools) with g, await spawn agent(m, "b", tools) with g, await spawn ask(m, "c") with g]`,
			runtime.Limits{}, 0, ""},
		{"a call in flight when the scope times out",
			`scope (timeout_ms: 100) { ask(m, "HANG") }`, runtime.Limits{}, 0, "budget exceeded: timeout (limit 100 ms)"},
		{"parallel calls in flight when a sibling fails",
			`let boom = fn() { sleep(100); 1 / 0 };
scope { for i in [1, 2, 3, 4] { spawn ask(m, "HANG") with g }; spawn boom() }`, runtime.Limits{}, 0, "division by zero"},
		{"a tool in flight when the scope times out",
			`scope (timeout_ms: 300) { await spawn agent(m, "SLOWTOOL", tools) with g }`, runtime.Limits{}, 0, "budget exceeded: timeout (limit 300 ms)"},
		{"parallel calls refused by a shared budget",
			`scope (supervise: "collect") { let ts = for i in [1, 2, 3, 4, 5, 6] { spawn ask(m, "q") with g }; for t in ts { match await t { ok(v) => v, err(e) => e["kind"] } } }`,
			runtime.Limits{Tokens: 20000}, 0, ""},
		{"an agent past max_turns",
			`scope (max_turns: 1) { agent(m, "a", tools) }`, runtime.Limits{}, 0, "budget exceeded: max_turns (limit 1)"},
		{"the host cancels a run with an agent and a call in flight",
			`scope { spawn agent(m, "SLOWTOOL", tools) with g; spawn ask(m, "HANG") with g }`, runtime.Limits{}, 200 * time.Millisecond, "context"},
	}
	// Warm up once so lazily started runtime goroutines are in the baseline.
	if _, err := Run(prelude+`ask(m, "warm")`, Options{Caps: caps, LLM: host}); err != nil {
		t.Fatal(err)
	}
	before := settle(0)
	for _, r := range runs {
		ctx, cancel := context.WithCancel(context.Background())
		if r.cancel > 0 {
			time.AfterFunc(r.cancel, cancel)
		}
		start := time.Now()
		_, err := Run(prelude+r.src, Options{Ctx: ctx, Caps: caps, LLM: host, Limits: r.limits})
		cancel()
		switch {
		case r.errHas == "" && err != nil:
			t.Errorf("%s: %v", r.name, err)
		case r.errHas != "" && (err == nil || !strings.Contains(err.Error(), r.errHas)):
			t.Errorf("%s: err = %v, want one containing %q", r.name, err, r.errHas)
		}
		if d := time.Since(start); d > time.Minute {
			t.Errorf("%s: the run took %s", r.name, d)
		}
	}
	if after := settle(before); after > before {
		buf := make([]byte, 1<<20)
		t.Fatalf("%d goroutines before the runs, %d after:\n%s", before, after, buf[:goruntime.Stack(buf, true)])
	}
}
