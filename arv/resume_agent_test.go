package arv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// agentPlanDet is agentPlan without randomness or time, so a resumed run
// can be compared with the uninterrupted one.
var agentPlanDet = agentScript{
	"main":  {{"fetch:/a", "save:out.txt", "home:ARV_P67_HOME", "deploy:web.yaml", "secret:x", "broken:x"}, {"load:out.txt"}},
	"child": {{"fetch:/b", "deploy:api.yaml", "save:denied.txt"}},
	"loop":  {{"fetch:/c"}, {"home:ARV_P67_HOME"}, {"fetch:/d"}, {"deploy:x"}, {"fetch:/e"}},
}

// requestKeys is each request a provider received, as JSON.
func requestKeys(t *testing.T, calls []llm.Request) map[string]bool {
	keys := map[string]bool{}
	for _, c := range calls {
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		keys[string(b)] = true
	}
	return keys
}

// A run killed anywhere in an agent run, resumed, prints what the
// uninterrupted run printed. Its live part repeats only what the log
// lacks: one model call per llm record missing, one request per http_get
// record missing, one host call per host record missing, each live model
// request one that the uninterrupted run sent. A replay of the resumed
// log prints the same again, and no goroutine outlives any of the runs.
func TestResumeOfAnAgentRunAfterAnyLine(t *testing.T) {
	w := newAgentWorld(t, agentRun)
	ref := agentPlanDet.fake()
	host := func(f *llm.Fake) *llm.Host { return &llm.Host{Providers: map[string]llm.Provider{"anthropic": f}} }
	out, log, err := recordRun(t, w.src, Options{LLM: host(ref), Caps: w.caps, Effects: w.effects, Ctx: hostCtx(t)})
	if err != nil || !strings.Contains(out, "max_turns (limit 4)") {
		t.Fatalf("recording: %v\n%s", err, out)
	}
	sent := requestKeys(t, ref.Calls())
	calls, hits, applied := int64(len(ref.Calls())), w.hits.Load(), w.applied.Load()
	before := settle(0)
	lines := bytes.SplitAfter(log, []byte("\n"))
	for k := 1; k < len(lines)-1; k++ {
		crashed := bytes.Join(lines[:k], nil)
		count := func(kind string) int64 { return int64(bytes.Count(crashed, []byte(`"kind":"`+kind+`","req"`))) }
		l, err := runtime.ReadLog(bytes.NewReader(crashed))
		if err != nil {
			t.Fatal(err)
		}
		live := agentPlanDet.fake()
		h0, a0 := w.hits.Load(), w.applied.Load()
		rec := bytes.NewBuffer(bytes.Clone(crashed))
		var got bytes.Buffer
		_, err = Run(w.src, Options{LLM: host(live), Caps: w.caps, Effects: w.effects, Resume: l, Record: rec, Out: &got, Ctx: hostCtx(t)})
		noLeak(t, before, fmt.Sprintf("resume after %d lines", k))
		if err != nil || got.String() != out {
			t.Fatalf("resume after %d lines: %v\nprinted %q\nwant    %q", k, err, got.String(), out)
		}
		if n := int64(len(live.Calls())); n != calls-count("llm") {
			t.Fatalf("resume after %d lines: %d live model calls, want %d", k, n, calls-count("llm"))
		}
		for key := range requestKeys(t, live.Calls()) {
			if !sent[key] {
				t.Fatalf("resume after %d lines sent a request the uninterrupted run did not:\n%s", k, key)
			}
		}
		if h, a := w.hits.Load()-h0, w.applied.Load()-a0; h != hits-count("http_get") || a != applied-count("host:k8s_apply") {
			t.Fatalf("resume after %d lines: %d requests and %d host calls, want %d and %d",
				k, h, a, hits-count("http_get"), applied-count("host:k8s_apply"))
		}
		resumed, err := runtime.ReadLog(bytes.NewReader(rec.Bytes()))
		if err != nil {
			t.Fatalf("the resume after %d lines: %v", k, err)
		}
		var replayed bytes.Buffer
		_, err = Run(w.src, Options{Replay: resumed, Out: &replayed, Effects: w.effects})
		if err != nil || replayed.String() != out {
			t.Fatalf("replay of the resume after %d lines: %v\n%s", k, err, replayed.String())
		}
	}
	t.Logf("resumed after each of %d lines", len(lines)-2)
	if after := settle(before); after > before {
		t.Fatalf("%d goroutines before, %d after", before, after)
	}
}
