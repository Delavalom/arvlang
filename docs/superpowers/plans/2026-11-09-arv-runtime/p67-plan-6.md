# Phase 6x7, task p67.6: an agent run resumed after any line of its log

> **For agentic workers:** this file is one task, p67.6. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p67.1 to p67.5 are committed on `phase/p67`. Work in `/Users/delavalom/delavalom-labs/arvlang-wt/p67` on branch `phase/p67`. Start every shell command with `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 &&`: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`, never push, never touch remotes; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `min`, `max` and `t.Chdir` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report status "blocked", reason "disk" (only `go clean -cache` may be deleted). The machine is shared and heavily loaded: keep wall-clock bounds generous and order events with server gates, not sleeps. No test may reach the network or spend money: every test uses the fake provider or a local `httptest` server; never set `ARV_LIVE_ANTHROPIC`, never put a real API key anywhere. If a test exposes a real bug outside this task, mark it `t.Skip("BUG: ...")` with a reproduction and report it; never weaken a test to make it pass.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` at `b516ca2` (Phases 3 to 8.3 and 5E merged) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`); each "expected failure" was seen. Designs: `13-phase6-durable-replay.md`, `14-phase7-agents.md`; what was built overrides them (`16-implementation-notes.md`, `16o-phase-index.md`). Phase overview and deviations: `p67-plan-1.md`.

### Task p67.6: an agent run resumed after any line of its log

"Resume after SIGKILL mid-agent-run equals an uninterrupted run", at every point: the run of p67.5 (with `agentPlanDet`, which has no `random` or `now`, so the two runs can be compared) is recorded once, and for each line of its log the log is cut after that line and resumed with a fresh fake. Each resume must print the uninterrupted run's output, make exactly one live model call per `llm` record the cut log lacks, one request per missing `http_get`, one host call per missing `host:k8s_apply`, send only requests the uninterrupted run sent (compared as JSON), and leave a log whose replay prints the same again. No goroutine may survive any run (`noLeak`, `settle`). It covers a crash between `llm_model` and `llm_reserve`, between `llm_reserve` and `llm` (the resume takes the logged reservation and sends the call live), between turns, and inside a spawned agent. Expect it to pass at once; a failure is a real gap: stop and report it.

**Files:**
- Create (test): `arv/resume_agent_test.go`

- [ ] **Step 1: Write the test**

Create `arv/resume_agent_test.go`:

```go
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
```

- [ ] **Step 2: Run it**

Run: `cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && go test ./arv -run TestResumeOfAnAgentRunAfterAnyLine -count=1 -v`

Expected: PASS. It logs `resumed after each of 38 lines` and takes well under a minute.

- [ ] **Step 3: Check and commit**

```bash
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && gofmt -l . && go vet ./... && go test -race ./...
cd /Users/delavalom/delavalom-labs/arvlang-wt/p67 && git add -A && git commit -m "test(arv): an agent run resumed after any line equals the uninterrupted run

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
